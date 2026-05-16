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
	return &Authenticator{
		cfg:      cfg,
		log:      log,
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.OIDC.ClientID}),
		oauth:    oauthCfg,
		signer:   newSigner([]byte(cfg.Session.Secret)),
	}, nil
}

// Enabled reports whether the authenticator should be applied. A nil
// receiver counts as disabled so call sites don't need a separate nil check.
func (a *Authenticator) Enabled() bool {
	return a != nil && a.cfg.Enabled
}

// allowed checks the post-login allowlist. Empty slices are ignored
// (Settings.validate already requires at least one of the three).
func (a *Authenticator) allowed(s sessionData) bool {
	for _, e := range a.cfg.Access.AllowedEmails {
		if strings.EqualFold(e, s.Email) {
			return true
		}
	}
	if domain := domainOf(s.Email); domain != "" {
		for _, d := range a.cfg.Access.AllowedDomains {
			if strings.EqualFold(d, domain) {
				return true
			}
		}
	}
	for _, want := range a.cfg.Access.AllowedGroups {
		for _, have := range s.Groups {
			if strings.EqualFold(want, have) {
				return true
			}
		}
	}
	return false
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
// the cookie format.
type Identity struct {
	Sub        string
	Email      string
	Name       string
	GivenName  string
	FamilyName string
	Groups     []string
}

// FromContext returns the identity attached by the middleware, or zero if
// none — useful for /api/auth/me.
func FromContext(ctx context.Context) (Identity, bool) {
	s, ok := ctx.Value(sessionKey).(sessionData)
	if !ok {
		return Identity{}, false
	}
	return Identity{
		Sub:        s.Sub,
		Email:      s.Email,
		Name:       s.Name,
		GivenName:  s.GivenName,
		FamilyName: s.FamilyName,
		Groups:     s.Groups,
	}, true
}

// withSession returns a new request whose context carries the verified
// session payload.
func withSession(r *http.Request, s sessionData) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), sessionKey, s))
}
