package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// MCPJWTEnabled reports whether /mcp should accept unverified bearer JWTs
// (auth.mcp_jwt) — claims are read without any signature/issuer/expiry
// check. Nil-safe.
func (a *Authenticator) MCPJWTEnabled() bool {
	return a != nil && a.cfg.Enabled && a.cfg.MCPJWT.Enabled
}

// mcpJWTSession resolves an UNVERIFIED bearer JWT to a session + role.
// Nothing about the token is validated — not the signature, issuer,
// audience, or expiry; the payload is simply decoded and its identity
// claims run through the same auth.access allowlists as every other path.
// The trust model is "a gateway in front already verified this token";
// anyone who can reach /mcp directly can forge any identity, so the
// operator opts in explicitly via auth.mcp_jwt.enabled.
func (a *Authenticator) mcpJWTSession(raw string) (sessionData, Role, error) {
	sess, err := a.decodeUnverifiedJWT(raw)
	if err != nil {
		return sessionData{}, "", err
	}
	role, ok := a.authorize(sess)
	if !ok {
		return sessionData{}, "", errors.New("subject is not on the allowlist")
	}
	return sess, role, nil
}

// decodeUnverifiedJWT extracts the standard identity claims (plus the
// configured groups claim) from a JWT payload without verifying anything.
// It only requires the token to be structurally a JWT: three dot-separated
// segments with a base64url JSON payload.
func (a *Authenticator) decodeUnverifiedJWT(raw string) (sessionData, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return sessionData{}, errors.New("token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return sessionData{}, errors.New("malformed JWT payload encoding")
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(payload, &all); err != nil {
		return sessionData{}, errors.New("JWT payload is not a JSON object")
	}
	str := func(key string) string {
		var s string
		if v, ok := all[key]; ok {
			_ = json.Unmarshal(v, &s)
		}
		return s
	}
	return sessionData{
		Sub:        str("sub"),
		Email:      str("email"),
		Name:       str("name"),
		GivenName:  str("given_name"),
		FamilyName: str("family_name"),
		Groups:     groupsFromClaims(all, a.cfg.Access.GroupsClaim),
	}, nil
}

// groupsFromClaims reads the groups claim out of a decoded claims map,
// accepting both array and single-string shapes (mirrors extractGroups,
// which works on a verified *oidc.IDToken instead of a raw map).
func groupsFromClaims(all map[string]json.RawMessage, claim string) []string {
	if claim == "" {
		claim = "groups"
	}
	v, ok := all[claim]
	if !ok {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(v, &arr); err == nil {
		return arr
	}
	var single string
	if err := json.Unmarshal(v, &single); err == nil && single != "" {
		return []string{single}
	}
	return nil
}
