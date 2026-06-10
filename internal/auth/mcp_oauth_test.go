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
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"

	"github.com/1995parham/cheshmhayash/internal/config"
)

const testIssuer = "https://issuer.test"

// mcpOAuthAuth builds an Authenticator wired for MCP OAuth against a local,
// in-memory signing key — no network, no real IdP. Returns the authenticator
// and a function that mints signed access-token JWTs with the given claims.
func mcpOAuthAuth(t *testing.T, mcp config.AuthMCPOAuth, access config.AuthAccess) (*Authenticator, func(map[string]any) string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	keySet := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{key.Public()}}
	verifier := oidc.NewVerifier(testIssuer, keySet, &oidc.Config{SkipClientIDCheck: true})

	auds := mcp.Audiences
	if len(auds) == 0 {
		auds = []string{mcp.Resource}
	}
	a := &Authenticator{
		cfg: config.Auth{
			Enabled:  true,
			OIDC:     config.AuthOIDC{Issuer: testIssuer},
			Access:   access,
			MCPOAuth: mcp,
		},
		log:          slog.New(slog.DiscardHandler),
		mcpVerifier:  verifier,
		mcpAudiences: auds,
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

func TestVerifyMCPToken(t *testing.T) {
	mcp := config.AuthMCPOAuth{Enabled: true, Resource: "https://host/mcp"}
	access := config.AuthAccess{AllowedDomains: []string{"snapp.cab"}}
	a, mint := mcpOAuthAuth(t, mcp, access)

	t.Run("valid token on allowlist", func(t *testing.T) {
		tok := mint(map[string]any{
			"sub": "u1", "email": "ops@snapp.cab", "aud": "https://host/mcp",
		})
		sess, role, err := a.verifyMCPToken(context.Background(), tok)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sess.Email != "ops@snapp.cab" || role != RoleAdmin {
			t.Fatalf("got %q/%q, want ops@snapp.cab/admin", sess.Email, role)
		}
	})

	t.Run("wrong audience rejected", func(t *testing.T) {
		tok := mint(map[string]any{
			"sub": "u1", "email": "ops@snapp.cab", "aud": "https://someone-else/mcp",
		})
		if _, _, err := a.verifyMCPToken(context.Background(), tok); err == nil {
			t.Fatal("token with foreign audience must be rejected")
		}
	})

	t.Run("not on allowlist rejected", func(t *testing.T) {
		tok := mint(map[string]any{
			"sub": "x", "email": "stranger@gmail.com", "aud": "https://host/mcp",
		})
		if _, _, err := a.verifyMCPToken(context.Background(), tok); err == nil {
			t.Fatal("off-allowlist subject must be rejected")
		}
	})

	t.Run("expired token rejected", func(t *testing.T) {
		tok := mint(map[string]any{
			"sub": "u1", "email": "ops@snapp.cab", "aud": "https://host/mcp",
			"exp": time.Now().Add(-time.Hour).Unix(),
		})
		if _, _, err := a.verifyMCPToken(context.Background(), tok); err == nil {
			t.Fatal("expired token must be rejected")
		}
	})
}

func TestVerifyMCPToken_SkipAudienceCheck(t *testing.T) {
	mcp := config.AuthMCPOAuth{Enabled: true, Resource: "https://host/mcp", SkipAudienceCheck: true}
	access := config.AuthAccess{AllowedDomains: []string{"snapp.cab"}}
	a, mint := mcpOAuthAuth(t, mcp, access)

	t.Run("foreign audience accepted", func(t *testing.T) {
		tok := mint(map[string]any{
			"sub": "u1", "email": "ops@snapp.cab", "aud": "account",
		})
		if _, _, err := a.verifyMCPToken(context.Background(), tok); err != nil {
			t.Fatalf("expected accept with skip_audience_check, got %v", err)
		}
	})
	t.Run("no audience accepted", func(t *testing.T) {
		tok := mint(map[string]any{"sub": "u1", "email": "ops@snapp.cab"})
		if _, _, err := a.verifyMCPToken(context.Background(), tok); err != nil {
			t.Fatalf("expected accept with skip_audience_check, got %v", err)
		}
	})
	t.Run("allowlist still enforced", func(t *testing.T) {
		tok := mint(map[string]any{"sub": "x", "email": "stranger@gmail.com", "aud": "account"})
		if _, _, err := a.verifyMCPToken(context.Background(), tok); err == nil {
			t.Fatal("off-allowlist subject must still be rejected")
		}
	})
	t.Run("expired token still rejected", func(t *testing.T) {
		tok := mint(map[string]any{
			"sub": "u1", "email": "ops@snapp.cab",
			"exp": time.Now().Add(-time.Hour).Unix(),
		})
		if _, _, err := a.verifyMCPToken(context.Background(), tok); err == nil {
			t.Fatal("expired token must still be rejected")
		}
	})
}

func TestMCPMiddleware_Precedence(t *testing.T) {
	mcp := config.AuthMCPOAuth{Enabled: true, Resource: "https://host/mcp"}
	access := config.AuthAccess{AllowedDomains: []string{"snapp.cab"}}
	a, mint := mcpOAuthAuth(t, mcp, access)

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	keys := []KeyMatcher{{Name: "ci", Value: "static-secret"}}
	h := a.MCPMiddleware(keys, ok)

	do := func(authz string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/mcp", nil)
		if authz != "" {
			req.Header.Set("Authorization", authz)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := do("Bearer static-secret"); rec.Code != http.StatusOK {
		t.Errorf("static key should pass: got %d", rec.Code)
	}
	tok := mint(map[string]any{"sub": "u1", "email": "ops@snapp.cab", "aud": "https://host/mcp"})
	if rec := do("Bearer " + tok); rec.Code != http.StatusOK {
		t.Errorf("valid OIDC token should pass: got %d", rec.Code)
	}
	rec := do("")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no creds should 401: got %d", rec.Code)
	}
	if wa := rec.Header().Get("WWW-Authenticate"); !strings.Contains(wa, "resource_metadata=") {
		t.Errorf("401 should carry resource_metadata hint, got %q", wa)
	}
}

func TestResourceMetadataURL(t *testing.T) {
	cases := map[string]string{
		"https://host/mcp": "https://host/.well-known/oauth-protected-resource/mcp",
		"https://host":     "https://host/.well-known/oauth-protected-resource",
		"not-a-url":        wellKnownPRMPath,
	}
	for resource, want := range cases {
		a := &Authenticator{cfg: config.Auth{MCPOAuth: config.AuthMCPOAuth{Resource: resource}}}
		if got := a.resourceMetadataURL(); got != want {
			t.Errorf("resourceMetadataURL(%q) = %q, want %q", resource, got, want)
		}
	}
}

func TestResourceMetadataHandler(t *testing.T) {
	a := &Authenticator{cfg: config.Auth{
		OIDC:     config.AuthOIDC{Issuer: testIssuer},
		MCPOAuth: config.AuthMCPOAuth{Resource: "https://host/mcp"},
	}}
	rec := httptest.NewRecorder()
	a.handleResourceMetadata(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, wellKnownPRMPath, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var doc struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Resource != "https://host/mcp" {
		t.Errorf("resource = %q", doc.Resource)
	}
	if len(doc.AuthorizationServers) != 1 || doc.AuthorizationServers[0] != testIssuer {
		t.Errorf("authorization_servers = %v, want [%s]", doc.AuthorizationServers, testIssuer)
	}
}
