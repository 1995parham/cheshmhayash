package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// ServeHTTP implements the MCP "Streamable HTTP" transport at a single
// endpoint. The two methods of interest:
//
//   - POST /mcp  — client sends one JSON-RPC frame in the body; server
//     returns the JSON-RPC response (or 204 for notifications).
//     Content-Type: application/json on both directions.
//   - GET  /mcp  — opens an SSE stream the server can push notifications
//     through. We have no server-initiated notifications
//     today, so the stream is a keep-alive that stays open
//     until the client disconnects. Bare minimum to keep
//     spec-conformant clients happy.
//
// No authentication is enforced — the operator is expected to gate the
// `/mcp` path with a reverse-proxy bearer/mTLS rule when exposing it.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.servePost(w, r)
	case http.MethodGet:
		s.serveSSE(w, r)
	case http.MethodOptions:
		w.Header().Set("Allow", "GET, POST, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, POST, OPTIONS")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) servePost(w http.ResponseWriter, r *http.Request) {
	// Cap the body to a reasonable size — JSON-RPC requests are tiny;
	// blowing 1 MiB is almost certainly an attack or a misuse.
	const maxBody = 1 << 20
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Honor the request's context for tool calls so a client disconnect
	// cancels in-flight NATS requests.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	resp := s.dispatch(ctx, body)
	if resp == nil {
		// Notification — JSON-RPC spec says respond with no body.
		w.WriteHeader(http.StatusAccepted)
		return
	}

	buf, err := json.Marshal(resp)
	if err != nil {
		s.log.Error("mcp http: marshal response", "err", err)
		http.Error(w, "encode response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write(buf); err != nil && !errors.Is(err, context.Canceled) {
		s.log.Warn("mcp http: write response", "err", err)
	}
}

// serveSSE answers GET /mcp with an SSE stream that stays open until the
// client disconnects. We send a comment heartbeat every 30s so corporate
// proxies don't kill the idle connection.
func (s *Server) serveSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx
	w.WriteHeader(http.StatusOK)

	// Initial comment so the client sees the stream open immediately.
	if _, err := io.WriteString(w, ": mcp ready\n\n"); err != nil {
		return
	}
	flusher.Flush()

	ctx := r.Context()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := io.WriteString(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
