package chatops_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/1995parham/cheshmhayash/internal/ops/chatops"
	"github.com/1995parham/cheshmhayash/internal/ops/infra/store"
	"github.com/1995parham/cheshmhayash/internal/ops/manifest"
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
		{"empty defaults to help", nil, "cheshmhayash commands"},
		{"help", []string{"help"}, "cheshmhayash commands"},
		{"list", []string{"list"}, "demo"},
		{"show existing", []string{"show", "demo"}, "`stale-1`"},
		{"show missing", []string{"show", "nope"}, "not found"},
		{"last needs args", []string{"last"}, "usage:"},
		{"last needs stream", []string{"last", "ctx"}, "usage:"},
		{"unknown", []string{"frobnicate"}, "unknown subcommand"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := chatops.Dispatch(ctx, chatops.Deps{Store: st}, "!cheshmhayash", c.argv)
			if !strings.Contains(got, c.wantSub) {
				t.Errorf("Dispatch(%v) = %q, want substring %q", c.argv, got, c.wantSub)
			}
		})
	}
}

func TestHelpUsesTrigger(t *testing.T) {
	t.Parallel()

	got := chatops.Help("!cheshmhayash")
	if !strings.Contains(got, "!cheshmhayash list") {
		t.Errorf("Help did not interpolate trigger: %q", got)
	}
}
