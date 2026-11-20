package serve

import (
	"maps"
	"slices"
	"testing"
)

func TestParseComponents(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    []Component
		wantErr bool
	}{
		{name: "all", in: []string{"api", "ui", "notify"}, want: []Component{ComponentAPI, ComponentUI, ComponentNotify}},
		{name: "comma separated", in: []string{"api,notify"}, want: []Component{ComponentAPI, ComponentNotify}},
		{name: "spaces trimmed", in: []string{" api , ui "}, want: []Component{ComponentAPI, ComponentUI}},
		{name: "duplicates", in: []string{"notify", "notify"}, want: []Component{ComponentNotify}},
		{name: "unknown", in: []string{"api", "mcp"}, wantErr: true},
		{name: "empty name", in: []string{"api,"}, wantErr: true},
		{name: "none", in: nil, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseComponents(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseComponents(%q) = %v, want an error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseComponents(%q): %v", tt.in, err)
			}
			keys := slices.Sorted(maps.Keys(got))
			want := slices.Sorted(slices.Values(tt.want))
			if !slices.Equal(keys, want) {
				t.Fatalf("ParseComponents(%q) = %v, want %v", tt.in, keys, want)
			}
		})
	}
}

func TestSelectedKeepsCanonicalOrder(t *testing.T) {
	got := selected(map[Component]bool{ComponentNotify: true, ComponentAPI: true})
	want := []Component{ComponentAPI, ComponentNotify}
	if !slices.Equal(got, want) {
		t.Fatalf("selected = %v, want %v", got, want)
	}
}
