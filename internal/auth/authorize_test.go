package auth

import (
	"testing"

	"github.com/1995parham/cheshmhayash/internal/config"
)

// auth builds an Authenticator with just the Access config populated —
// enough to exercise authorize without an OIDC provider.
func authFor(access config.AuthAccess) *Authenticator {
	return &Authenticator{cfg: config.Auth{Access: access}}
}

func TestAuthorize_NoAdminAllowlist_GrantsAdmin(t *testing.T) {
	// Backward-compatible: with no admin allowlist, every signed-in user is
	// an admin (full access, same as before roles existed).
	a := authFor(config.AuthAccess{AllowedDomains: []string{"snappcloud.io"}})

	role, ok := a.authorize(sessionData{Email: "ops@snappcloud.io"})
	if !ok || role != RoleAdmin {
		t.Fatalf("want admin/true, got %q/%v", role, ok)
	}

	if _, ok := a.authorize(sessionData{Email: "intruder@example.com"}); ok {
		t.Fatal("non-allowlisted email must be denied")
	}
}

func TestAuthorize_AdminAllowlist_SplitsTiers(t *testing.T) {
	a := authFor(config.AuthAccess{
		AllowedDomains: []string{"snapp.cab"},
		Admin:          config.AccessRule{AllowedDomains: []string{"snappcloud.io"}},
	})

	cases := []struct {
		email    string
		groups   []string
		wantRole Role
		wantOK   bool
	}{
		{email: "platform@snappcloud.io", wantRole: RoleAdmin, wantOK: true},
		{email: "employee@snapp.cab", wantRole: RoleReadOnly, wantOK: true},
		{email: "stranger@gmail.com", wantOK: false},
	}
	for _, c := range cases {
		role, ok := a.authorize(sessionData{Email: c.email, Groups: c.groups})
		if ok != c.wantOK || (ok && role != c.wantRole) {
			t.Errorf("authorize(%s) = %q/%v, want %q/%v", c.email, role, ok, c.wantRole, c.wantOK)
		}
	}
}

func TestAuthorize_AdminNotOnBaseList_StillAdmin(t *testing.T) {
	// An admin matched only by the admin allowlist (not the sign-in list)
	// must still be let in — authorize ORs the two.
	a := authFor(config.AuthAccess{
		AllowedDomains: []string{"snapp.cab"},
		Admin:          config.AccessRule{AllowedEmails: []string{"contractor@external.com"}},
	})
	role, ok := a.authorize(sessionData{Email: "contractor@external.com"})
	if !ok || role != RoleAdmin {
		t.Fatalf("want admin/true, got %q/%v", role, ok)
	}
}

func TestAuthorize_AdminByGroup(t *testing.T) {
	a := authFor(config.AuthAccess{
		AllowedDomains: []string{"snapp.cab"},
		Admin:          config.AccessRule{AllowedGroups: []string{"nats-admins"}},
	})
	role, ok := a.authorize(sessionData{Email: "lead@snapp.cab", Groups: []string{"nats-admins", "eng"}})
	if !ok || role != RoleAdmin {
		t.Fatalf("group admin: want admin/true, got %q/%v", role, ok)
	}
	role, ok = a.authorize(sessionData{Email: "lead@snapp.cab", Groups: []string{"eng"}})
	if !ok || role != RoleReadOnly {
		t.Fatalf("non-admin group: want readonly/true, got %q/%v", role, ok)
	}
}

func TestIsWriteMethod(t *testing.T) {
	for m, want := range map[string]bool{
		"GET": false, "HEAD": false, "OPTIONS": false,
		"POST": true, "PUT": true, "PATCH": true, "DELETE": true,
	} {
		if got := isWriteMethod(m); got != want {
			t.Errorf("isWriteMethod(%s) = %v, want %v", m, got, want)
		}
	}
}
