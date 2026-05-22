package httpsrv

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"testing"
	"time"

	"github.com/1995parham/natsie/internal/infra/store"
	"github.com/1995parham/natsie/internal/manifest"
)

func newSlashServer(t *testing.T, token string) (*Server, store.Store) {
	t.Helper()

	st, err := store.NewFile(t.TempDir())
	if err != nil {
		t.Fatalf("NewFile: %v", err)
	}

	return New(":0", st, Options{SigningKey: token}, log.New(io.Discard, "", 0)), st
}

func doSlash(s *Server, form neturl.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/slash", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	s.e.ServeHTTP(rec, req)

	return rec
}

func decodeSlash(t *testing.T, rec *httptest.ResponseRecorder) slashResponse {
	t.Helper()

	var got slashResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	return got
}

func TestSlashRequiresToken(t *testing.T) {
	s, _ := newSlashServer(t, "secret-token")

	rec := doSlash(s, neturl.Values{"token": {"wrong"}, "text": {"list"}})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}
}

func TestSlashHelpOnEmptyText(t *testing.T) {
	s, _ := newSlashServer(t, "secret-token")

	rec := doSlash(s, neturl.Values{"token": {"secret-token"}, "text": {""}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}

	got := decodeSlash(t, rec)
	if !strings.Contains(got.Text, "/natsie list") {
		t.Errorf("help missing: %q", got.Text)
	}
}

func TestSlashListEmpty(t *testing.T) {
	s, _ := newSlashServer(t, "tok")
	rec := doSlash(s, neturl.Values{"token": {"tok"}, "text": {"list"}})

	got := decodeSlash(t, rec)
	if !strings.Contains(got.Text, "no manifests") {
		t.Errorf("text=%q", got.Text)
	}
}

func TestSlashListAndShow(t *testing.T) {
	s, st := newSlashServer(t, "tok")

	m := &manifest.Manifest{
		Version:     manifest.Version,
		GeneratedAt: time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC),
		Scan:        manifest.ScanInfo{Context: "snapp-js-main-teh1"},
		Entries: []manifest.Entry{{
			Cluster:    "snapp-js-main-teh1",
			Stream:     "rides",
			Consumer:   "stale-one",
			Status:     "STALE",
			NumPending: 1000,
		}},
	}
	if err := st.Put(context.Background(), "m-1", m); err != nil {
		t.Fatalf("Put: %v", err)
	}

	list := decodeSlash(t, doSlash(s, neturl.Values{"token": {"tok"}, "text": {"list"}}))
	if !strings.Contains(list.Text, "m-1") {
		t.Errorf("list missing id: %q", list.Text)
	}

	show := decodeSlash(t, doSlash(s, neturl.Values{"token": {"tok"}, "text": {"show m-1"}}))
	if !strings.Contains(show.Text, "stale-one") {
		t.Errorf("show missing entry: %q", show.Text)
	}
}

func TestSlashShowMissing(t *testing.T) {
	s, _ := newSlashServer(t, "tok")
	rec := doSlash(s, neturl.Values{"token": {"tok"}, "text": {"show absent-id"}})

	got := decodeSlash(t, rec)
	if !strings.Contains(got.Text, "not found") {
		t.Errorf("show missing should say not-found: %q", got.Text)
	}
}

func TestSlashUnknownSubcommand(t *testing.T) {
	s, _ := newSlashServer(t, "tok")

	got := decodeSlash(t, doSlash(s, neturl.Values{"token": {"tok"}, "text": {"frobnicate"}}))
	if !strings.Contains(got.Text, "unknown subcommand") {
		t.Errorf("text=%q", got.Text)
	}
}

func TestSlashDisabledWithoutToken(t *testing.T) {
	// New(..., "", ...) must not register /slash.
	s, _ := newSlashServer(t, "")

	rec := doSlash(s, neturl.Values{"token": {""}, "text": {"list"}})
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 404/405 when slash disabled, got %d", rec.Code)
	}
}
