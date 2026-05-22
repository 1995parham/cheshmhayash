package cleanup

import (
	"testing"
	"time"
)

func TestIsActive(t *testing.T) {
	manifestTime := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	afterManifest := manifestTime.Add(5 * time.Minute)
	beforeManifest := manifestTime.Add(-1 * time.Hour)

	cases := []struct {
		name string
		info consumerInfo
		want bool
	}{
		{"push bound", consumerInfo{PushBound: true}, true},
		{"pull waiter present", consumerInfo{NumWaiting: 1}, true},
		{"ack since manifest", func() consumerInfo {
			var c consumerInfo
			c.AckFloor.LastActive = &afterManifest
			return c
		}(), true},
		{"ack before manifest", func() consumerInfo {
			var c consumerInfo
			c.AckFloor.LastActive = &beforeManifest
			return c
		}(), false},
		{"never acked", consumerInfo{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isActive(&c.info, manifestTime) != ""
			if got != c.want {
				t.Fatalf("isActive=%v want %v", got, c.want)
			}
		})
	}
}

func TestResultSummary(t *testing.T) {
	r := &Result{Deleted: 3, Preserved: 2, Gone: 1, Skipped: 0, Failed: 1}
	got := r.Summary()
	want := "3 deleted, 2 preserved (active), 1 already gone, 0 skipped, 1 failed"
	if got != want {
		t.Errorf("Summary=%q want %q", got, want)
	}
}
