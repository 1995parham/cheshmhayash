package handler

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The request log must stay quiet for probe traffic at the default Info
// level: /healthz is hit every few seconds by kubelet probes and every
// line counts against the pod's ephemeral-storage limit.
func TestRequestLogSkipsHealthzAtInfo(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	h := requestLog(log, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	if buf.Len() != 0 {
		t.Fatalf("healthz was logged at Info: %q", buf.String())
	}

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/version", nil))
	if !strings.Contains(buf.String(), "path=/api/version") || !strings.Contains(buf.String(), "status=204") {
		t.Fatalf("regular request not logged: %q", buf.String())
	}
}

// With LOG_LEVEL=debug the probe lines are still available.
func TestRequestLogKeepsHealthzAtDebug(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := requestLog(log, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	if !strings.Contains(buf.String(), "level=DEBUG") || !strings.Contains(buf.String(), "path=/healthz") {
		t.Fatalf("healthz not logged at Debug: %q", buf.String())
	}
}
