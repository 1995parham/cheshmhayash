package mcp

import (
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	natstest "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"

	"github.com/1995parham/cheshmhayash/internal/codec"
	"github.com/1995parham/cheshmhayash/internal/config"
	"github.com/1995parham/cheshmhayash/internal/natsx"
)

func TestSubjectSample(t *testing.T) {
	opts := natstest.DefaultTestOptions
	opts.Port = -1
	ns := natstest.RunServer(&opts)
	defer ns.Shutdown()

	mgr, err := natsx.NewManager(t.Context(), []config.NATS{{Name: "local", URL: ns.ClientURL()}})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	srv := NewServer(mgr, slog.New(slog.DiscardHandler), false)
	if srv.findTool("subject_sample") != nil {
		t.Fatal("subject_sample is registered before EnableSampling")
	}
	srv.EnableSampling(func(string) codec.Chain { return codec.DefaultFallbacks() })
	if srv.findTool("subject_sample") == nil {
		t.Fatal("subject_sample is not registered")
	}

	pub, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer pub.Close()

	// Publish until the tool's subscription has collected its messages.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				_ = pub.Publish("demo.a", []byte(`{"n":1}`))
				_ = pub.Publish("other", []byte("ignored"))
			}
		}
	}()

	out, err := toolSubjectSample(t.Context(), srv, json.RawMessage(`{"cluster":"local","subject":"demo.>","count":3,"timeout_seconds":10}`))
	if err != nil {
		t.Fatalf("subject_sample: %v", err)
	}
	var res sampleResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("result %q: %v", out, err)
	}
	if res.Received != 3 || res.TimedOut || len(res.Messages) != 3 {
		t.Fatalf("result = %+v", res)
	}
	if !slices.ContainsFunc(res.Messages, func(m sampledMessage) bool {
		return m.Subject == "demo.a" && m.Format == codec.FormatJSON && string(m.Payload) == `{"n":1}`
	}) {
		t.Fatalf("messages = %+v", res.Messages)
	}
}

func TestSubjectSampleTimesOutAndValidates(t *testing.T) {
	opts := natstest.DefaultTestOptions
	opts.Port = -1
	ns := natstest.RunServer(&opts)
	defer ns.Shutdown()

	mgr, err := natsx.NewManager(t.Context(), []config.NATS{{Name: "local", URL: ns.ClientURL()}})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	srv := NewServer(mgr, slog.New(slog.DiscardHandler), false)
	srv.EnableSampling(func(string) codec.Chain { return codec.DefaultFallbacks() })

	out, err := toolSubjectSample(t.Context(), srv, json.RawMessage(`{"cluster":"local","subject":"quiet","timeout_seconds":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"timed_out":true`) || !strings.Contains(out, `"received":0`) {
		t.Fatalf("result = %s", out)
	}

	if _, err := toolSubjectSample(t.Context(), srv, json.RawMessage(`{"cluster":"local","subject":"a..b"}`)); err == nil {
		t.Error("bad subject accepted")
	}
	if _, err := toolSubjectSample(t.Context(), srv, json.RawMessage(`{"cluster":"nope","subject":"a"}`)); err == nil {
		t.Error("unknown cluster accepted")
	}
}

func TestClamp(t *testing.T) {
	if clamp(0, 5, 50) != 5 || clamp(-1, 5, 50) != 5 || clamp(7, 5, 50) != 7 || clamp(99, 5, 50) != 50 {
		t.Fatal("clamp is wrong")
	}
}
