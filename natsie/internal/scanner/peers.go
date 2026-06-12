package scanner

import (
	"context"
	"fmt"
	"sort"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"

	"github.com/1995parham/natsie/internal/infra/natsctx"
)

// PeerHealth classifies a server by how it appears across the Raft groups
// (stream and consumer replica sets) that reference it.
type PeerHealth string

const (
	// PeerOK — the peer leads or is current in at least one group, so the
	// server is demonstrably alive.
	PeerOK PeerHealth = "OK"
	// PeerLagging — the peer is referenced and not offline everywhere, but is
	// not current in any group: catching up, or partially wedged.
	PeerLagging PeerHealth = "LAGGING"
	// PeerGhost — the peer is offline in every group that references it and
	// leads none. This is the classic "removed server still pinned into Raft
	// replica sets" — a peer-remove that never happened.
	PeerGhost PeerHealth = "GHOST"
)

// PeerRow is one server's aggregated standing across all Raft groups on a
// cluster.
type PeerRow struct {
	Cluster string     `json:"cluster"`
	Peer    string     `json:"peer"`
	Health  PeerHealth `json:"health"`
	Groups  int        `json:"groups"`           // raft groups that reference this peer
	Offline int        `json:"offline"`          // groups where it's reported offline
	Current int        `json:"current"`          // groups where it's caught up
	Leads   int        `json:"leads"`            // groups where it's the leader
	Sample  []string   `json:"sample,omitempty"` // example group names referencing it
}

// raftGroup is one stream- or consumer-level Raft group's membership, the raw
// input to aggregatePeers. Kept separate from the I/O walk so the
// classification is unit-testable without a live cluster.
type raftGroup struct {
	name     string
	leader   string
	replicas []api.PeerInfo
}

const maxPeerSamples = 5

// CheckPeers walks every stream and consumer Raft group on nc and reports each
// server's standing, flagging likely ghost peers (offline everywhere, leading
// nothing). A group that can't be loaded — e.g. an R1 asset with no cluster
// info — is skipped rather than failing the whole check.
func CheckPeers(ctx context.Context, nc *natsctx.Conn) ([]PeerRow, error) {
	mgr, err := jsm.New(nc.Conn)
	if err != nil {
		return nil, fmt.Errorf("jsm.New: %w", err)
	}

	streamNames, err := mgr.StreamNames(nil)
	if err != nil {
		return nil, fmt.Errorf("stream names: %w", err)
	}

	var groups []raftGroup

	for _, sn := range streamNames {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		stream, err := mgr.LoadStream(sn)
		if err != nil {
			continue
		}

		if ci, err := stream.ClusterInfo(); err == nil {
			groups = append(groups, raftGroup{name: sn, leader: ci.Leader, replicas: derefPeers(ci.Replicas)})
		}

		consumerNames, err := stream.ConsumerNames()
		if err != nil {
			continue
		}

		for _, cn := range consumerNames {
			c, err := mgr.LoadConsumer(sn, cn)
			if err != nil {
				continue
			}

			if ci, err := c.ClusterInfo(); err == nil {
				groups = append(groups, raftGroup{name: sn + "/" + cn, leader: ci.Leader, replicas: derefPeers(ci.Replicas)})
			}
		}
	}

	return aggregatePeers(nc.Name, groups), nil
}

// derefPeers copies the value of each non-nil *PeerInfo so the aggregation
// works on a flat, nil-free slice.
func derefPeers(in []*api.PeerInfo) []api.PeerInfo {
	out := make([]api.PeerInfo, 0, len(in))
	for _, p := range in {
		if p != nil {
			out = append(out, *p)
		}
	}

	return out
}

// aggregatePeers folds the per-group membership into one row per server and
// classifies each. A group's leader counts as a current, leading reference;
// each replica contributes its offline/current state. Rows are sorted ghosts
// first, then by name, so the most actionable peers lead the report.
func aggregatePeers(cluster string, groups []raftGroup) []PeerRow {
	type agg struct {
		groups, offline, current, leads int
		sample                          []string
	}

	byPeer := map[string]*agg{}

	touch := func(name, group string) *agg {
		a := byPeer[name]
		if a == nil {
			a = &agg{}
			byPeer[name] = a
		}

		a.groups++
		if len(a.sample) < maxPeerSamples {
			a.sample = append(a.sample, group)
		}

		return a
	}

	for _, g := range groups {
		if g.leader != "" {
			a := touch(g.leader, g.name)
			a.leads++
			a.current++
		}

		for _, p := range g.replicas {
			// The leader can also appear in the replica list; don't double count.
			if p.Name == g.leader {
				continue
			}

			a := touch(p.Name, g.name)
			if p.Offline {
				a.offline++
			}

			if p.Current {
				a.current++
			}
		}
	}

	rows := make([]PeerRow, 0, len(byPeer))

	for name, a := range byPeer {
		rows = append(rows, PeerRow{
			Cluster: cluster,
			Peer:    name,
			Health:  classifyPeer(a.groups, a.offline, a.current, a.leads),
			Groups:  a.groups,
			Offline: a.offline,
			Current: a.current,
			Leads:   a.leads,
			Sample:  a.sample,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if (rows[i].Health == PeerGhost) != (rows[j].Health == PeerGhost) {
			return rows[i].Health == PeerGhost
		}

		return rows[i].Peer < rows[j].Peer
	})

	return rows
}

// GhostPeers returns, in a fresh slice, only the rows classified GHOST.
func GhostPeers(rows []PeerRow) []PeerRow {
	var out []PeerRow

	for _, r := range rows {
		if r.Health == PeerGhost {
			out = append(out, r)
		}
	}

	return out
}

func classifyPeer(groups, offline, current, leads int) PeerHealth {
	switch {
	case current > 0 || leads > 0:
		return PeerOK
	case groups > 0 && offline == groups:
		return PeerGhost
	default:
		return PeerLagging
	}
}
