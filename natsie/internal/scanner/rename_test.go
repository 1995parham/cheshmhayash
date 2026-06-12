package scanner

import (
	"testing"
	"time"

	"github.com/nats-io/jsm.go/api"
)

func consumerConfig(subject string, subjects []string) api.ConsumerConfig {
	return api.ConsumerConfig{FilterSubject: subject, FilterSubjects: subjects}
}

func TestDetectRenames(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	t.Run("stale matched to active successor on same subject", func(t *testing.T) {
		rows := []Row{
			{Stream: "rides", Consumer: "old-group", Status: StatusStale, FilterSubject: "ride.accepted"},
			{Stream: "rides", Consumer: "new-group", Status: StatusActive, FilterSubject: "ride.accepted", LastAck: t0},
		}

		detectRenames(rows)

		if rows[0].RenamedTo != "new-group" {
			t.Errorf("stale.RenamedTo = %q, want %q", rows[0].RenamedTo, "new-group")
		}

		if rows[1].RenamedTo != "" {
			t.Errorf("active.RenamedTo = %q, want empty", rows[1].RenamedTo)
		}
	})

	t.Run("most recently active successor wins", func(t *testing.T) {
		rows := []Row{
			{Stream: "rides", Consumer: "old", Status: StatusStale, FilterSubject: "ride.accepted"},
			{Stream: "rides", Consumer: "newer", Status: StatusActive, FilterSubject: "ride.accepted", LastAck: t0.Add(time.Hour)},
			{Stream: "rides", Consumer: "older", Status: StatusActive, FilterSubject: "ride.accepted", LastAck: t0},
		}

		detectRenames(rows)

		if rows[0].RenamedTo != "newer" {
			t.Errorf("RenamedTo = %q, want %q", rows[0].RenamedTo, "newer")
		}
	})

	t.Run("different subject is not a rename", func(t *testing.T) {
		rows := []Row{
			{Stream: "rides", Consumer: "old", Status: StatusStale, FilterSubject: "ride.accepted"},
			{Stream: "rides", Consumer: "new", Status: StatusActive, FilterSubject: "ride.cancelled", LastAck: t0},
		}

		detectRenames(rows)

		if rows[0].RenamedTo != "" {
			t.Errorf("RenamedTo = %q, want empty (different subject)", rows[0].RenamedTo)
		}
	})

	t.Run("different stream is not a rename", func(t *testing.T) {
		rows := []Row{
			{Stream: "rides", Consumer: "old", Status: StatusStale, FilterSubject: "ride.accepted"},
			{Stream: "trips", Consumer: "new", Status: StatusActive, FilterSubject: "ride.accepted", LastAck: t0},
		}

		detectRenames(rows)

		if rows[0].RenamedTo != "" {
			t.Errorf("RenamedTo = %q, want empty (different stream)", rows[0].RenamedTo)
		}
	})

	t.Run("empty filter subject is never matched", func(t *testing.T) {
		rows := []Row{
			{Stream: "rides", Consumer: "old", Status: StatusStale, FilterSubject: ""},
			{Stream: "rides", Consumer: "new", Status: StatusActive, FilterSubject: "", LastAck: t0},
		}

		detectRenames(rows)

		if rows[0].RenamedTo != "" {
			t.Errorf("RenamedTo = %q, want empty (no filter subject)", rows[0].RenamedTo)
		}
	})

	t.Run("stale with no active successor is left untouched", func(t *testing.T) {
		rows := []Row{
			{Stream: "rides", Consumer: "abandoned", Status: StatusStale, FilterSubject: "ride.accepted"},
		}

		detectRenames(rows)

		if rows[0].RenamedTo != "" {
			t.Errorf("RenamedTo = %q, want empty (no successor)", rows[0].RenamedTo)
		}
	})
}

func TestFilterSubjectNormalization(t *testing.T) {
	t.Run("single subject", func(t *testing.T) {
		got := filterSubject(consumerConfig("ride.accepted", nil))
		if got != "ride.accepted" {
			t.Errorf("got %q, want %q", got, "ride.accepted")
		}
	})

	t.Run("multi subject is sorted for order independence", func(t *testing.T) {
		a := filterSubject(consumerConfig("", []string{"b.evt", "a.evt"}))
		b := filterSubject(consumerConfig("", []string{"a.evt", "b.evt"}))

		if a != b {
			t.Errorf("order-dependent: %q != %q", a, b)
		}

		if a != "a.evt,b.evt" {
			t.Errorf("got %q, want %q", a, "a.evt,b.evt")
		}
	})

	t.Run("no filter is empty", func(t *testing.T) {
		if got := filterSubject(consumerConfig("", nil)); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}
