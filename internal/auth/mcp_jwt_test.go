package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/1995parham/cheshmhayash/internal/config"
)

// mintUnsigned builds a structurally valid JWT whose signature is garbage —
// exactly what the unverified path must accept and the verified path must
// reject.
func mintUnsigned(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	buf, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(buf)
	return header + "." + payload + ".not-a-real-signature"
}

func mcpJWTAuth(access config.AuthAccess) *Authenticator {
	return &Authenticator{
		cfg: config.Auth{
			Enabled: true,
			Access:  access,
			MCPJWT:  config.AuthMCPJWT{Enabled: true},
		},
		log: slog.New(slog.DiscardHandler),
	}
}

func TestMCPJWTSession(t *testing.T) {
	a := mcpJWTAuth(config.AuthAccess{
		AllowedDomains: []string{"snapp.cab"},
		Admin:          config.AccessRule{AllowedGroups: []string{"/snapp-kc/Cloud"}},
	})

	t.Run("unsigned token accepted, claims drive role", func(t *testing.T) {
		tok := mintUnsigned(t, map[string]any{
			"sub": "u1", "email": "ops@snapp.cab", "groups": []string{"/snapp-kc/Cloud"},
		})
		sess, role, err := a.mcpJWTSession(tok)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sess.Email != "ops@snapp.cab" || role != RoleAdmin {
			t.Fatalf("got %q/%q, want ops@snapp.cab/admin", sess.Email, role)
		}
	})

	t.Run("sign-in only resolves readonly", func(t *testing.T) {
		tok := mintUnsigned(t, map[string]any{"sub": "u2", "email": "dev@snapp.cab"})
		_, role, err := a.mcpJWTSession(tok)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if role != RoleReadOnly {
			t.Fatalf("got role %q, want readonly", role)
		}
	})

	t.Run("allowlist still enforced", func(t *testing.T) {
		tok := mintUnsigned(t, map[string]any{"sub": "x", "email": "stranger@gmail.com"})
		if _, _, err := a.mcpJWTSession(tok); err == nil {
			t.Fatal("off-allowlist subject must be rejected")
		}
	})

	t.Run("string groups claim accepted", func(t *testing.T) {
		tok := mintUnsigned(t, map[string]any{
			"sub": "u3", "email": "ops@snapp.cab", "groups": "/snapp-kc/Cloud",
		})
		_, role, err := a.mcpJWTSession(tok)
		if err != nil || role != RoleAdmin {
			t.Fatalf("single-string group should grant admin, got %q/%v", role, err)
		}
	})

	t.Run("non-JWT rejected", func(t *testing.T) {
		for _, bad := range []string{"random-token", "a.b", "a.!!!.c"} {
			if _, _, err := a.mcpJWTSession(bad); err == nil {
				t.Fatalf("%q must be rejected", bad)
			}
		}
	})
}

func TestMCPMiddleware_UnverifiedJWT(t *testing.T) {
	a := mcpJWTAuth(config.AuthAccess{AllowedDomains: []string{"snapp.cab"}})

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	keys := []KeyMatcher{{Name: "ci", Value: "static-secret"}}
	h := a.MCPMiddleware(keys, ok)

	do := func(authz string) int {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/mcp", nil)
		if authz != "" {
			req.Header.Set("Authorization", authz)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := do("Bearer static-secret"); got != http.StatusOK {
		t.Errorf("static key should still pass: got %d", got)
	}
	tok := mintUnsigned(t, map[string]any{"sub": "u1", "email": "ops@snapp.cab"})
	if got := do("Bearer " + tok); got != http.StatusOK {
		t.Errorf("unverified jwt on allowlist should pass: got %d", got)
	}
	bad := mintUnsigned(t, map[string]any{"sub": "x", "email": "stranger@gmail.com"})
	if got := do("Bearer " + bad); got != http.StatusUnauthorized {
		t.Errorf("off-allowlist jwt should 401: got %d", got)
	}
	if got := do(""); got != http.StatusUnauthorized {
		t.Errorf("no creds should 401: got %d", got)
	}
}
