package scanner

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/1995parham/natsie/internal/infra/natsctx"
)

// startJetStream boots an in-process NATS server with JetStream enabled and
// returns a connection wrapped the way the rest of natsie passes it around.
func startJetStream(t *testing.T) *natsctx.Conn {
	t.Helper()

	srv, err := natsserver.NewServer(&natsserver.Options{
		Port:      -1, // random free port
		JetStream: true,
		StoreDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	go srv.Start()

	t.Cleanup(srv.Shutdown)

	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("server not ready")
	}

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	t.Cleanup(nc.Close)

	return &natsctx.Conn{Conn: nc, Name: "test-cluster"}
}

func TestScanUnlimitedStreams(t *testing.T) {
	conn := startJetStream(t)

	mgr, err := jsm.New(conn.Conn)
	if err != nil {
		t.Fatalf("jsm.New: %v", err)
	}

	// Three streams: one truly unbounded, two that should NOT be flagged.
	mustStream(t, mgr, api.StreamConfig{
		Name:      "unbounded",
		Subjects:  []string{"unbounded.>"},
		Retention: api.LimitsPolicy, // no MaxAge/MaxBytes/MaxMsgs → flagged
	})
	mustStream(t, mgr, api.StreamConfig{
		Name:      "bounded",
		Subjects:  []string{"bounded.>"},
		Retention: api.LimitsPolicy,
		MaxAge:    time.Hour, // capped → not flagged
	})
	mustStream(t, mgr, api.StreamConfig{
		Name:      "workqueue",
		Subjects:  []string{"workqueue.>"},
		Retention: api.WorkQueuePolicy, // self-trims → not flagged
	})

	// A couple of messages so State() reports non-zero counts.
	if err := conn.Publish("unbounded.a", []byte("hello")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if err := conn.Publish("unbounded.b", []byte("world")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if err := conn.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	rows, err := ScanUnlimitedStreams(context.Background(), conn)
	if err != nil {
		t.Fatalf("ScanUnlimitedStreams: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(rows), rows)
	}

	row := rows[0]
	if row.Stream != "unbounded" {
		t.Errorf("stream = %q, want %q", row.Stream, "unbounded")
	}

	if row.Cluster != "test-cluster" {
		t.Errorf("cluster = %q, want %q", row.Cluster, "test-cluster")
	}

	if row.Messages != 2 {
		t.Errorf("messages = %d, want 2", row.Messages)
	}

	if row.Bytes == 0 {
		t.Error("bytes = 0, want non-zero")
	}
}

// TestScanUnlimitedStreamsCancelled verifies the scan honours a cancelled
// context instead of walking every stream.
func TestScanUnlimitedStreamsCancelled(t *testing.T) {
	conn := startJetStream(t)

	mgr, err := jsm.New(conn.Conn)
	if err != nil {
		t.Fatalf("jsm.New: %v", err)
	}

	mustStream(t, mgr, api.StreamConfig{
		Name:      "unbounded",
		Subjects:  []string{"unbounded.>"},
		Retention: api.LimitsPolicy,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rows, err := ScanUnlimitedStreams(ctx, conn)
	if err == nil {
		t.Fatalf("expected context error, got nil (rows=%+v)", rows)
	}
}

// TestScanCapturesFilterSubject proves the classify path threads a live
// consumer's filter subject onto the row — the signal rename detection runs on.
func TestScanCapturesFilterSubject(t *testing.T) {
	conn := startJetStream(t)

	mgr, err := jsm.New(conn.Conn)
	if err != nil {
		t.Fatalf("jsm.New: %v", err)
	}

	mustStream(t, mgr, api.StreamConfig{
		Name:      "rides",
		Subjects:  []string{"ride.>"},
		Retention: api.LimitsPolicy,
		MaxAge:    time.Hour,
	})

	js, err := conn.JetStream()
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}

	if _, err := js.AddConsumer("rides", &nats.ConsumerConfig{
		Durable:       "ride-accepted",
		FilterSubject: "ride.accepted",
		AckPolicy:     nats.AckExplicitPolicy,
	}); err != nil {
		t.Fatalf("add consumer: %v", err)
	}

	rows, err := Scan(context.Background(), conn, nil, Options{MinPending: 0, MinIdle: time.Hour})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	var found *Row

	for i := range rows {
		if rows[i].Consumer == "ride-accepted" {
			found = &rows[i]

			break
		}
	}

	if found == nil {
		t.Fatalf("consumer not in rows: %+v", rows)
	}

	if found.FilterSubject != "ride.accepted" {
		t.Errorf("FilterSubject = %q, want %q", found.FilterSubject, "ride.accepted")
	}
}

func mustStream(t *testing.T, mgr *jsm.Manager, cfg api.StreamConfig) {
	t.Helper()

	if _, err := mgr.NewStreamFromDefault(cfg.Name, cfg); err != nil {
		t.Fatalf("create stream %q: %v", cfg.Name, err)
	}
}
