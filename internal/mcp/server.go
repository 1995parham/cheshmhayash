// Package mcp exposes cheshmhayash's NATS admin surface as a Model Context
// Protocol server. Two transports are supported:
//
//   - stdio (RunStdio) — newline-delimited JSON-RPC 2.0, what every
//     local MCP client (Claude Desktop, Cursor, Continue, …) speaks.
//   - HTTP (ServeHTTP) — MCP "Streamable HTTP" transport: POST /mcp for
//     a synchronous JSON response, GET /mcp for an SSE keep-alive
//     (no server-initiated notifications today).
//
// The same natsx.Manager that backs the HTTP API is reused here, so tool
// calls hit NATS through the operator-configured connections.
//
// Destructive tools (purge/delete/kick/reload/lame-duck/update) are only
// registered when CHESHMHAYASH_MCP_WRITE=1. Read tools are always on.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/1995parham/cheshmhayash/internal/natsx"
)

// protocolVersion is the MCP spec version this server claims. Clients send
// their own version on initialize; we echo back what we support.
const protocolVersion = "2024-11-05"

// Server is a stdio JSON-RPC 2.0 loop dispatching MCP methods. One Server
// instance is bound to one read/write pair (typically stdin/stdout).
type Server struct {
	mgr   *natsx.Manager
	log   *slog.Logger
	tools []tool
	write bool

	out   io.Writer
	outMu sync.Mutex
}

// NewServer wires the tool registry. write=true enables mutating tools.
func NewServer(mgr *natsx.Manager, log *slog.Logger, write bool) *Server {
	s := &Server{mgr: mgr, log: log, write: write}
	s.tools = buildTools(write)
	return s
}

// Serve runs the JSON-RPC loop until the input closes or ctx is cancelled.
// Returns nil on graceful EOF.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	s.out = out

	// Stdio framing: one JSON message per line. A long stream-info payload
	// from NATS can blow past bufio's default 64 KB token cap, so size up.
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	lines := make(chan []byte)
	errs := make(chan error, 1)
	go func() {
		defer close(lines)
		for scanner.Scan() {
			b := scanner.Bytes()
			if len(b) == 0 {
				continue
			}
			cp := make([]byte, len(b))
			copy(cp, b)
			lines <- cp
		}
		if err := scanner.Err(); err != nil {
			errs <- err
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errs:
			return err
		case line, ok := <-lines:
			if !ok {
				return nil
			}
			s.handleLine(ctx, line)
		}
	}
}

// ----- JSON-RPC types --------------------------------------------------

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// JSON-RPC 2.0 reserved error codes (only the ones we actually return).
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

func (s *Server) handleLine(ctx context.Context, line []byte) {
	if resp := s.dispatch(ctx, line); resp != nil {
		s.writeMessage(*resp)
	}
}

// dispatch parses one JSON-RPC frame and returns the response, or nil for
// notifications. Pure function over the request bytes — no I/O side
// effects — so it's shared by stdio (handleLine) and HTTP (ServeHTTP).
func (s *Server) dispatch(ctx context.Context, frame []byte) *rpcResponse {
	var req rpcRequest
	if err := json.Unmarshal(frame, &req); err != nil {
		return &rpcResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &rpcError{Code: codeParseError, Message: "invalid JSON: " + err.Error()},
		}
	}
	if req.JSONRPC != "2.0" {
		return s.errResponse(req.ID, codeInvalidRequest, "jsonrpc must be \"2.0\"")
	}

	isNotification := len(req.ID) == 0

	switch req.Method {
	case "initialize":
		return s.okResponse(req.ID, s.initializeResult())
	case "notifications/initialized", "initialized":
		return nil
	case "ping":
		return s.okResponse(req.ID, map[string]any{})
	case "tools/list":
		return s.okResponse(req.ID, s.toolsListResult())
	case "tools/call":
		return s.handleToolCall(ctx, req.ID, req.Params)
	default:
		if isNotification {
			return nil
		}
		return s.errResponse(req.ID, codeMethodNotFound, "method not implemented: "+req.Method)
	}
}

