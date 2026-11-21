package scanner

import (
	"testing"
	"time"

	"github.com/nats-io/jsm.go/api"
)

func TestClassifyConsumer(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time {
		v := now.Add(-d)

		return &v
	}

	cases := []struct {
		name string
		info api.ConsumerInfo
		want Status
	}{
		{"push bound", api.ConsumerInfo{PushBound: true, NumPending: 9}, StatusActive},
		{"pull waiters", api.ConsumerInfo{NumWaiting: 1, NumPending: 9}, StatusActive},
		{"recent ack", api.ConsumerInfo{AckFloor: api.SequenceInfo{Last: ago(time.Hour)}, NumPending: 9}, StatusActive},
		{"old ack with backlog", api.ConsumerInfo{AckFloor: api.SequenceInfo{Last: ago(48 * time.Hour)}, NumPending: 9}, StatusStale},
		{"never acked, no backlog", api.ConsumerInfo{Created: now.Add(-72 * time.Hour)}, StatusOK},
		{"never acked, young", api.ConsumerInfo{Created: now.Add(-time.Minute), NumPending: 9}, StatusOK},
		{"never acked, old", api.ConsumerInfo{Created: now.Add(-72 * time.Hour), NumPending: 9}, StatusStale},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := ClassifyConsumer(&tc.info, now, 24*time.Hour).Status; got != tc.want {
				t.Errorf("status = %s, want %s", got, tc.want)
			}
		})
	}
}
