package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/1995parham/cheshmhayash/internal/natsx"
)

// emptyManager builds a Manager with no clusters. Enough to exercise
// protocol-level paths (initialize, tools/list, cluster_list, error
// formatting) without dialing NATS.
func emptyManager(t *testing.T) *natsx.Manager {
	t.Helper()
	mgr, err := natsx.NewManager(context.Background(), nil)
	if err != nil {
		t.Fatalf("empty manager: %v", err)
	}
	return mgr
}

// rpcLine wraps a request into the line-delimited JSON the server expects.
func rpcLine(t *testing.T, id any, method string, params any) []byte {
	t.Helper()
	m := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		m["params"] = params
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return append(b, '\n')
}

// runServer drives the server with a fixed input transcript and returns
// every response line.
func runServer(t *testing.T, write bool, input [][]byte) []map[string]any {
	t.Helper()
	mgr := emptyManager(t)
	srv := NewServer(mgr, slog.New(slog.DiscardHandler), write)

	var in bytes.Buffer
	for _, line := range input {
		in.Write(line)
	}
	var out bytes.Buffer
	if err := srv.Serve(context.Background(), &in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var resps []map[string]any
	for line := range strings.SplitSeq(strings.TrimRight(out.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("response not JSON %q: %v", line, err)
		}
		resps = append(resps, m)
	}
	return resps
}

func TestInitializeHandshake(t *testing.T) {
	resps := runServer(t, false, [][]byte{
		rpcLine(t, 1, "initialize", map[string]any{"protocolVersion": protocolVersion}),
		rpcLine(t, nil, "notifications/initialized", nil),
	})
	if len(resps) != 1 {
		t.Fatalf("want 1 response (notifications get no reply), got %d: %+v", len(resps), resps)
	}
	r := resps[0]
	if r["id"].(float64) != 1 {
		t.Errorf("id: want 1, got %v", r["id"])
	}
	result, ok := r["result"].(map[string]any)
	if !ok {
		t.Fatalf("result missing: %+v", r)
	}
	if result["protocolVersion"] != protocolVersion {
		t.Errorf("protocolVersion: want %q, got %v", protocolVersion, result["protocolVersion"])
	}
	if _, ok := result["capabilities"].(map[string]any)["tools"]; !ok {
		t.Errorf("capabilities.tools missing")
	}
}

func TestToolsListReadOnly(t *testing.T) {
	resps := runServer(t, false, [][]byte{
		rpcLine(t, 1, "tools/list", nil),
	})
	tools := resps[0]["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, t := range tools {
		names[t.(map[string]any)["name"].(string)] = true
	}
	mustHave := []string{"cluster_list", "server_ping", "stream_info", "jsm_overview"}
	for _, n := range mustHave {
		if !names[n] {
			t.Errorf("tools/list missing read tool %q", n)
		}
	}
	mustNotHave := []string{"stream_delete", "stream_purge", "server_kick", "stream_update"}
	for _, n := range mustNotHave {
		if names[n] {
			t.Errorf("tools/list exposes write tool %q in read-only mode", n)
		}
	}
}

func TestToolsListWriteMode(t *testing.T) {
	resps := runServer(t, true, [][]byte{
		rpcLine(t, 1, "tools/list", nil),
	})
	tools := resps[0]["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, t := range tools {
		names[t.(map[string]any)["name"].(string)] = true
	}
	for _, n := range []string{"stream_delete", "stream_purge", "server_kick", "stream_update", "consumer_delete", "server_reload", "server_lame_duck"} {
		if !names[n] {
			t.Errorf("write mode missing tool %q", n)
		}
	}
}

func TestClusterListWorks(t *testing.T) {
	resps := runServer(t, false, [][]byte{
		rpcLine(t, 1, "tools/call", map[string]any{
			"name":      "cluster_list",
			"arguments": map[string]any{},
		}),
	})
	r := resps[0]["result"].(map[string]any)
	if r["isError"] != nil {
		t.Fatalf("cluster_list returned isError: %+v", r)
	}
	content := r["content"].([]any)[0].(map[string]any)
	if content["text"].(string) != "null" && content["text"].(string) != "[]" {
		t.Errorf("expected empty array/null for no clusters, got %q", content["text"])
	}
}

func TestUnknownClusterReturnsToolError(t *testing.T) {
	resps := runServer(t, false, [][]byte{
		rpcLine(t, 1, "tools/call", map[string]any{
			"name":      "server_ping",
			"arguments": map[string]any{"cluster": "nope"},
		}),
	})
	r := resps[0]["result"].(map[string]any)
	if r["isError"] != true {
		t.Fatalf("expected isError=true, got %+v", r)
	}
	text := r["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "not configured") {
		t.Errorf("expected helpful error text, got %q", text)
	}
}

func TestUnknownMethodReturnsJSONRPCError(t *testing.T) {
	resps := runServer(t, false, [][]byte{
		rpcLine(t, 1, "no/such/method", nil),
	})
	errObj, ok := resps[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %+v", resps[0])
	}
	if int(errObj["code"].(float64)) != codeMethodNotFound {
		t.Errorf("expected method-not-found code, got %v", errObj["code"])
	}
}

func TestMalformedJSONReturnsParseError(t *testing.T) {
	resps := runServer(t, false, [][]byte{
		[]byte("not json at all\n"),
	})
	errObj := resps[0]["error"].(map[string]any)
	if int(errObj["code"].(float64)) != codeParseError {
		t.Errorf("expected parse error code, got %v", errObj["code"])
	}
}

func TestDestructiveToolRequiresConfirmInWriteMode(t *testing.T) {
	resps := runServer(t, true, [][]byte{
		rpcLine(t, 1, "tools/call", map[string]any{
			"name": "stream_delete",
			"arguments": map[string]any{
				"cluster": "anything",
				"stream":  "ORDERS",
				"confirm": false,
			},
		}),
	})
	r := resps[0]["result"].(map[string]any)
	if r["isError"] != true {
		t.Fatalf("expected isError=true when confirm=false, got %+v", r)
	}
	text := r["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "confirm") {
		t.Errorf("expected confirm-required error, got %q", text)
	}
}
