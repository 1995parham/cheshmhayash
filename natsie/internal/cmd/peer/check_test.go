package peer

import (
	"testing"

	"github.com/1995parham/natsie/internal/scanner"
)

func TestFilterGhosts(t *testing.T) {
	rows := []scanner.PeerRow{
		{Peer: "n3", Health: scanner.PeerGhost},
		{Peer: "n1", Health: scanner.PeerOK},
		{Peer: "n2", Health: scanner.PeerLagging},
		{Peer: "n4", Health: scanner.PeerGhost},
	}

	got := filterGhosts(rows)

	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(got), got)
	}

	for _, r := range got {
		if r.Health != scanner.PeerGhost {
			t.Errorf("non-ghost survived filter: %+v", r)
		}
	}
}

func TestFilterGhostsEmpty(t *testing.T) {
	got := filterGhosts([]scanner.PeerRow{
		{Peer: "n1", Health: scanner.PeerOK},
	})

	if len(got) != 0 {
		t.Errorf("got %d, want 0", len(got))
	}
}
