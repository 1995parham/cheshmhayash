package bot

import (
	"strings"
	"testing"

	"github.com/1995parham/natsie/internal/infra/config"
	"github.com/1995parham/natsie/internal/scanner"
)

func validBot(kind string) config.Bot {
	return config.Bot{
		Schedules: []config.Schedule{{Name: "s", Cron: "@daily", Context: "teh1", Kind: kind}},
		Store:     "file:///tmp/natsie",
		Notify:    []string{"stdout://"},
	}
}

func TestValidateBotConfigAcceptsScheduleKinds(t *testing.T) {
	kinds := []string{
		"", // empty defaults to consumer-stale
		config.KindConsumerStale,
		config.KindStreamUnlimited,
		config.KindPeerCheck,
		config.KindStreamReport,
	}

	for _, k := range kinds {
		b := validBot(k)
		if err := validateBotConfig(&b); err != nil {
			t.Errorf("kind %q rejected: %v", k, err)
		}
	}
}

func TestValidateBotConfigRejectsUnknownKind(t *testing.T) {
	b := validBot("bogus")
	if err := validateBotConfig(&b); err == nil {
		t.Error("unknown schedule kind accepted")
	}
}

func TestBuildPeerCheckMessage(t *testing.T) {
	s := config.Schedule{Name: "nightly", Context: "teh1"}
	ghosts := []scanner.PeerRow{
		{Peer: "n3", Health: scanner.PeerGhost, Offline: 2, Groups: 2, Sample: []string{"rides"}},
	}

	msg := buildPeerCheckMessage(s, ghosts)

	if !strings.Contains(msg.Title, "ghost peers") {
		t.Errorf("title = %q", msg.Title)
	}

	for _, want := range []string{"1 ghost peer", "n3", "teh1"} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("body missing %q:\n%s", want, msg.Body)
		}
	}

	// Notify-only: no manifest to approve.
	if msg.ManifestID != "" {
		t.Errorf("manifest id = %q, want empty (notify-only)", msg.ManifestID)
	}
}

func TestBuildStreamReportMessage(t *testing.T) {
	s := config.Schedule{Name: "nightly", Context: "teh1"}
	under := []scanner.StreamReportRow{{Stream: "rides", Replicas: 1, Messages: 42}}
	skew := []scanner.ServerLoad{{Server: "n1", Leads: 5, Replicas: 9}}

	msg := buildStreamReportMessage(s, under, skew)

	for _, want := range []string{"under-replicated", "rides", "R=1", "Placement skew", "n1"} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("body missing %q:\n%s", want, msg.Body)
		}
	}

	if msg.ManifestID != "" {
		t.Errorf("manifest id = %q, want empty (notify-only)", msg.ManifestID)
	}
}
