package consumer

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
		{
			name: "push bound",
			info: consumerInfo{PushBound: true},
			want: true,
		},
		{
			name: "pull waiter present",
			info: consumerInfo{NumWaiting: 1},
			want: true,
		},
		{
			name: "ack since manifest",
			info: func() consumerInfo {
				var c consumerInfo
				c.AckFloor.LastActive = &afterManifest
				return c
			}(),
			want: true,
		},
		{
			name: "ack before manifest",
			info: func() consumerInfo {
				var c consumerInfo
				c.AckFloor.LastActive = &beforeManifest
				return c
			}(),
			want: false,
		},
		{
			name: "never acked",
			info: consumerInfo{},
			want: false,
		},
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
