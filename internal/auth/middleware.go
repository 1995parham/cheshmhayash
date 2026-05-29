package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// Middleware gates routes behind a valid session cookie. /api/auth/*
// stays public so the SPA can check status and redirect to login.
//
// Returns next unchanged when the authenticator is disabled — keeps the
// caller cheap for the common "auth-off" case.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	if !a.Enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.isPublic(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(a.cfg.Session.CookieName)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"message": "authentication required",
			})
			return
		}
		sess, err := a.signer.readSession(c.Value)
		if err != nil {
			clearCookie(w, a.cfg.Session.CookieName, a.cfg.Session.Secure)
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"message": "invalid session: " + err.Error(),
			})
			return
		}
		// Cookie outlived the allowlist — re-checks every request so
		// kicking someone (or changing their tier) is as simple as editing
		// settings.toml; the next request re-resolves it.
		role, ok := a.authorize(sess)
		if !ok {
			clearCookie(w, a.cfg.Session.CookieName, a.cfg.Session.Secure)
			writeJSON(w, http.StatusForbidden, map[string]any{
				"message": "access denied",
			})
			return
		}
		// Read-only sessions may issue safe requests only. Every mutating
		// API route is POST/PUT/PATCH/DELETE, so gating by method is
		// sufficient — and resilient to new write routes being added.
		// Public paths (/api/auth/*, /mcp, healthz, version, SPA) already
		// returned above via isPublic, so this only fences /api/admin and
		// /api/jsm mutations.
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

// isWriteMethod reports whether an HTTP method mutates state. OPTIONS is
// short-circuited by the CORS middleware before it reaches here.
func isWriteMethod(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// isPublic lists the path prefixes that bypass the session check. SPA
// assets, healthz, and the auth endpoints themselves are public.
func (a *Authenticator) isPublic(path string) bool {
	switch {
	case path == "/healthz":
		return true
	case path == "/api/version":
		return true
	case strings.HasPrefix(path, "/api/auth/"):
		return true
	case strings.HasPrefix(path, "/api/"):
		return false
	case path == "/mcp", strings.HasPrefix(path, "/mcp/"):
		// /mcp has its own bearer-token middleware.
		return true
	}
	// Anything else (the SPA itself, /banner.png, /assets/*) is public —
	// the SPA does the redirect-to-login dance on the client side via
	// /api/auth/me.
	return true
}

// MCPMiddleware checks the Authorization header against the configured
// bearer-token list. When the list is empty the endpoint stays open — the
// operator opts in by listing keys, so MCP auth is independent of the
// dashboard auth flag.
func MCPMiddleware(keys []KeyMatcher, next http.Handler) http.Handler {
	if len(keys) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr := r.Header.Get("Authorization")
		token := strings.TrimSpace(strings.TrimPrefix(hdr, "Bearer "))
		if hdr == "" || token == hdr {
			// Either no header at all, or a non-Bearer scheme.
			unauthorizedMCP(w)
			return
		}
		for _, k := range keys {
			if subtle.ConstantTimeCompare([]byte(token), []byte(k.Value)) == 1 {
				next.ServeHTTP(w, r)
				return
			}
		}
		unauthorizedMCP(w)
	})
}

// KeyMatcher is the loose shape MCPMiddleware needs — kept independent of
// the config package so the auth package doesn't depend on config for the
// MCP path. main wires the conversion.
type KeyMatcher struct {
	Name  string
	Value string
}

func unauthorizedMCP(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="cheshmhayash"`)
	writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "bearer token required"})
}
