package scanner

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"
	"github.com/nats-io/nats.go"
)

func TestPlacementSkew(t *testing.T) {
	rows := []StreamReportRow{
		{Stream: "a", Leader: "n1", Peers: []string{"n1", "n2", "n3"}},
		{Stream: "b", Leader: "n1", Peers: []string{"n1", "n2", "n3"}},
		{Stream: "c", Leader: "n2", Peers: []string{"n2", "n1"}},
	}

	skew := PlacementSkew(rows)

	if len(skew) != 3 {
		t.Fatalf("got %d servers, want 3: %+v", len(skew), skew)
	}

	// Ordered by leads desc, then replicas desc, then name.
	if skew[0] != (ServerLoad{Server: "n1", Leads: 2, Replicas: 3}) {
		t.Errorf("skew[0] = %+v, want n1 leads=2 replicas=3", skew[0])
	}

	if skew[1] != (ServerLoad{Server: "n2", Leads: 1, Replicas: 3}) {
		t.Errorf("skew[1] = %+v, want n2 leads=1 replicas=3", skew[1])
	}

	if skew[2] != (ServerLoad{Server: "n3", Leads: 0, Replicas: 2}) {
		t.Errorf("skew[2] = %+v, want n3 leads=0 replicas=2", skew[2])
	}
}

func TestPlacementSkewEmpty(t *testing.T) {
	if got := PlacementSkew(nil); len(got) != 0 {
		t.Errorf("got %d, want 0", len(got))
	}
}

func TestReportStreams(t *testing.T) {
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
		MaxMsgs:   1000,
	})
	mustStream(t, mgr, api.StreamConfig{
		Name:      "work",
		Subjects:  []string{"work.>"},
		Retention: api.WorkQueuePolicy,
	})

	for _, subj := range []string{"ride.a", "ride.b"} {
		if err := conn.Publish(subj, []byte("x")); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	if err := conn.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	js, err := conn.JetStream()
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}

	if _, err := js.AddConsumer("rides", &nats.ConsumerConfig{
		Durable:   "c1",
		AckPolicy: nats.AckExplicitPolicy,
	}); err != nil {
		t.Fatalf("add consumer: %v", err)
	}

	rows, err := ReportStreams(context.Background(), conn, "")
	if err != nil {
		t.Fatalf("ReportStreams: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(rows), rows)
	}

	rides := streamRow(t, rows, "rides")
	if rides.Retention != api.LimitsPolicy.String() {
		t.Errorf("retention = %q, want %q", rides.Retention, api.LimitsPolicy.String())
	}

	if rides.Replicas != 1 {
		t.Errorf("replicas = %d, want 1", rides.Replicas)
	}

	if rides.MaxAge != time.Hour || rides.MaxMsgs != 1000 {
		t.Errorf("limits = age %s msgs %d, want 1h/1000", rides.MaxAge, rides.MaxMsgs)
	}

	if rides.Messages != 2 {
		t.Errorf("messages = %d, want 2", rides.Messages)
	}

	if rides.Consumers != 1 {
		t.Errorf("consumers = %d, want 1", rides.Consumers)
	}

	if rides.Leader == "" || len(rides.Peers) != 1 || rides.Peers[0] != rides.Leader {
		t.Errorf("placement = leader %q peers %v, want one self-hosted peer", rides.Leader, rides.Peers)
	}

	// A single node leads and hosts both streams — one skew row, two of each.
	skew := PlacementSkew(rows)
	if len(skew) != 1 {
		t.Fatalf("got %d skew rows, want 1: %+v", len(skew), skew)
	}

	if skew[0].Leads != 2 || skew[0].Replicas != 2 {
		t.Errorf("skew[0] = %+v, want leads=2 replicas=2", skew[0])
	}
}

// TestReportStreamsFilter checks the single-stream filter.
func TestReportStreamsFilter(t *testing.T) {
	conn := startJetStream(t)

	mgr, err := jsm.New(conn.Conn)
	if err != nil {
		t.Fatalf("jsm.New: %v", err)
	}

	mustStream(t, mgr, api.StreamConfig{Name: "rides", Subjects: []string{"ride.>"}, Retention: api.LimitsPolicy})
	mustStream(t, mgr, api.StreamConfig{Name: "work", Subjects: []string{"work.>"}, Retention: api.WorkQueuePolicy})

	rows, err := ReportStreams(context.Background(), conn, "work")
	if err != nil {
		t.Fatalf("ReportStreams: %v", err)
	}

	if len(rows) != 1 || rows[0].Stream != "work" {
		t.Fatalf("filter returned %+v, want only work", rows)
	}
}

func streamRow(t *testing.T, rows []StreamReportRow, name string) StreamReportRow {
	t.Helper()

	for _, r := range rows {
		if r.Stream == name {
			return r
		}
	}

	t.Fatalf("stream %q not in rows: %+v", name, rows)

	return StreamReportRow{}
}
