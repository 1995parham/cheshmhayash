// Package handler exposes cheshmhayash's HTTP API. Routes are registered on
// the stdlib http.ServeMux (Go 1.22+ pattern syntax with method+path).
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/nats-io/nats.go"

	"github.com/1995parham/cheshmhayash/internal/auth"
	"github.com/1995parham/cheshmhayash/internal/natsx"
	"github.com/1995parham/cheshmhayash/internal/version"
)

// Mux returns a fully wired ServeMux covering /api/admin, /api/jsm,
// /healthz, an optional /mcp endpoint, and a static-file SPA fallback at /.
//
// mcpHandler is optional — pass nil to skip /mcp registration. Likewise
// cache is optional; when nil, /api/jsm/.../overview falls back to a
// live NATS call on every request and /overview/stream returns 503.
//
// authn is optional. When non-nil and Enabled, /api/auth/* endpoints are
// registered and all /api/* routes are wrapped in the session-check
// middleware. mcpKeys, when non-empty, gates /mcp with bearer-token auth
// (independent of authn — MCP keys work even with OIDC disabled).
func Mux(
	mgr *natsx.Manager,
	cache *natsx.OverviewCache,
	staticDir string,
	logger *slog.Logger,
	mcpHandler http.Handler,
	authn *auth.Authenticator,
	mcpKeys []auth.KeyMatcher,
) http.Handler {
	mux := http.NewServeMux()

	admin := admin{mgr: mgr, log: logger}
	admin.register(mux)

	jsm := jsm{mgr: mgr, cache: cache, log: logger}
	jsm.register(mux)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// /api/version is intentionally public (see auth.isPublic) so the SPA
	// footer and the login screen can render the build tag pre-auth.
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": version.String()})
	})

	if authn.Enabled() {
		authn.Register(mux)
	}

	if mcpHandler != nil {
		mux.Handle("/mcp", auth.MCPMiddleware(mcpKeys, mcpHandler))
	}

	spa(mux, staticDir)

	var inner http.Handler = mux
	if authn.Enabled() {
		inner = authn.Middleware(mux)
	}
	return cors(requestLog(logger, inner))
}

// spa serves the built SPA from staticDir. Unknown paths fall back to
// index.html so client-side routes resolve.
func spa(mux *http.ServeMux, dir string) {
	fs := http.FileServer(http.Dir(dir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// If the requested file exists on disk, serve it. Otherwise fall
		// back to index.html.
		if r.URL.Path != "/" {
			f, err := http.Dir(dir).Open(strings.TrimPrefix(r.URL.Path, "/"))
			if err == nil {
				_ = f.Close()
				fs.ServeHTTP(w, r)
				return
			}
		}
		http.ServeFile(w, r, dir+"/index.html")
	})
}

// ----- errors / encoding helpers ---------------------------------------

type apiError struct {
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Default().Error("encode response", "err", err)
	}
}

// writeRaw streams an already-JSON payload back to the client without a
// second encode pass. Used for everything that comes straight from NATS.
func writeRaw(w http.ResponseWriter, status int, body json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeRawArray(w http.ResponseWriter, status int, items []json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if len(items) == 0 {
		_, _ = w.Write([]byte("[]"))
		return
	}
	_, _ = w.Write([]byte("["))
	for i, b := range items {
		if i > 0 {
			_, _ = w.Write([]byte(","))
		}
		_, _ = w.Write(b)
	}
	_, _ = w.Write([]byte("]"))
}

// upstreamError translates a NATS error to a 502 with a JSON body so the
// frontend can show it.
func upstreamError(w http.ResponseWriter, log *slog.Logger, err error) {
	log.Warn("upstream request failed", "err", err)
	status := http.StatusBadGateway
	if errors.Is(err, nats.ErrNoResponders) {
		status = http.StatusBadGateway
	} else if errors.Is(err, nats.ErrTimeout) {
		status = http.StatusGatewayTimeout
	}
	writeJSON(w, status, apiError{Message: err.Error()})
}

// ----- middleware ------------------------------------------------------

// cors mirrors actix-cors' allow-any behavior used by the previous Rust
// build. Tighten the origin list if exposing publicly.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestLog emits one slog line per request.
func requestLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"remote", r.RemoteAddr,
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(c int) { r.status = c; r.ResponseWriter.WriteHeader(c) }

// Flush passes through to the underlying writer so SSE (e.g. /mcp GET)
// keeps working — http.ResponseWriter loses its Flusher capability when
// wrapped unless the wrapper exposes it explicitly.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
