package auth

import (
	"strings"
	"testing"
	"time"
)

func TestSigner_RoundTrip(t *testing.T) {
	s := newSigner([]byte("not-a-real-secret-but-long-enough"))
	want := sessionData{
		Sub:    "abc",
		Email:  "alice@example.com",
		Name:   "Alice",
		Groups: []string{"admins"},
		Exp:    time.Now().Add(time.Hour).Unix(),
	}
	tok, err := s.signSession(want)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	got, err := s.readSession(tok)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Sub != want.Sub || got.Email != want.Email || len(got.Groups) != 1 {
		t.Fatalf("round-trip mismatch: %+v vs %+v", got, want)
	}
}

func TestSigner_RejectsTamper(t *testing.T) {
	s := newSigner([]byte("not-a-real-secret-but-long-enough"))
	tok, _ := s.signSession(sessionData{Sub: "abc", Exp: time.Now().Add(time.Hour).Unix()})
	// Flip a byte in the payload (before the dot).
	dot := strings.IndexByte(tok, '.')
	tampered := tok[:dot-1] + "A" + tok[dot:]
	if _, err := s.readSession(tampered); err == nil {
		t.Fatal("expected tampered token to be rejected")
	}
}

func TestSigner_RejectsExpired(t *testing.T) {
	s := newSigner([]byte("not-a-real-secret-but-long-enough"))
	tok, _ := s.signSession(sessionData{Sub: "abc", Exp: time.Now().Add(-time.Hour).Unix()})
	if _, err := s.readSession(tok); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestSigner_DifferentKeysReject(t *testing.T) {
	a := newSigner([]byte("not-a-real-secret-but-long-enough"))
	b := newSigner([]byte("a-totally-different-secret-value-yes"))
	tok, _ := a.signSession(sessionData{Sub: "abc", Exp: time.Now().Add(time.Hour).Unix()})
	if _, err := b.readSession(tok); err == nil {
		t.Fatal("token from key A must not verify under key B")
	}
}

func TestSanitizeReturnTo(t *testing.T) {
	cases := map[string]string{
		"":                    "/",
		"/":                   "/",
		"/streams":            "/streams",
		"/jsm?cluster=x":      "/jsm?cluster=x",
		"//evil.example.com":  "/",
		"https://evil.com":    "/",
		"javascript:alert(1)": "/",
		"streams":             "/", // missing leading slash
	}
	for in, want := range cases {
		if got := sanitizeReturnTo(in); got != want {
			t.Errorf("sanitizeReturnTo(%q) = %q, want %q", in, got, want)
		}
	}
}
