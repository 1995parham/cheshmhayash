package health_test

import (
	"strings"
	"testing"
	"time"

	"github.com/1995parham/cheshmhayash/internal/health"
)

// overview is three servers' JSZ replies. ORDERS (R3) is led by n1 and n2
// sends an older copy of it; n3 is offline everywhere. LOGS (R1) is near its
// max_bytes with discard new. NOLEAD has lost its leader.
const overview = `[
 {"server":{"name":"n1"},"data":{"account_details":[{"name":"APP","stream_detail":[
  {"name":"ORDERS",
   "cluster":{"leader":"n1","replicas":[{"name":"n2","current":true},{"name":"n3","offline":true}]},
   "config":{"name":"ORDERS","retention":"limits","num_replicas":3,"discard":"old"},
   "state":{"messages":500,"bytes":2048},
   "consumer_detail":[
    {"stream_name":"ORDERS","name":"old","config":{"filter_subject":"orders.created","ack_policy":"explicit"},
     "ack_floor":{"consumer_seq":1,"stream_seq":1,"last_active":"2026-11-19T00:00:00Z"},
     "num_pending":100,"cluster":{"leader":"n1"}},
    {"stream_name":"ORDERS","name":"new","config":{"filter_subject":"orders.created","ack_policy":"explicit"},
     "push_bound":true,"cluster":{"leader":"n1"}},
    {"stream_name":"ORDERS","name":"full","config":{"ack_policy":"explicit","max_ack_pending":10},
     "num_ack_pending":10,"num_pending":7,"push_bound":true,"cluster":{"leader":"n1"}},
    {"stream_name":"ORDERS","name":"nack","config":{"ack_policy":"explicit","metadata":{"cheshmhayash.io/managed":"nack"}},
     "ack_floor":{"consumer_seq":1,"stream_seq":1,"last_active":"2026-11-01T00:00:00Z"},
     "num_pending":3,"cluster":{"leader":"n1"}}
   ]},
  {"name":"LOGS","cluster":{"leader":"n1"},
   "config":{"name":"LOGS","retention":"limits","num_replicas":1,"max_bytes":1000,"discard":"new"},
   "state":{"messages":9,"bytes":950}}
 ]}]}},
 {"server":{"name":"n2"},"data":{"account_details":[{"name":"APP","stream_detail":[
  {"name":"ORDERS",
   "cluster":{"leader":"n1","replicas":[{"name":"n2","current":true},{"name":"n3","offline":true}]},
   "config":{"name":"ORDERS","retention":"limits","num_replicas":3,"discard":"old"},
   "state":{"messages":1,"bytes":1}},
  {"name":"NOLEAD","cluster":{"replicas":[{"name":"n2","current":false},{"name":"n3","offline":true}]},
   "config":{"name":"NOLEAD","retention":"workqueue","num_replicas":3},
   "state":{"messages":0,"bytes":0}}
 ]}]}}
]`

func TestAnalyze(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)

	rep, err := health.Analyze([]byte(overview), now, health.DefaultOptions())
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	type key struct {
		kind             health.Kind
		stream, consumer string
		peer             string
		severity         health.Severity
	}

	got := map[key]health.Finding{}
	for _, f := range rep.Findings {
		got[key{f.Kind, f.Stream, f.Consumer, f.Peer, f.Severity}] = f
	}

	want := []key{
		{health.KindAckPendingFull, "ORDERS", "full", "", health.Critical},
		{health.KindNearLimit, "LOGS", "", "", health.Critical},
		{health.KindNoLeader, "NOLEAD", "", "", health.Critical},
		{health.KindGhostPeer, "", "", "n3", health.Critical},
		{health.KindReplicaOffline, "ORDERS", "", "", health.Warning},
		{health.KindStaleConsumer, "ORDERS", "old", "", health.Warning},
		{health.KindStaleConsumer, "ORDERS", "nack", "", health.Warning},
		{health.KindUnlimitedStream, "ORDERS", "", "", health.Warning},
		{health.KindUnderReplicated, "LOGS", "", "", health.Info},
	}

	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("missing finding %+v; got %+v", k, rep.Findings)
		}
	}

	if len(rep.Findings) != len(want) {
		t.Errorf("got %d findings, want %d: %+v", len(rep.Findings), len(want), rep.Findings)
	}

	if f := got[key{health.KindStaleConsumer, "ORDERS", "old", "", health.Warning}]; f.RenamedTo != "new" || f.Pending != 100 || f.Account != "APP" {
		t.Errorf("stale old: renamed_to=%q pending=%d account=%q", f.RenamedTo, f.Pending, f.Account)
	}

	if f := got[key{health.KindStaleConsumer, "ORDERS", "nack", "", health.Warning}]; f.Managed == "" {
		t.Errorf("stale nack: want a managed reason")
	}

	if rep.Findings[0].Severity != health.Critical {
		t.Errorf("first finding is %s, want critical first", rep.Findings[0].Severity)
	}

	if rep.Summary.Streams != 3 || rep.Summary.Consumers != 4 || rep.Summary.Critical != 4 {
		t.Errorf("summary = %+v", rep.Summary)
	}
}

func TestAnalyzeLeaderCopyWins(t *testing.T) {
	t.Parallel()

	rep, err := health.Analyze([]byte(overview), time.Now(), health.DefaultOptions())
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	for _, f := range rep.Findings {
		if f.Kind == health.KindUnlimitedStream && f.Stream == "ORDERS" {
			if want := "500 messages"; !strings.Contains(f.Message, want) {
				t.Errorf("message %q does not use the leader's state (%s)", f.Message, want)
			}

			return
		}
	}

	t.Fatal("no unlimited_stream finding for ORDERS")
}

func TestAnalyzeEmpty(t *testing.T) {
	t.Parallel()

	rep, err := health.Analyze([]byte(`[]`), time.Now(), health.DefaultOptions())
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if rep.Findings == nil || len(rep.Findings) != 0 {
		t.Errorf("findings = %#v, want an empty, non-nil slice", rep.Findings)
	}
}

func TestAnalyzeBadInput(t *testing.T) {
	t.Parallel()

	if _, err := health.Analyze([]byte(`{`), time.Now(), health.DefaultOptions()); err == nil {
		t.Fatal("want an error for bad JSON")
	}
}
