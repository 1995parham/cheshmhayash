package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// errForbidden marks a token that verified cleanly but whose subject isn't on
// the allowlist. The middleware maps it to 403 (vs 401 for a missing,
// malformed, expired, or wrong-issuer/audience token).
var errForbidden = errors.New("access denied")

// verifyBearer validates an access-token JWT presented in jwt mode and
// resolves it to a session + role using the same allowlist as the cookie
// flow. The token must be signed by the configured OIDC issuer and — when
// jwtAudiences is set — carry one of those audiences (RFC 8707).
func (a *Authenticator) verifyBearer(ctx context.Context, raw string) (sessionData, Role, error) {
	tok, err := a.jwtVerifier.Verify(ctx, raw)
	if err != nil {
		return sessionData{}, "", err
	}
	if len(a.jwtAudiences) > 0 && !audienceMatches(tok.Audience, a.jwtAudiences) {
		return sessionData{}, "", errors.New("token audience not accepted")
	}
	// go-oidc returns an *oidc.IDToken from Verify for any signed JWT, so the
	// access token's standard + groups claims decode through the same path the
	// UI uses.
	sess, err := a.sessionFromIDToken(tok)
	if err != nil {
		return sessionData{}, "", err
	}
	role, ok := a.authorize(sess)
	if !ok {
		return sessionData{}, "", errForbidden
	}
	return sess, role, nil
}

// bearerFromRequest extracts the Authorization: Bearer token and verifies it.
func (a *Authenticator) bearerFromRequest(r *http.Request) (sessionData, Role, error) {
	hdr := r.Header.Get("Authorization")
	token := strings.TrimSpace(strings.TrimPrefix(hdr, "Bearer "))
	if hdr == "" || token == hdr || token == "" {
		// No header, a non-Bearer scheme, or an empty token.
		return sessionData{}, "", errors.New("bearer token required")
	}
	return a.verifyBearer(r.Context(), token)
}

// jwtMiddleware gates the API in jwt mode: every non-public request must carry
// a valid bearer token. It mirrors the cookie Middleware's role + write-method
// gating so everything downstream (handlers, FromContext) is identical across
// modes.
func (a *Authenticator) jwtMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.isPublic(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		sess, role, err := a.bearerFromRequest(r)
		if err != nil {
			a.writeBearerError(w, err)
			return
		}
		if role != RoleAdmin && isWriteMethod(r.Method) {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"message": "write access requires the admin role",
				"role":    string(role),
			})
			return
		}
		next.ServeHTTP(w, withSession(r, sess, role))
	})
}

// writeBearerError maps a verifyBearer/bearerFromRequest error onto the right
// status: 403 when the token is valid but the subject isn't allowed, 401
// otherwise. The 401 carries a Bearer challenge so HTTP clients know what to
// present.
func (a *Authenticator) writeBearerError(w http.ResponseWriter, err error) {
	if errors.Is(err, errForbidden) {
		writeJSON(w, http.StatusForbidden, map[string]any{"message": "access denied"})
		return
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="cheshmhayash"`)
	writeJSON(w, http.StatusUnauthorized, map[string]any{"message": err.Error()})
}

// handleMeJWT is the jwt-mode identity probe. It verifies the request's bearer
// token directly (the middleware lets /api/auth/* through without injecting
// context) and returns the same shape as the cookie-mode handleMe so the SPA
// is mode-agnostic. The "mode" field lets the SPA hide the login affordance —
// there's nothing for the user to click in jwt mode.
func (a *Authenticator) handleMeJWT(w http.ResponseWriter, r *http.Request) {
	sess, role, err := a.bearerFromRequest(r)
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, errForbidden) {
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]any{"authenticated": false, "mode": "jwt"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"mode":          "jwt",
		"sub":           sess.Sub,
		"email":         sess.Email,
		"name":          sess.Name,
		"given_name":    sess.GivenName,
		"family_name":   sess.FamilyName,
		"groups":        sess.Groups,
		"role":          string(role),
	})
}
