package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natstest "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
)

// syncBuffer is a bytes.Buffer that the command goroutine and the test
// can share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func startServer(t *testing.T) *server.Server {
	t.Helper()
	opts := natstest.DefaultTestOptions
	opts.Port = -1
	srv := natstest.RunServer(&opts)
	t.Cleanup(srv.Shutdown)
	return srv
}

func writeSettings(t *testing.T, url string) string {
	t.Helper()
	protos, err := filepath.Abs("../schema/testdata/backend")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "settings.toml")
	body := fmt.Sprintf(`
[[nats]]
name = "local"
url = %q

[[schemas.sources]]
name = "backend"
type = "proto_dir"
path = %q

[[schemas.subjects]]
match = "demo.ping.>"
message = "backend:demo.v1.Ping"
`, url, protos)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// run executes the command tree with args and returns its stdout.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out syncBuffer
	root := Root()
	root.Writer = &out
	root.ErrWriter = io.Discard
	root.Reader = strings.NewReader("")
	err := root.Run(t.Context(), append([]string{"cheshmhayash", "--log-level", "error"}, args...))
	return out.String(), err
}

type line struct {
	Subject string          `json:"subject"`
	Format  string          `json:"format"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func TestSubAndPub(t *testing.T) {
	srv := startServer(t)
	cfg := writeSettings(t, srv.ClientURL())

	var (
		out    syncBuffer
		errOut syncBuffer
		subErr error
		done   = make(chan struct{})
	)
	go func() {
		defer close(done)
		root := Root()
		root.Writer = &out
		root.ErrWriter = &errOut
		subErr = root.Run(t.Context(), []string{
			"cheshmhayash", "--log-level", "error", "--config", cfg,
			"sub", "--count", "3", "--timeout", "20s", "-o", "json", "demo.>",
		})
	}()
	// sub prints "listening on" after the server confirmed the interest.
	waitFor(t, func() bool { return strings.Contains(errOut.String(), "listening on") })

	for _, args := range [][]string{
		{"pub", "demo.ping.eu1", `{"id":"r-1","count":"5","region":"REGION_EU"}`},
		{"pub", "demo.text", "hello"},
		{"pub", "--type", "backend:demo.v1.Ping", "demo.forced", `{"id":"r-2"}`},
	} {
		if _, err := run(t, append([]string{"--config", cfg}, args...)...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	<-done
	if subErr != nil {
		t.Fatalf("sub: %v", subErr)
	}

	var got []line
	for raw := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		var l line
		if err := json.Unmarshal([]byte(raw), &l); err != nil {
			t.Fatalf("output line %q: %v", raw, err)
		}
		got = append(got, l)
	}
	if len(got) != 3 {
		t.Fatalf("got %d lines, want 3:\n%s", len(got), out.String())
	}
	if got[0].Format != "protobuf" || got[0].Type != "backend:demo.v1.Ping" || !strings.Contains(string(got[0].Payload), `"region":"REGION_EU"`) {
		t.Errorf("line 1 = %+v %s", got[0], got[0].Payload)
	}
	if got[1].Format != "text" || string(got[1].Payload) != `"hello"` {
		t.Errorf("line 2 = %+v %s", got[1], got[1].Payload)
	}
	// The forced type was only used to encode; without a rule for
	// demo.forced the subscriber sees a schemaless protobuf dump.
	if got[2].Format != "protowire" || !strings.Contains(string(got[2].Payload), `"1":"r-2"`) {
		t.Errorf("line 3 = %+v %s", got[2], got[2].Payload)
	}
}

func TestPubRequest(t *testing.T) {
	srv := startServer(t)
	cfg := writeSettings(t, srv.ClientURL())

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	// The responder echoes the request payload back.
	if _, err := nc.Subscribe("demo.ping.echo", func(m *nats.Msg) { _ = m.Respond(m.Data) }); err != nil {
		t.Fatal(err)
	}
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "--config", cfg, "pub", "--request", "--reply-type", "backend:demo.v1.Ping", "-o", "json",
		"demo.ping.echo", `{"id":"echo"}`)
	if err != nil {
		t.Fatalf("pub --request: %v", err)
	}
	var l line
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &l); err != nil {
		t.Fatalf("reply %q: %v", out, err)
	}
	if l.Type != "backend:demo.v1.Ping" || string(l.Payload) != `{"id":"echo"}` {
		t.Fatalf("reply = %+v %s", l, l.Payload)
	}
}

func TestPubErrors(t *testing.T) {
	srv := startServer(t)
	cfg := writeSettings(t, srv.ClientURL())
	for name, args := range map[string][]string{
		"wildcard subject":  {"pub", "demo.>", "x"},
		"json does not fit": {"pub", "demo.ping.x", `{"nope":1}`},
		"bad header":        {"pub", "-H", "novalue", "demo.x", "x"},
		"no subject":        {"pub"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := run(t, append([]string{"--config", cfg}, args...)...); err == nil {
				t.Fatal("no error")
			}
		})
	}
}

func TestSubErrors(t *testing.T) {
	srv := startServer(t)
	cfg := writeSettings(t, srv.ClientURL())
	for name, args := range map[string][]string{
		"no subject":     {"sub"},
		"bad subject":    {"sub", "a..b"},
		"unknown type":   {"sub", "--type", "demo.v1.Nope", "x"},
		"bad output":     {"sub", "-o", "xml", "x"},
		"unknown server": {"sub", "-s", "nats://127.0.0.1:1", "--timeout", "1s", "x"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := run(t, append([]string{"--config", cfg}, args...)...); err == nil {
				t.Fatal("no error")
			}
		})
	}
}

func TestSchemaCommands(t *testing.T) {
	cfg := writeSettings(t, "nats://127.0.0.1:4222")

	out, err := run(t, "--config", cfg, "schema", "list")
	if err != nil || !strings.Contains(out, "backend") || !strings.Contains(out, "1 subject rules") {
		t.Errorf("schema list = %q, %v", out, err)
	}

	out, err = run(t, "--config", cfg, "schema", "messages", "ping")
	if err != nil || !strings.Contains(out, "backend:demo.v1.Ping\n") || strings.Contains(out, "Timestamp") {
		t.Errorf("schema messages ping = %q, %v", out, err)
	}

	out, err = run(t, "--config", cfg, "schema", "show", "demo.v1.Ping")
	if err != nil || !strings.Contains(out, "map<string, int32>") || !strings.Contains(out, "REGION_EU=1") {
		t.Errorf("schema show = %q, %v", out, err)
	}

	out, err = run(t, "--config", cfg, "schema", "check")
	if err != nil || !strings.Contains(out, "ok: 1 sources, 1 subject rules") {
		t.Errorf("schema check = %q, %v", out, err)
	}

	bad := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(bad, []byte("[[subjects]]\nmatch = \"x.>\"\nmessage = \"demo.v1.Nope\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "--config", cfg, "schema", "check", "--schemas", bad); err == nil || !strings.Contains(err.Error(), "rule x.>") {
		t.Errorf("schema check with a missing message: err = %v", err)
	}

	out, err = run(t, "--config", cfg, "schema", "resolve", "demo.ping.a", "other")
	if err != nil || !strings.Contains(out, "backend:demo.v1.Ping") || !strings.Contains(out, "no rule") {
		t.Errorf("schema resolve = %q, %v", out, err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
