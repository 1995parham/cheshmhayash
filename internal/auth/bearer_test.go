package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"

	"github.com/1995parham/cheshmhayash/internal/config"
)

// jwtAuth builds an Authenticator wired for "jwt" mode against a local,
// in-memory signing key — no network, no real IdP. Returns the authenticator
// and a function that mints signed access-token JWTs with the given claims.
func jwtAuth(t *testing.T, jwt config.AuthJWT, access config.AuthAccess) (*Authenticator, func(map[string]any) string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	keySet := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{key.Public()}}
	verifier := oidc.NewVerifier(testIssuer, keySet, &oidc.Config{SkipClientIDCheck: true})

	a := &Authenticator{
		cfg: config.Auth{
			Enabled: true,
			Mode:    config.AuthModeJWT,
			OIDC:    config.AuthOIDC{Issuer: testIssuer},
			Access:  access,
			JWT:     jwt,
		},
		log:          slog.New(slog.DiscardHandler),
		jwtVerifier:  verifier,
		jwtAudiences: jwt.Audiences,
	}

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	mint := func(claims map[string]any) string {
		if _, ok := claims["iss"]; !ok {
			claims["iss"] = testIssuer
		}
		if _, ok := claims["exp"]; !ok {
			claims["exp"] = time.Now().Add(time.Hour).Unix()
		}
		buf, err := json.Marshal(claims)
		if err != nil {
			t.Fatalf("marshal claims: %v", err)
		}
		jws, err := signer.Sign(buf)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		raw, err := jws.CompactSerialize()
		if err != nil {
			t.Fatalf("serialize: %v", err)
		}
		return raw
	}
	return a, mint
}

func TestVerifyBearer(t *testing.T) {
	access := config.AuthAccess{
		AllowedDomains: []string{"snapp.cab"},
		Admin:          config.AccessRule{AllowedGroups: []string{"nats-admin"}},
	}
	a, mint := jwtAuth(t, config.AuthJWT{}, access)

	t.Run("admin group resolves to admin role", func(t *testing.T) {
		tok := mint(map[string]any{
			"sub": "u1", "email": "ops@snapp.cab", "groups": []string{"nats-admin"},
		})
		sess, role, err := a.verifyBearer(context.Background(), tok)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sess.Email != "ops@snapp.cab" || role != RoleAdmin {
			t.Fatalf("got %q/%q, want ops@snapp.cab/admin", sess.Email, role)
		}
	})

	t.Run("sign-in only resolves to readonly", func(t *testing.T) {
		tok := mint(map[string]any{"sub": "u2", "email": "dev@snapp.cab"})
		_, role, err := a.verifyBearer(context.Background(), tok)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if role != RoleReadOnly {
			t.Fatalf("got role %q, want readonly", role)
		}
	})

	t.Run("off-allowlist subject is forbidden", func(t *testing.T) {
		tok := mint(map[string]any{"sub": "x", "email": "stranger@gmail.com"})
		if _, _, err := a.verifyBearer(context.Background(), tok); err == nil {
			t.Fatal("off-allowlist subject must be rejected")
		}
	})

	t.Run("expired token rejected", func(t *testing.T) {
		tok := mint(map[string]any{
			"sub": "u1", "email": "ops@snapp.cab",
			"exp": time.Now().Add(-time.Hour).Unix(),
		})
		if _, _, err := a.verifyBearer(context.Background(), tok); err == nil {
			t.Fatal("expired token must be rejected")
		}
	})
}

func TestVerifyBearer_Audience(t *testing.T) {
	access := config.AuthAccess{AllowedDomains: []string{"snapp.cab"}}
	a, mint := jwtAuth(t, config.AuthJWT{Audiences: []string{"cheshmhayash"}}, access)

	t.Run("matching audience accepted", func(t *testing.T) {
		tok := mint(map[string]any{"sub": "u1", "email": "ops@snapp.cab", "aud": "cheshmhayash"})
		if _, _, err := a.verifyBearer(context.Background(), tok); err != nil {
			t.Fatalf("expected accept, got %v", err)
		}
	})
	t.Run("foreign audience rejected", func(t *testing.T) {
		tok := mint(map[string]any{"sub": "u1", "email": "ops@snapp.cab", "aud": "other"})
		if _, _, err := a.verifyBearer(context.Background(), tok); err == nil {
			t.Fatal("token with foreign audience must be rejected")
		}
	})
}

