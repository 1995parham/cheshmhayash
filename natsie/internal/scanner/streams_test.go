package scanner

import (
	"testing"
	"time"

	"github.com/nats-io/jsm.go/api"
)

func TestIsUnlimited(t *testing.T) {
	cases := []struct {
		name string
		cfg  api.StreamConfig
		want bool
	}{
		{
			name: "limits with no caps is unlimited",
			cfg:  api.StreamConfig{Retention: api.LimitsPolicy},
			want: true,
		},
		{
			name: "limits with max age is bounded",
			cfg:  api.StreamConfig{Retention: api.LimitsPolicy, MaxAge: time.Hour},
			want: false,
		},
		{
			name: "limits with max bytes is bounded",
			cfg:  api.StreamConfig{Retention: api.LimitsPolicy, MaxBytes: 1 << 20},
			want: false,
		},
		{
			name: "limits with max msgs is bounded",
			cfg:  api.StreamConfig{Retention: api.LimitsPolicy, MaxMsgs: 1000},
			want: false,
		},
		{
			name: "workqueue with no caps self-trims, not flagged",
			cfg:  api.StreamConfig{Retention: api.WorkQueuePolicy},
			want: false,
		},
		{
			name: "interest with no caps self-trims, not flagged",
			cfg:  api.StreamConfig{Retention: api.InterestPolicy},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsUnlimited(tc.cfg); got != tc.want {
				t.Errorf("IsUnlimited() = %v, want %v", got, tc.want)
			}
		})
	}
}
