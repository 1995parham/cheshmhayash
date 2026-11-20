package owners

import (
	"reflect"
	"sort"
	"testing"

	"github.com/1995parham/natsie/internal/infra/config"
	"github.com/1995parham/natsie/internal/manifest"
)

func TestNewRouterRejectsEmptyOwner(t *testing.T) {
	_, err := NewRouter([]config.Owner{{Name: "empty"}})
	if err == nil {
		t.Fatal("expected error for empty rules")
	}
}

func TestNewRouterRejectsMissingName(t *testing.T) {
	_, err := NewRouter([]config.Owner{{Streams: []string{"rides"}}})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestMatchByStream(t *testing.T) {
	r, err := NewRouter([]config.Owner{
		{Name: "rides-team", Streams: []string{"rides"}},
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	got := r.Match(manifest.Entry{Stream: "rides", Consumer: "anything"})
	if got != "rides-team" {
		t.Errorf("got %q want rides-team", got)
	}

	if other := r.Match(manifest.Entry{Stream: "other", Consumer: "x"}); other != "" {
		t.Errorf("got %q want \"\"", other)
	}
}

func TestMatchByConsumerPrefix(t *testing.T) {
	r, err := NewRouter([]config.Owner{
		{Name: "chronos", ConsumerPrefix: []string{"chronos-"}},
		{Name: "dolos", ConsumerPrefix: []string{"dolos-", "dolos-dolos-"}},
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	cases := []struct{ consumer, want string }{
		{"chronos-ride-started", "chronos"},
		{"chronos-anything", "chronos"},
		{"dolos-ride-finished", "dolos"},
		{"dolos-dolos-ride-accepted", "dolos"},
		{"gabriel-ride-x", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := r.Match(manifest.Entry{Consumer: c.consumer})
		if got != c.want {
			t.Errorf("Match(%q)=%q want %q", c.consumer, got, c.want)
		}
	}
}

func TestMatchFirstWins(t *testing.T) {
	r, _ := NewRouter([]config.Owner{
		{Name: "general-rides", Streams: []string{"rides"}},
		{Name: "chronos-specific", ConsumerPrefix: []string{"chronos-"}},
	})
	// chronos-ride-x on rides stream — owner ordering matters; the
	// rides-team should win because it comes first.
	got := r.Match(manifest.Entry{Stream: "rides", Consumer: "chronos-ride-x"})
	if got != "general-rides" {
		t.Errorf("got %q want general-rides (first-match-wins)", got)
	}
}

func TestEmptyPrefixIgnored(t *testing.T) {
	// An empty string in ConsumerPrefix would match every consumer
	// (strings.HasPrefix("anything", "") == true). Defend against that.
	r, _ := NewRouter([]config.Owner{
		{Name: "everything", ConsumerPrefix: []string{""}, Streams: []string{"x"}},
	})
	if got := r.Match(manifest.Entry{Stream: "y", Consumer: "z"}); got != "" {
		t.Errorf("empty prefix should not match anything, got %q", got)
	}
}

func TestGroup(t *testing.T) {
	r, _ := NewRouter([]config.Owner{
		{Name: "chronos", ConsumerPrefix: []string{"chronos-"}},
		{Name: "seraat", ConsumerPrefix: []string{"seraat-"}},
	})
	entries := []manifest.Entry{
		{Stream: "rides", Consumer: "chronos-ride-started"},
		{Stream: "rides", Consumer: "seraat-dpn"},
		{Stream: "rides", Consumer: "chronos-ride-finished"},
		{Stream: "rides", Consumer: "gabriel-orphan"},
	}
	got := r.Group(entries)

	if len(got["chronos"]) != 2 {
		t.Errorf("chronos=%d want 2", len(got["chronos"]))
	}

	if len(got["seraat"]) != 1 {
		t.Errorf("seraat=%d want 1", len(got["seraat"]))
	}

	if len(got[""]) != 1 {
		t.Errorf("unmatched=%d want 1", len(got[""]))
	}
}

func TestNames(t *testing.T) {
	r, _ := NewRouter([]config.Owner{
		{Name: "alpha", Streams: []string{"x"}},
		{Name: "bravo", Streams: []string{"y"}},
	})
	got := r.Names()

	want := []string{"alpha", "bravo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Names=%v want %v", got, want)
	}
}

func TestEmptyRouterMatchesNothing(t *testing.T) {
	r, _ := NewRouter(nil)
	if got := r.Match(manifest.Entry{Stream: "x", Consumer: "y"}); got != "" {
		t.Errorf("empty router matched %q", got)
	}

	group := r.Group([]manifest.Entry{{Consumer: "a"}, {Consumer: "b"}})
	if len(group[""]) != 2 {
		t.Errorf("unmatched=%d want 2", len(group[""]))
	}
	// Make sure no spurious other keys appeared.
	keys := make([]string, 0, len(group))
	for k := range group {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	if !reflect.DeepEqual(keys, []string{""}) {
		t.Errorf("group keys=%v want [\"\"]", keys)
	}
}