func TestJWTMiddleware(t *testing.T) {
	access := config.AuthAccess{
		AllowedDomains: []string{"snapp.cab"},
		Admin:          config.AccessRule{AllowedGroups: []string{"nats-admin"}},
	}
	a, mint := jwtAuth(t, config.AuthJWT{}, access)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := a.jwtMiddleware(next)

	do := func(method, path, authz string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(context.Background(), method, path, nil)
		if authz != "" {
			req.Header.Set("Authorization", authz)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	adminTok := mint(map[string]any{"sub": "a", "email": "ops@snapp.cab", "groups": []string{"nats-admin"}})
	roTok := mint(map[string]any{"sub": "r", "email": "dev@snapp.cab"})

	t.Run("public path bypasses auth", func(t *testing.T) {
		if rec := do(http.MethodGet, "/healthz", ""); rec.Code != http.StatusOK {
			t.Fatalf("healthz should pass without a token: got %d", rec.Code)
		}
	})
	t.Run("missing token 401", func(t *testing.T) {
		rec := do(http.MethodGet, "/api/admin/clusters", "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("missing token should 401: got %d", rec.Code)
		}
		if wa := rec.Header().Get("WWW-Authenticate"); wa == "" {
			t.Error("401 should carry a Bearer challenge")
		}
	})
	t.Run("admin GET passes", func(t *testing.T) {
		if rec := do(http.MethodGet, "/api/admin/clusters", "Bearer "+adminTok); rec.Code != http.StatusOK {
			t.Fatalf("admin GET should pass: got %d", rec.Code)
		}
	})
	t.Run("readonly GET passes, write forbidden", func(t *testing.T) {
		if rec := do(http.MethodGet, "/api/jsm/clusters/c/overview", "Bearer "+roTok); rec.Code != http.StatusOK {
			t.Fatalf("readonly GET should pass: got %d", rec.Code)
		}
		if rec := do(http.MethodPost, "/api/jsm/clusters/c/streams/s/actions/stepdown", "Bearer "+roTok); rec.Code != http.StatusForbidden {
			t.Fatalf("readonly write should 403: got %d", rec.Code)
		}
	})
	t.Run("admin write passes", func(t *testing.T) {
		if rec := do(http.MethodPost, "/api/jsm/clusters/c/streams/s/actions/stepdown", "Bearer "+adminTok); rec.Code != http.StatusOK {
			t.Fatalf("admin write should pass: got %d", rec.Code)
		}
	})
	t.Run("off-allowlist 403", func(t *testing.T) {
		stranger := mint(map[string]any{"sub": "x", "email": "stranger@gmail.com"})
		if rec := do(http.MethodGet, "/api/admin/clusters", "Bearer "+stranger); rec.Code != http.StatusForbidden {
			t.Fatalf("off-allowlist should 403: got %d", rec.Code)
		}
	})
}

func TestHandleMeJWT(t *testing.T) {
	access := config.AuthAccess{AllowedDomains: []string{"snapp.cab"}}
	a, mint := jwtAuth(t, config.AuthJWT{}, access)

	t.Run("valid token reports identity + mode", func(t *testing.T) {
		tok := mint(map[string]any{"sub": "u1", "email": "ops@snapp.cab"})
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		a.handleMeJWT(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var body struct {
			Authenticated bool   `json:"authenticated"`
			Mode          string `json:"mode"`
			Email         string `json:"email"`
			Role          string `json:"role"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !body.Authenticated || body.Mode != "jwt" || body.Email != "ops@snapp.cab" || body.Role != string(RoleAdmin) {
			t.Fatalf("unexpected body: %+v", body)
		}
	})

	t.Run("no token reports anonymous + mode", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/auth/me", nil)
		rec := httptest.NewRecorder()
		a.handleMeJWT(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		var body struct {
			Authenticated bool   `json:"authenticated"`
			Mode          string `json:"mode"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Authenticated || body.Mode != "jwt" {
			t.Fatalf("unexpected body: %+v", body)
		}
	})
}
