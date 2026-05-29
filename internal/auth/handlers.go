package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	flowCookieName = "cheshmhayash_flow"
	flowTTL        = 10 * time.Minute
)

// Register attaches /api/auth/{login,callback,logout,me} to mux. Public —
// the middleware lets these through.
func (a *Authenticator) Register(mux *http.ServeMux) {
	if !a.Enabled() {
		return
	}
	mux.HandleFunc("GET /api/auth/login", a.handleLogin)
	mux.HandleFunc("GET /api/auth/callback", a.handleCallback)
	mux.HandleFunc("POST /api/auth/logout", a.handleLogout)
	mux.HandleFunc("GET /api/auth/me", a.handleMe)
}

func (a *Authenticator) handleLogin(w http.ResponseWriter, r *http.Request) {
	verifier := oauth2.GenerateVerifier()
	state := randString(24)
	nonce := randString(24)
	flow := flowData{
		State:    state,
		Nonce:    nonce,
		Verifier: verifier,
		ReturnTo: sanitizeReturnTo(r.URL.Query().Get("return_to")),
		Exp:      time.Now().Add(flowTTL).Unix(),
	}
	token, err := a.signer.signFlow(flow)
	if err != nil {
		http.Error(w, "sign flow: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     flowCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cfg.Session.Secure,
		// Lax (not Strict) so the cookie survives the top-level redirect
		// back from the IdP. None+Secure would also work but is overkill.
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(flowTTL.Seconds()),
	})
	authURL := a.oauth.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (a *Authenticator) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if errParam := q.Get("error"); errParam != "" {
		a.callbackError(w, r, "idp returned error: "+errParam+" "+q.Get("error_description"))
		return
	}
	code := q.Get("code")
	state := q.Get("state")
	if code == "" || state == "" {
		a.callbackError(w, r, "missing code or state")
		return
	}
	c, err := r.Cookie(flowCookieName)
	if err != nil {
		a.callbackError(w, r, "missing flow cookie — start over at /api/auth/login")
		return
	}
	flow, err := a.signer.readFlow(c.Value)
	if err != nil {
		a.callbackError(w, r, "invalid flow cookie: "+err.Error())
		return
	}
	clearCookie(w, flowCookieName, a.cfg.Session.Secure)

	if flow.State != state {
		a.callbackError(w, r, "state mismatch")
		return
	}

	tok, err := a.oauth.Exchange(r.Context(), code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		a.callbackError(w, r, "token exchange: "+err.Error())
		return
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		a.callbackError(w, r, "no id_token in response")
		return
	}
	idTok, err := a.verifier.Verify(r.Context(), rawID)
	if err != nil {
		a.callbackError(w, r, "id_token verify: "+err.Error())
		return
	}
	if idTok.Nonce != flow.Nonce {
		a.callbackError(w, r, "nonce mismatch")
		return
	}

	sess, err := a.sessionFromIDToken(idTok)
	if err != nil {
		a.callbackError(w, r, err.Error())
		return
	}
	role, ok := a.authorize(sess)
	if !ok {
		a.log.Warn("auth: rejected — not on allowlist", "email", sess.Email, "sub", sess.Sub)
		http.Error(w, "access denied: "+sess.Email+" is not on the allowlist", http.StatusForbidden)
		return
	}
	if err := a.writeSession(w, sess); err != nil {
		http.Error(w, "write session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	a.log.Info("auth: login", "email", sess.Email, "sub", sess.Sub, "role", role)
	http.Redirect(w, r, flow.ReturnTo, http.StatusFound)
}

func (a *Authenticator) handleLogout(w http.ResponseWriter, _ *http.Request) {
	clearCookie(w, a.cfg.Session.CookieName, a.cfg.Session.Secure)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleMe returns 200 + identity (incl. role) when authenticated, 401
// otherwise. The SPA polls this on boot to decide whether to show the
// login splash and which write controls to render. Middleware lets
// /api/auth/* through without injecting context, so we resolve from the
// cookie directly here.
func (a *Authenticator) handleMe(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(a.cfg.Session.CookieName)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"authenticated": false})
		return
	}
	s, err := a.signer.readSession(c.Value)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"authenticated": false})
		return
	}
	role, ok := a.authorize(s)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"authenticated": false,
			"message":       "access denied",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"sub":           s.Sub,
		"email":         s.Email,
		"name":          s.Name,
		"given_name":    s.GivenName,
		"family_name":   s.FamilyName,
		"groups":        s.Groups,
		"role":          string(role),
	})
}

func (a *Authenticator) sessionFromIDToken(t *oidc.IDToken) (sessionData, error) {
	// Pull the claims we care about. Extra claims (groups + custom group
	// claim names) are read separately because the standard struct doesn't
	// know about the operator's chosen claim name.
	var std struct {
		Email      string `json:"email"`
		Name       string `json:"name"`
		GivenName  string `json:"given_name"`
		FamilyName string `json:"family_name"`
	}
	if err := t.Claims(&std); err != nil {
		return sessionData{}, errors.New("decode id_token claims: " + err.Error())
	}
	groups, err := extractGroups(t, a.cfg.Access.GroupsClaim)
	if err != nil {
		return sessionData{}, err
	}
	return sessionData{
		Sub:        t.Subject,
		Email:      std.Email,
		Name:       std.Name,
		GivenName:  std.GivenName,
		FamilyName: std.FamilyName,
		Groups:     groups,
		Exp:        time.Now().Add(a.cfg.Session.TTL()).Unix(),
	}, nil
}

func extractGroups(t *oidc.IDToken, claim string) ([]string, error) {
	if claim == "" {
		claim = "groups"
	}
	var all map[string]json.RawMessage
	if err := t.Claims(&all); err != nil {
		return nil, errors.New("decode claims: " + err.Error())
	}
	raw, ok := all[claim]
	if !ok {
		return nil, nil
	}
	// Some IdPs emit a string, others an array. Try array first.
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return []string{single}, nil
	}
	return nil, nil
}

func (a *Authenticator) writeSession(w http.ResponseWriter, s sessionData) error {
	token, err := a.signer.signSession(s)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     a.cfg.Session.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cfg.Session.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(a.cfg.Session.TTL().Seconds()),
	})
	return nil
}

func (a *Authenticator) callbackError(w http.ResponseWriter, _ *http.Request, msg string) {
	a.log.Warn("auth callback error", "msg", msg)
	clearCookie(w, flowCookieName, a.cfg.Session.Secure)
	http.Error(w, "login failed: "+msg, http.StatusBadRequest)
}

func clearCookie(w http.ResponseWriter, name string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func randString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// sanitizeReturnTo rejects absolute URLs and protocol-relative paths so
// the IdP redirect can't be turned into an open-redirect.
func sanitizeReturnTo(s string) string {
	if s == "" {
		return "/"
	}
	if strings.HasPrefix(s, "//") || strings.Contains(s, "://") {
		return "/"
	}
	if !strings.HasPrefix(s, "/") {
		return "/"
	}
	u, err := url.Parse(s)
	if err != nil || u.Host != "" {
		return "/"
	}
	return s
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Default().Error("auth: encode response", "err", err)
	}
}
