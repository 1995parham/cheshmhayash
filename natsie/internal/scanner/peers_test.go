package scanner

import (
	"context"
	"testing"

	"github.com/nats-io/jsm.go/api"
)

func peerRowByName(rows []PeerRow, name string) *PeerRow {
	for i := range rows {
		if rows[i].Peer == name {
			return &rows[i]
		}
	}

	return nil
}

func TestAggregatePeers(t *testing.T) {
	// Topology: n1 leads everything and is healthy. n2 is a healthy follower.
	// n3 is offline in every group it appears in and leads nothing — a ghost.
	groups := []raftGroup{
		{
			name:   "rides",
			leader: "n1",
			replicas: []api.PeerInfo{
				{Name: "n2", Current: true},
				{Name: "n3", Offline: true},
			},
		},
		{
			name:   "trips",
			leader: "n1",
			replicas: []api.PeerInfo{
				{Name: "n2", Current: true},
				{Name: "n3", Offline: true},
			},
		},
	}

	rows := aggregatePeers("teh1", groups)

	if len(rows) != 3 {
		t.Fatalf("got %d peers, want 3: %+v", len(rows), rows)
	}

	// Ghost sorts first.
	if rows[0].Peer != "n3" || rows[0].Health != PeerGhost {
		t.Errorf("first row = %+v, want n3/GHOST", rows[0])
	}

	n1 := peerRowByName(rows, "n1")
	if n1.Health != PeerOK || n1.Leads != 2 || n1.Current != 2 {
		t.Errorf("n1 = %+v, want OK leads=2 current=2", n1)
	}

	n2 := peerRowByName(rows, "n2")
	if n2.Health != PeerOK || n2.Current != 2 || n2.Leads != 0 {
		t.Errorf("n2 = %+v, want OK current=2 leads=0", n2)
	}

	n3 := peerRowByName(rows, "n3")
	if n3.Health != PeerGhost || n3.Offline != 2 || n3.Current != 0 {
		t.Errorf("n3 = %+v, want GHOST offline=2 current=0", n3)
	}

	if n3.Cluster != "teh1" {
		t.Errorf("cluster = %q, want teh1", n3.Cluster)
	}
}

func TestAggregatePeersLeaderNotDoubleCounted(t *testing.T) {
	// Some server responses echo the leader inside the replica list too.
	groups := []raftGroup{
		{
			name:   "rides",
			leader: "n1",
			replicas: []api.PeerInfo{
				{Name: "n1", Current: true},
				{Name: "n2", Current: true},
			},
		},
	}

	rows := aggregatePeers("teh1", groups)

	n1 := peerRowByName(rows, "n1")
	if n1.Groups != 1 || n1.Leads != 1 {
		t.Errorf("n1 = %+v, want groups=1 leads=1 (no double count)", n1)
	}
}

func TestAggregatePeersLagging(t *testing.T) {
	// n2 is referenced but neither offline nor current anywhere, and never
	// leads — catching up, not a ghost.
	groups := []raftGroup{
		{name: "rides", leader: "n1", replicas: []api.PeerInfo{{Name: "n2"}}},
	}

	rows := aggregatePeers("teh1", groups)

	n2 := peerRowByName(rows, "n2")
	if n2.Health != PeerLagging {
		t.Errorf("n2 health = %q, want LAGGING", n2.Health)
	}
}

func TestAggregatePeersSampleCap(t *testing.T) {
	groups := make([]raftGroup, 0, maxPeerSamples+3)
	for i := range maxPeerSamples + 3 {
		groups = append(groups, raftGroup{
			name:     "s" + string(rune('a'+i)),
			leader:   "n1",
			replicas: []api.PeerInfo{{Name: "n2", Offline: true}},
		})
	}

	rows := aggregatePeers("teh1", groups)

	n2 := peerRowByName(rows, "n2")
	if len(n2.Sample) != maxPeerSamples {
		t.Errorf("sample len = %d, want capped at %d", len(n2.Sample), maxPeerSamples)
	}

	if n2.Groups != maxPeerSamples+3 {
		t.Errorf("groups = %d, want %d (count is not capped)", n2.Groups, maxPeerSamples+3)
	}
}

func TestDerefPeersSkipsNil(t *testing.T) {
	in := []*api.PeerInfo{{Name: "n1"}, nil, {Name: "n2"}}

	out := derefPeers(in)
	if len(out) != 2 {
		t.Fatalf("got %d, want 2 (nil skipped)", len(out))
	}

	if out[0].Name != "n1" || out[1].Name != "n2" {
		t.Errorf("unexpected: %+v", out)
	}
}

// TestCheckPeersAgainstSingleServer proves the I/O walk runs cleanly against a
// real (single-node) JetStream server. A non-clustered server has no Raft
// groups, so no ghost peers can be reported — the point is that it returns
// without error.
func TestCheckPeersAgainstSingleServer(t *testing.T) {
	conn := startJetStream(t)

	rows, err := CheckPeers(context.Background(), conn)
	if err != nil {
		t.Fatalf("CheckPeers: %v", err)
	}

	for _, r := range rows {
		if r.Health == PeerGhost {
			t.Errorf("single-node server should have no ghosts, got %+v", r)
		}
	}
}
