package notify

import (
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name        string
		subject     string
		body        string
		wantTitle   string
		wantBodyHas string
	}{
		{
			name:        "stream created (account-local)",
			subject:     "$JS.EVENT.ADVISORY.STREAM.CREATED.orders",
			body:        `{"stream":"orders","account":"$G"}`,
			wantTitle:   "Stream `orders` created",
			wantBodyHas: "orders",
		},
		{
			name:        "stream deleted (sys-bridged)",
			subject:     "$SYS.ACCOUNT.$G.JETSTREAM.EVENT.ADVISORY.STREAM.DELETED.orders",
			body:        `{"stream":"orders"}`,
			wantTitle:   "Stream `orders` deleted",
			wantBodyHas: "orders",
		},
		{
			name:        "stream leader elected with replicas as []map",
			subject:     "$JS.EVENT.ADVISORY.STREAM.LEADER_ELECTED.orders",
			body:        `{"stream":"orders","leader":"n-1","replicas":[{"name":"n-1"},{"name":"n-2"},{"name":"n-3"}]}`,
			wantTitle:   "Stream `orders` leader elected",
			wantBodyHas: "n-1",
		},
		{
			name:        "consumer created",
			subject:     "$JS.EVENT.ADVISORY.CONSUMER.CREATED.orders.processor",
			body:        `{"stream":"orders","consumer":"processor","account":"$G"}`,
			wantTitle:   "Consumer `orders/processor` created",
			wantBodyHas: "processor",
		},
		{
			name:        "consumer leader elected",
			subject:     "$JS.EVENT.ADVISORY.CONSUMER.LEADER_ELECTED.orders.processor",
			body:        `{"leader":"n-2"}`,
			wantTitle:   "Consumer `orders/processor` leader elected",
			wantBodyHas: "n-2",
		},
		{
			name:        "stream quorum lost",
			subject:     "$JS.EVENT.ADVISORY.STREAM.QUORUM_LOST.orders",
			body:        `{"stream":"orders"}`,
			wantTitle:   "⚠ Stream `orders` lost quorum",
			wantBodyHas: "quorum",
		},
		{
			name:    "unknown action drops to nil",
			subject: "$JS.EVENT.ADVISORY.STREAM.SOMETHING.orders",
		},
		{
			name:    "non-advisory subject drops to nil",
			subject: "$SYS.SERVER.foo.STATSZ",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classify(tc.subject, []byte(tc.body))
			if tc.wantTitle == "" {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected event, got nil")
			}
			if got.Title != tc.wantTitle {
				t.Errorf("title: got %q want %q", got.Title, tc.wantTitle)
			}
			if tc.wantBodyHas != "" && !strings.Contains(got.Body, tc.wantBodyHas) {
				t.Errorf("body %q must contain %q", got.Body, tc.wantBodyHas)
			}
		})
	}
}
