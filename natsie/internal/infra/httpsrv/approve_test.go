package httpsrv

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/1995parham/natsie/internal/cleanup"
	"github.com/1995parham/natsie/internal/infra/store"
	"github.com/1995parham/natsie/internal/manifest"
)

func newApprovalServer(t *testing.T, signingKey string) (*Server, store.Store) {
	t.Helper()
	st, err := store.NewFile(t.TempDir())
	if err != nil {
		t.Fatalf("NewFile: %v", err)
	}
	connector := func(string) (*nats.Conn, func(), error) {
		// Tests don't reach this — applyManifest is stubbed.
		return nil, func() {}, nil
	}
	return New(":0", st, Options{SigningKey: signingKey, Connector: connector}, log.New(io.Discard, "", 0)), st
}

func TestSignApprovalDeterministic(t *testing.T) {
	a := signApproval("k", "m-1")
	b := signApproval("k", "m-1")
	if a != b {
		t.Errorf("HMAC not stable: %q != %q", a, b)
	}
	if a == signApproval("different-key", "m-1") {
		t.Errorf("HMAC ignored key")
	}
	if a == signApproval("k", "m-2") {
		t.Errorf("HMAC ignored manifest id")
	}
}

func TestApprovalPreviewRequiresToken(t *testing.T) {
	s, st := newApprovalServer(t, "key")
	if err := st.Put(context.Background(), "m-1", &manifest.Manifest{Version: manifest.Version, GeneratedAt: time.Now()}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/approve/m-1?token=wrong", nil)
	s.e.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}
}

func TestApprovalPreviewHappyPath(t *testing.T) {
	s, st := newApprovalServer(t, "key")
	m := &manifest.Manifest{
		Version:     manifest.Version,
		GeneratedAt: time.Now(),
		Entries: []manifest.Entry{{
			Cluster:    "c",
			Stream:     "s",
			Consumer:   "stale-one",
			NumPending: 10,
		}},
	}
	if err := st.Put(context.Background(), "m-1", m); err != nil {
		t.Fatalf("Put: %v", err)
	}
	tok := s.SignApproval("m-1")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/approve/m-1?token="+tok, nil)
	s.e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), "stale-one") {
		t.Errorf("preview missing entry: %s", rec.Body.String())
	}
}

func TestApprovalDoBadToken(t *testing.T) {
	s, _ := newApprovalServer(t, "key")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/approve/m-1?token=wrong", nil)
	s.e.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}
}

func TestApprovalDoHappyPath(t *testing.T) {
	s, st := newApprovalServer(t, "key")
	m := &manifest.Manifest{
		Version:     manifest.Version,
		GeneratedAt: time.Now(),
		Entries: []manifest.Entry{{Cluster: "c", Stream: "s", Consumer: "x"}},
	}
	if err := st.Put(context.Background(), "m-1", m); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Stub the apply bridge so we don't try to connect to NATS.
	origApply := applyManifest
	t.Cleanup(func() { applyManifest = origApply })
	applyManifest = func(_ context.Context, _ *manifest.Manifest, _ cleanup.Connector) (*cleanup.Result, error) {
		return &cleanup.Result{Deleted: 1, Events: []cleanup.Event{{
			Cluster: "c", Stream: "s", Consumer: "x", Action: cleanup.ActionDeleted,
		}}}, nil
	}

	tok := s.SignApproval("m-1")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/approve/m-1?token="+tok, nil)
	s.e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Summary string           `json:"summary"`
		Events  []cleanup.Event  `json:"events"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !contains(body.Summary, "1 deleted") {
		t.Errorf("summary=%q", body.Summary)
	}
	if len(body.Events) != 1 || body.Events[0].Action != cleanup.ActionDeleted {
		t.Errorf("events=%+v", body.Events)
	}
}

func TestApprovalDisabledWithoutKey(t *testing.T) {
	st, _ := store.NewFile(t.TempDir())
	connector := func(string) (*nats.Conn, func(), error) { return nil, func() {}, nil }
	s := New(":0", st, Options{Connector: connector}, log.New(io.Discard, "", 0))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/approve/m-1?token=anything", nil)
	s.e.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("expected non-200 when approval disabled; got %d", rec.Code)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
