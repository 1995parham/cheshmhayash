package scanner

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"
	"github.com/nats-io/nats.go"

	"github.com/1995parham/natsie/internal/infra/natsctx"
)

func TestSortOwnerMatchesAndOwner(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	matches := []OwnerMatch{
		{Cluster: "a", Consumer: "stale-old", Status: StatusStale, LastAck: t0},
		{Cluster: "b", Consumer: "active-owner", Status: StatusActive, LastAck: t0.Add(-time.Hour)},
		{Cluster: "a", Consumer: "stale-new", Status: StatusStale, LastAck: t0.Add(time.Hour)},
	}

	sortOwnerMatches(matches)

	// Active leads despite an older ack; stales then sort most-recent first.
	want := []string{"active-owner", "stale-new", "stale-old"}
	for i, w := range want {
		if matches[i].Consumer != w {
			t.Errorf("matches[%d] = %q, want %q (order: %+v)", i, matches[i].Consumer, w, matches)
		}
	}

	owner, ok := Owner(matches)
	if !ok || owner.Consumer != "active-owner" || owner.Cluster != "b" {
		t.Errorf("Owner = %+v ok=%v, want active-owner on b", owner, ok)
	}
}

func TestOwnerNoneActive(t *testing.T) {
	matches := []OwnerMatch{
		{Cluster: "a", Consumer: "stale", Status: StatusStale},
	}

	if _, ok := Owner(matches); ok {
		t.Error("Owner reported an owner with no active match")
	}
}

func TestResolveOwnerAcrossContexts(t *testing.T) {
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

	for _, c := range []struct{ durable, subject string }{
		{"c-accepted", "ride.accepted"},
		{"c-cancelled", "ride.cancelled"},
	} {
		if _, err := js.AddConsumer("rides", &nats.ConsumerConfig{
			Durable:       c.durable,
			FilterSubject: c.subject,
			AckPolicy:     nats.AckExplicitPolicy,
		}); err != nil {
			t.Fatalf("add consumer %s: %v", c.durable, err)
		}
	}

	// The same server presented under two cluster names stands in for two
	// configured contexts — enough to exercise the multi-conn walk and the
	// per-cluster labelling.
	conns := []*natsctx.Conn{
		{Conn: conn.Conn, Name: "teh1"},
		{Conn: conn.Conn, Name: "teh2"},
	}

	matches, err := ResolveOwner(context.Background(), conns, "ride.accepted", Options{MinIdle: time.Hour})
	if err != nil {
		t.Fatalf("ResolveOwner: %v", err)
	}

	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2 (one per context): %+v", len(matches), matches)
	}

	clusters := map[string]bool{}

	for _, m := range matches {
		if m.Consumer != "c-accepted" {
			t.Errorf("matched wrong consumer %q (subject filter leaked)", m.Consumer)
		}

		clusters[m.Cluster] = true
	}

	if !clusters["teh1"] || !clusters["teh2"] {
		t.Errorf("clusters = %v, want both teh1 and teh2", clusters)
	}
}
