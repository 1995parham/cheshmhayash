package chatops_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/1995parham/natsie/internal/chatops"
	"github.com/1995parham/natsie/internal/infra/store"
	"github.com/1995parham/natsie/internal/manifest"
)

func TestDispatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	dir := t.TempDir()

	st, err := store.Dial("file://" + dir)
	if err != nil {
		t.Fatalf("dial store: %v", err)
	}

	m := &manifest.Manifest{
		Version:     manifest.Version,
		GeneratedAt: time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC),
		Entries: []manifest.Entry{
			{Stream: "rides", Consumer: "stale-1", NumPending: 12345, Idle: 25 * time.Hour},
		},
	}

	if err := st.Put(ctx, "demo", m); err != nil {
		t.Fatalf("put: %v", err)
	}

	cases := []struct {
		name    string
		argv    []string
		wantSub string
	}{
		{"empty defaults to help", nil, "natsie commands"},
		{"help", []string{"help"}, "natsie commands"},
		{"list", []string{"list"}, "demo"},
		{"show existing", []string{"show", "demo"}, "`stale-1`"},
		{"show missing", []string{"show", "nope"}, "not found"},
		{"unknown", []string{"frobnicate"}, "unknown subcommand"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := chatops.Dispatch(ctx, st, "!natsie", c.argv)
			if !strings.Contains(got, c.wantSub) {
				t.Errorf("Dispatch(%v) = %q, want substring %q", c.argv, got, c.wantSub)
			}
		})
	}
}

func TestHelpUsesTrigger(t *testing.T) {
	t.Parallel()

	got := chatops.Help("!natsie")
	if !strings.Contains(got, "!natsie list") {
		t.Errorf("Help did not interpolate trigger: %q", got)
	}
}
