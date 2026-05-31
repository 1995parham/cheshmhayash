// Package auth wires OIDC login + HMAC-signed cookie sessions for the HTTP
// API and bearer-token auth for MCP HTTP. State lives entirely in the
// cookie — no database, no in-memory session table — so the binary stays a
// single-file deploy.
//
// When config.Auth.Enabled is false the package is dormant; callers should
// just skip the middleware. The public Authenticator type carries every
// dependency a wired-up handler needs.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/1995parham/cheshmhayash/internal/config"
)

// Authenticator is the runtime entry point — built once at startup,
// shared across all requests.
type Authenticator struct {
	cfg      config.Auth
	log      *slog.Logger
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config
	signer   signer

	// mcpVerifier validates access-token JWTs presented at /mcp when
	// auth.mcp_oauth is enabled. It's distinct from verifier: the UI
	// verifier pins aud == client_id (correct for ID tokens), whereas an
	// access token's audience is the MCP resource, checked separately
	// against mcpAudiences. Nil when MCP OAuth is off.
	mcpVerifier  *oidc.IDTokenVerifier
	mcpAudiences []string
}

// New builds the authenticator. The OIDC discovery call may block on
// network I/O, so it takes a context. Returns nil and a clear error if any
// of the inputs are missing — caller should still validate cfg.Enabled
// before invoking.
func New(ctx context.Context, cfg config.Auth, log *slog.Logger) (*Authenticator, error) {
	if !cfg.Enabled {
		return nil, errors.New("auth.New called with Enabled=false")
	}
	provider, err := oidc.NewProvider(ctx, cfg.OIDC.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery (%s): %w", cfg.OIDC.Issuer, err)
	}
	scopes := cfg.OIDC.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}
	oauthCfg := &oauth2.Config{
		ClientID:     cfg.OIDC.ClientID,
		ClientSecret: cfg.OIDC.ClientSecret,
		RedirectURL:  cfg.OIDC.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes,
	}
	a := &Authenticator{
		cfg:      cfg,
		log:      log,
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.OIDC.ClientID}),
		oauth:    oauthCfg,
		signer:   newSigner([]byte(cfg.Session.Secret)),
	}
	if cfg.MCPOAuth.Enabled {
		// SkipClientIDCheck because we validate the audience ourselves
		// against mcpAudiences — an access token's aud is the MCP resource,
		// not the dashboard client_id. Signature, issuer and expiry are still
		// enforced by Verify.
		a.mcpVerifier = provider.Verifier(&oidc.Config{SkipClientIDCheck: true})
		a.mcpAudiences = cfg.MCPOAuth.Audiences
		if len(a.mcpAudiences) == 0 {
			a.mcpAudiences = []string{cfg.MCPOAuth.Resource}
		}
	}
	return a, nil
}

// MCPOAuthEnabled reports whether the /mcp transport should accept OIDC
// access tokens (in addition to any static keys). Nil-safe.
func (a *Authenticator) MCPOAuthEnabled() bool {
	return a != nil && a.cfg.Enabled && a.cfg.MCPOAuth.Enabled && a.mcpVerifier != nil
}

// Enabled reports whether the authenticator should be applied. A nil
// receiver counts as disabled so call sites don't need a separate nil check.
func (a *Authenticator) Enabled() bool {
	return a != nil && a.cfg.Enabled
}

// Role is the access tier a session resolves to. RoleReadOnly may issue
// only safe (GET) requests; RoleAdmin may also mutate (POST/PUT/DELETE).
type Role string

// The two access tiers a session can resolve to.
const (
	RoleReadOnly Role = "readonly"
	RoleAdmin    Role = "admin"
)

// matchIdentity reports whether the session satisfies any of the given
// email / domain / group rules. Empty slices match nobody.
func matchIdentity(s sessionData, emails, domains, groups []string) bool {
	for _, e := range emails {
		if strings.EqualFold(e, s.Email) {
			return true
		}
	}
	if domain := domainOf(s.Email); domain != "" {
		for _, d := range domains {
			if strings.EqualFold(d, domain) {
				return true
			}
		}
	}
	for _, want := range groups {
		for _, have := range s.Groups {
			if strings.EqualFold(want, have) {
				return true
			}
		}
	}
	return false
}

// authorize decides whether a session may access the dashboard and at what
// role. A user is let in if they match the sign-in allowlist OR the admin
// allowlist (so an admin who isn't also on the base list still gets in).
//
// Role resolution:
//   - admin allowlist empty  → every signed-in user is RoleAdmin (this is
//     the pre-role behaviour, kept for backward compatibility).
//   - matches admin allowlist → RoleAdmin.
//   - otherwise (sign-in only) → RoleReadOnly.
//
// Re-evaluated on every request, so moving someone between tiers in
// settings takes effect on their next call without a re-login.
func (a *Authenticator) authorize(s sessionData) (Role, bool) {
	base := matchIdentity(s, a.cfg.Access.AllowedEmails, a.cfg.Access.AllowedDomains, a.cfg.Access.AllowedGroups)
	admin := matchIdentity(s,
		a.cfg.Access.Admin.AllowedEmails,
		a.cfg.Access.Admin.AllowedDomains,
		a.cfg.Access.Admin.AllowedGroups,
	)

	if a.cfg.Access.Admin.IsEmpty() {
		if base {
			return RoleAdmin, true
		}
		return "", false
	}
	if admin {
		return RoleAdmin, true
	}
	if base {
		return RoleReadOnly, true
	}
	return "", false
}

func domainOf(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return email[at+1:]
}

// ctxKey scopes context values so they can't collide with anything else.
type ctxKey int

const (
	sessionKey ctxKey = iota + 1
)

// Identity is the subset of session data we expose to other packages —
// kept tiny on purpose so handlers don't grow accidental dependencies on
// the cookie format. Role is the resolved access tier.
type Identity struct {
	Sub        string
	Email      string
	Name       string
	GivenName  string
	FamilyName string
	Groups     []string
	Role       Role
}

// sessionCtx is what the middleware stashes in the request context: the
// verified cookie payload plus the role resolved for this request.
type sessionCtx struct {
	data sessionData
	role Role
}

// FromContext returns the identity attached by the middleware, or zero if
// none — useful for /api/auth/me.
func FromContext(ctx context.Context) (Identity, bool) {
	v, ok := ctx.Value(sessionKey).(sessionCtx)
	if !ok {
		return Identity{}, false
	}
	s := v.data
	return Identity{
		Sub:        s.Sub,
		Email:      s.Email,
		Name:       s.Name,
		GivenName:  s.GivenName,
		FamilyName: s.FamilyName,
		Groups:     s.Groups,
		Role:       v.role,
	}, true
}

// withSession returns a new request whose context carries the verified
// session payload and resolved role.
func withSession(r *http.Request, s sessionData, role Role) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), sessionKey, sessionCtx{data: s, role: role}))
}
