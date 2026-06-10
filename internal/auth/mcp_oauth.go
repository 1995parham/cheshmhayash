package auth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// wellKnownPRMPath is the RFC 9728 Protected Resource Metadata well-known
// path prefix. The MCP server serves it (and the path-specific variant) so
// clients can discover the authorization server after a 401.
const wellKnownPRMPath = "/.well-known/oauth-protected-resource"

// verifyMCPToken validates an OAuth access-token JWT presented at /mcp and
// resolves it to a session + role using the same allowlist as the dashboard.
// The token must be signed by the configured OIDC issuer and carry one of the
// accepted audiences (RFC 8707) — this is what stops a token minted for some
// other service from being replayed here (the "confused deputy" problem the
// MCP spec calls out). Operators whose IdP can't mint resource audiences can
// opt out via auth.mcp_oauth.skip_audience_check; signature, issuer, expiry
// and the allowlist still apply.
func (a *Authenticator) verifyMCPToken(ctx context.Context, raw string) (sessionData, Role, error) {
	tok, err := a.mcpVerifier.Verify(ctx, raw)
	if err != nil {
		return sessionData{}, "", err
	}
	if !a.cfg.MCPOAuth.SkipAudienceCheck && !audienceMatches(tok.Audience, a.mcpAudiences) {
		return sessionData{}, "", errors.New("token audience not accepted for this MCP resource")
	}
	// sessionFromIDToken pulls the same standard + groups claims the UI uses;
	// go-oidc returns an *oidc.IDToken from Verify for any signed JWT, so the
	// access token's identity claims decode through the same path.
	sess, err := a.sessionFromIDToken(tok)
	if err != nil {
		return sessionData{}, "", err
	}
	role, ok := a.authorize(sess)
	if !ok {
		return sessionData{}, "", errors.New("subject is not on the allowlist")
	}
	return sess, role, nil
}

// audienceMatches reports whether any of the token's aud values is in the
// accepted set. Audiences are URIs compared exactly.
func audienceMatches(have, want []string) bool {
	for _, h := range have {
		if slices.Contains(want, h) {
			return true
		}
	}
	return false
}

// authorizationServers is the metadata's authorization_servers list:
// the explicit override, or the OIDC issuer the dashboard already uses.
func (a *Authenticator) authorizationServers() []string {
	if len(a.cfg.MCPOAuth.AuthorizationServers) > 0 {
		return a.cfg.MCPOAuth.AuthorizationServers
	}
	return []string{a.cfg.OIDC.Issuer}
}

// resourceMetadataURL builds the absolute Protected Resource Metadata URL for
// the configured resource, per RFC 9728 §3.1 (well-known path inserted before
// the resource's path). Falls back to the bare well-known path if the
// configured resource isn't a parseable absolute URL.
func (a *Authenticator) resourceMetadataURL() string {
	u, err := url.Parse(a.cfg.MCPOAuth.Resource)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return wellKnownPRMPath
	}
	path := strings.TrimSuffix(u.Path, "/")
	return u.Scheme + "://" + u.Host + wellKnownPRMPath + path
}

// RegisterMCPMetadata attaches the RFC 9728 Protected Resource Metadata
// endpoints. Both the bare path and the resource-suffixed path return the same
// document — clients differ on which they request. These are public (not under
// /api/ or /mcp, so isPublic already lets them through).
func (a *Authenticator) RegisterMCPMetadata(mux *http.ServeMux) {
	if !a.MCPOAuthEnabled() {
		return
	}
	mux.HandleFunc("GET "+wellKnownPRMPath, a.handleResourceMetadata)
	mux.HandleFunc("GET "+wellKnownPRMPath+"/mcp", a.handleResourceMetadata)
}

func (a *Authenticator) handleResourceMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 a.cfg.MCPOAuth.Resource,
		"authorization_servers":    a.authorizationServers(),
		"scopes_supported":         []string{"openid", "profile", "email"},
		"bearer_methods_supported": []string{"header"},
	})
}
