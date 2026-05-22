package httpsrv

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"gopkg.in/yaml.v3"

	"github.com/1995parham/natsie/internal/infra/store"
	"github.com/1995parham/natsie/internal/manifest"
)

func newTestServer(t *testing.T) (*Server, store.Store) {
	t.Helper()
	st, err := store.NewFile(t.TempDir())
	if err != nil {
		t.Fatalf("NewFile: %v", err)
	}
	logger := log.New(io.Discard, "", 0)
	return New(":0", st, "", logger), st
}

func doRequest(s *Server, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	s.e.ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	s, _ := newTestServer(t)
	rec := doRequest(s, http.MethodGet, "/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", rec.Code)
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("status=%q want ok", body["status"])
	}
}

func TestGetManifestRoundTrip(t *testing.T) {
	s, st := newTestServer(t)
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

	rec := doRequest(s, http.MethodGet, "/manifest/m-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get(echo.HeaderContentType)
	if !strings.HasPrefix(ct, "application/yaml") {
		t.Errorf("content-type=%q want application/yaml", ct)
	}
	var got manifest.Manifest
	if err := yaml.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	if len(got.Entries) != 1 || got.Entries[0].Consumer != "stale-one" {
		t.Errorf("unexpected manifest: %+v", got)
	}
}

func TestGetManifestNotFound(t *testing.T) {
	s, _ := newTestServer(t)
	rec := doRequest(s, http.MethodGet, "/manifest/never-existed")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404", rec.Code)
	}
}

func TestGetManifestInvalidID(t *testing.T) {
	s, _ := newTestServer(t)
	// The router won't accept paths containing a slash, so we craft one
	// that's syntactically a single path segment but trips the store's
	// validator (leading dot).
	rec := doRequest(s, http.MethodGet, "/manifest/.hidden")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
