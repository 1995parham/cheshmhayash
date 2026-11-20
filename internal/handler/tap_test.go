package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	natstest "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"

	"github.com/1995parham/cheshmhayash/internal/codec"
	"github.com/1995parham/cheshmhayash/internal/config"
	"github.com/1995parham/cheshmhayash/internal/natsx"
)

type tapFixture struct {
	srv *httptest.Server
	pub *nats.Conn
}

func newTapFixture(t *testing.T, cfg Tap) *tapFixture {
	t.Helper()
	opts := natstest.DefaultTestOptions
	opts.Port = -1
	ns := natstest.RunServer(&opts)
	t.Cleanup(ns.Shutdown)

	mgr, err := natsx.NewManager(t.Context(), []config.NATS{{Name: "local", URL: ns.ClientURL()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	cfg.Chains = func(string) codec.Chain { return codec.DefaultFallbacks() }
	h := Mux(mgr, nil, Options{API: true, Tap: &cfg}, slog.New(slog.DiscardHandler), nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	pub, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pub.Close)
	return &tapFixture{srv: srv, pub: pub}
}

type sseEvent struct {
	name string
	data string
}

// open starts a tap and returns its HTTP status and its events. The events
// channel closes when the stream ends; open owns the response body.
func (f *tapFixture) open(ctx context.Context, t *testing.T, subject string) (<-chan sseEvent, int) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		f.srv.URL+"/api/tap/clusters/local/stream?subject="+url.QueryEscape(subject), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req) //nolint:bodyclose // closed below, or by the reader goroutine for a 200
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan sseEvent, 1024)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		close(events)
		return events, resp.StatusCode
	}
	go func() {
		defer close(events)
		defer func() { _ = resp.Body.Close() }()
		sc := bufio.NewScanner(resp.Body)
		var ev sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				ev.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				ev.data = strings.TrimPrefix(line, "data: ")
			case line == "" && ev.name != "":
				events <- ev
				ev = sseEvent{}
			}
		}
	}()
	return events, resp.StatusCode
}

// publishUntil keeps publishing until stop closes, so the tap's
// subscription receives messages whenever it is ready.
func publishUntil(nc *nats.Conn, stop <-chan struct{}, subject string, data []byte) {
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			_ = nc.Publish(subject, data)
		}
	}
}

func TestTapStreamsDecodedMessages(t *testing.T) {
	f := newTapFixture(t, Tap{Allow: []string{"demo.>"}, MaxRate: 1000, MaxDuration: time.Minute, MaxTaps: 2})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	events, status := f.open(ctx, t, "demo.*")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	stop := make(chan struct{})
	defer close(stop)
	go publishUntil(f.pub, stop, "demo.a", []byte(`{"n":1}`))

	for ev := range events {
		if ev.name != "message" {
			continue
		}
		var m tapEvent
		if err := json.Unmarshal([]byte(ev.data), &m); err != nil {
			t.Fatalf("event %q: %v", ev.data, err)
		}
		if m.Subject != "demo.a" || m.Format != codec.FormatJSON || string(m.Payload) != `{"n":1}` || m.Seq < 1 {
			t.Fatalf("event = %+v", m)
		}
		return
	}
	t.Fatal("no message event")
}

func TestTapRateLimitAndEnd(t *testing.T) {
	f := newTapFixture(t, Tap{MaxRate: 1, MaxDuration: 2500 * time.Millisecond, MaxTaps: 2})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	events, status := f.open(ctx, t, "flood")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	stop := make(chan struct{})
	defer close(stop)
	go publishUntil(f.pub, stop, "flood", []byte("x"))

	var sent int
	var skipped int64
	var ended bool
	for ev := range events {
		switch ev.name {
		case "message":
			sent++
		case "stats":
			var st tapStats
			if err := json.Unmarshal([]byte(ev.data), &st); err != nil {
				t.Fatal(err)
			}
			skipped = st.Skipped
		case "end":
			ended = strings.Contains(ev.data, "max_duration")
		}
	}
	if !ended {
		t.Error("no end event with max_duration")
	}
	if sent > 4 {
		t.Errorf("sent %d messages in 2.5 s at max_rate 1", sent)
	}
	if skipped == 0 {
		t.Error("no message was counted as skipped")
	}
}

func TestTapRejects(t *testing.T) {
	f := newTapFixture(t, Tap{Allow: []string{"ride.>"}, MaxRate: 10, MaxDuration: time.Minute, MaxTaps: 1})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	for subject, want := range map[string]int{
		"driver.status": http.StatusForbidden,  // not in the allow list
		">":             http.StatusForbidden,  // wider than the allow list
		"ride..x":       http.StatusBadRequest, // not a valid subject
	} {
		if _, status := f.open(ctx, t, subject); status != want {
			t.Errorf("subject %q: status = %d, want %d", subject, status, want)
		}
	}

	// MaxTaps is 1: a second open tap is refused.
	if _, status := f.open(ctx, t, "ride.accepted"); status != http.StatusOK {
		t.Fatalf("first tap: status = %d", status)
	}
	if _, status := f.open(ctx, t, "ride.finished"); status != http.StatusTooManyRequests {
		t.Errorf("second tap: status = %d, want 429", status)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.srv.URL+"/api/tap/config", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body tapConfigBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.CanTap || body.MaxRate != 10 || len(body.Allow) != 1 {
		t.Errorf("config = %+v", body)
	}
}
