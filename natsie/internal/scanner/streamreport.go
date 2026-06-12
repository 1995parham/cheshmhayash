package scanner

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"

	"github.com/1995parham/natsie/internal/infra/natsctx"
)

// StreamReportRow is one stream's size, retention, replication, and placement.
type StreamReportRow struct {
	Cluster   string        `json:"cluster"`
	Stream    string        `json:"stream"`
	Retention string        `json:"retention"`
	Replicas  int           `json:"replicas"`
	MaxAge    time.Duration `json:"max_age,omitempty"`
	MaxBytes  int64         `json:"max_bytes,omitempty"`
	MaxMsgs   int64         `json:"max_msgs,omitempty"`
	Messages  uint64        `json:"messages"`
	Bytes     uint64        `json:"bytes"`
	Consumers int           `json:"consumers"`
	Leader    string        `json:"leader,omitempty"`
	Peers     []string      `json:"peers,omitempty"` // servers hosting a replica, leader first
}

// ServerLoad is one server's share of stream placement across the cluster — how
// many streams it leads and how many replicas it hosts overall. Surfacing it
// exposes per-pod placement skew (one pod leading or hosting far more than its
// share).
type ServerLoad struct {
	Server   string `json:"server"`
	Leads    int    `json:"leads"`
	Replicas int    `json:"replicas"`
}

// ReportStreams enumerates streams on nc (optionally filtered to one stream)
// and reports each one's configuration, size, and placement. A stream that
// fails to load is skipped rather than failing the whole report.
func ReportStreams(ctx context.Context, nc *natsctx.Conn, stream string) ([]StreamReportRow, error) {
	mgr, err := jsm.New(nc.Conn)
	if err != nil {
		return nil, fmt.Errorf("jsm.New: %w", err)
	}

	names, err := mgr.StreamNames(nil)
	if err != nil {
		return nil, fmt.Errorf("stream names: %w", err)
	}

	var rows []StreamReportRow

	for _, sn := range names {
		if stream != "" && sn != stream {
			continue
		}

		if ctx.Err() != nil {
			return rows, ctx.Err()
		}

		s, err := mgr.LoadStream(sn)
		if err != nil {
			continue
		}

		cfg := s.Configuration()
		row := StreamReportRow{
			Cluster:   nc.Name,
			Stream:    sn,
			Retention: cfg.Retention.String(),
			Replicas:  cfg.Replicas,
			MaxAge:    cfg.MaxAge,
			MaxBytes:  cfg.MaxBytes,
			MaxMsgs:   cfg.MaxMsgs,
		}

		if state, err := s.State(); err == nil {
			row.Messages = state.Msgs
			row.Bytes = state.Bytes
			row.Consumers = state.Consumers
		}

		if ci, err := s.ClusterInfo(); err == nil {
			row.Leader = ci.Leader
			row.Peers = placementPeers(ci)
		}

		rows = append(rows, row)
	}

	return rows, nil
}

// placementPeers returns the servers hosting a replica of the group, leader
// first, each server once.
func placementPeers(ci api.ClusterInfo) []string {
	peers := make([]string, 0, len(ci.Replicas)+1)

	if ci.Leader != "" {
		peers = append(peers, ci.Leader)
	}

	for _, p := range ci.Replicas {
		if p != nil && p.Name != "" && p.Name != ci.Leader {
			peers = append(peers, p.Name)
		}
	}

	return peers
}

// PlacementSkew folds the per-stream placement into one row per server, sorted
// by leadership then replica count, so the most loaded pods lead the summary.
func PlacementSkew(rows []StreamReportRow) []ServerLoad {
	type load struct{ leads, replicas int }

	byServer := map[string]*load{}

	for _, r := range rows {
		for _, p := range r.Peers {
			l := byServer[p]
			if l == nil {
				l = &load{}
				byServer[p] = l
			}

			l.replicas++

			if p == r.Leader {
				l.leads++
			}
		}
	}

	out := make([]ServerLoad, 0, len(byServer))
	for server, l := range byServer {
		out = append(out, ServerLoad{Server: server, Leads: l.leads, Replicas: l.replicas})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Leads != out[j].Leads {
			return out[i].Leads > out[j].Leads
		}

		if out[i].Replicas != out[j].Replicas {
			return out[i].Replicas > out[j].Replicas
		}

		return out[i].Server < out[j].Server
	})

	return out
}
