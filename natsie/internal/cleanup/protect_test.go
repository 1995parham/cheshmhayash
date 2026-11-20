package cleanup

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/1995parham/natsie/internal/protect"
)

// The protection veto reads the metadata off the live CONSUMER.INFO response,
// so it has to survive the JSON round trip that response goes through. This
// pins the wire shape: a NACK-declared consumer carries its marker under
// config.metadata, and Apply must be able to see it there.
func TestConsumerInfoDecodesConfigMetadata(t *testing.T) {
	raw := []byte(`{
	  "push_bound": false,
	  "num_waiting": 0,
	  "config": {
	    "durable_name": "svc-trip",
	    "metadata": {"natsie.io/managed": "true", "other": "x"}
	  },
	  "ack_floor": {}
	}`)

	var info consumerInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got := info.Config.Metadata["natsie.io/managed"]; got != "true" {
		t.Fatalf("config.metadata not decoded, got %#v", info.Config.Metadata)
	}

	guard, err := protect.New(protect.Config{})
	if err != nil {
		t.Fatalf("protect.New: %v", err)
	}

	if reason := guard.Reason("rides", "svc-trip", info.Config.Metadata); reason == "" {
		t.Fatal("expected the decoded metadata to trip the protector")
	}
}

// A consumer with no metadata must stay deletable — protection that fires on
// everything would be just as broken as protection that never fires.
func TestUnannotatedConsumerIsNotProtected(t *testing.T) {
	var info consumerInfo
	if err := json.Unmarshal([]byte(`{"config":{},"ack_floor":{}}`), &info); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	guard, _ := protect.New(protect.Config{})
	if reason := guard.Reason("rides", "worker", info.Config.Metadata); reason != "" {
		t.Fatalf("unannotated consumer should be deletable, got %q", reason)
	}
}

func TestSummaryReportsProtected(t *testing.T) {
	r := &Result{Deleted: 1, Protected: 3}
	if got := r.Summary(); !strings.Contains(got, "3 protected") {
		t.Fatalf("summary should surface the protected count, got %q", got)
	}
}