// ----- MCP method handlers --------------------------------------------

func (s *Server) initializeResult() map[string]any {
	return map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{
			"name":    "cheshmhayash",
			"version": "0.5.2",
		},
		"instructions": s.instructions(),
	}
}

func (s *Server) instructions() string {
	base := "NATS administration server. Use tool calls to inspect servers, " +
		"accounts, streams, and consumers across the configured clusters. " +
		"Replies are the raw NATS JSON payloads."
	if !s.write {
		return base + " Read-only mode: mutating tools (reload, lame-duck, " +
			"kick, stream/consumer update/purge/delete) are not registered. " +
			"Start the server with CHESHMHAYASH_MCP_WRITE=1 to expose them."
	}
	return base + " Write mode is enabled — destructive verbs (purge, " +
		"delete, kick) require an explicit confirm=true argument."
}

func (s *Server) toolsListResult() map[string]any {
	out := make([]map[string]any, 0, len(s.tools))
	for _, t := range s.tools {
		out = append(out, map[string]any{
			"name":        t.name,
			"description": t.description,
			"inputSchema": t.inputSchema,
		})
	}
	return map[string]any{"tools": out}
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) handleToolCall(ctx context.Context, id json.RawMessage, raw json.RawMessage) *rpcResponse {
	var p toolCallParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return s.errResponse(id, codeInvalidParams, "decode params: "+err.Error())
	}
	t := s.findTool(p.Name)
	if t == nil {
		return s.errResponse(id, codeMethodNotFound, "unknown tool: "+p.Name)
	}

	args := p.Arguments
	if len(args) == 0 {
		args = []byte("{}")
	}

	result, err := t.handler(ctx, s, args)
	if err != nil {
		// MCP convention: surface tool errors as a successful response with
		// isError=true so the model can react. Reserve JSON-RPC errors for
		// protocol-level failures.
		return s.okResponse(id, map[string]any{
			"isError": true,
			"content": []map[string]any{
				{"type": "text", "text": err.Error()},
			},
		})
	}
	return s.okResponse(id, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": result},
		},
	})
}

func (s *Server) findTool(name string) *tool {
	for i := range s.tools {
		if s.tools[i].name == name {
			return &s.tools[i]
		}
	}
	return nil
}

// ----- response builders (pure) ---------------------------------------

func (s *Server) okResponse(id json.RawMessage, result any) *rpcResponse {
	if len(id) == 0 {
		return nil // notification — no response expected
	}
	return &rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func (s *Server) errResponse(id json.RawMessage, code int, msg string) *rpcResponse {
	if len(id) == 0 {
		s.log.Warn("mcp protocol error on notification", "code", code, "msg", msg)
		return nil
	}
	return &rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

// ----- stdio writer ----------------------------------------------------

func (s *Server) writeMessage(resp rpcResponse) {
	buf, err := json.Marshal(resp)
	if err != nil {
		s.log.Error("mcp marshal response", "err", err)
		return
	}
	s.outMu.Lock()
	defer s.outMu.Unlock()
	if _, err := s.out.Write(append(buf, '\n')); err != nil {
		s.log.Error("mcp write", "err", err)
	}
}

// ----- cluster resolution ---------------------------------------------

// resolveCluster fetches a cluster by name, building a friendly error if
// it is not configured. Most tools route through this.
func (s *Server) resolveCluster(name string) (*natsx.Cluster, error) {
	if name == "" {
		return nil, errors.New("cluster argument is required")
	}
	c := s.mgr.Get(name)
	if c == nil {
		names := s.mgr.Names()
		return nil, fmt.Errorf("cluster %q not configured (have: %v)", name, names)
	}
	return c, nil
}

// RunStdio is a convenience wrapper for main(): bind to os.Stdin/os.Stdout
// and serve until EOF.
func (s *Server) RunStdio(ctx context.Context) error {
	return s.Serve(ctx, os.Stdin, os.Stdout)
}
