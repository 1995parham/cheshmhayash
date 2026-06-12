package scanner

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/nats-io/jsm.go"

	"github.com/1995parham/natsie/internal/infra/natsctx"
)

// OwnerMatch is one consumer, on some cluster, that filters the subject a
// resolution was asked about. The active match (if any) is the service that
// currently owns the subject; stale matches are its abandoned or superseded
// siblings.
type OwnerMatch struct {
	Cluster    string        `json:"cluster"`
	Stream     string        `json:"stream"`
	Consumer   string        `json:"consumer"`
	Status     Status        `json:"status"`
	NumPending int64         `json:"num_pending"`
	LastAck    time.Time     `json:"last_ack,omitzero"`
	Idle       time.Duration `json:"idle"`
}

// ResolveOwner searches every connection in conns for consumers that filter
// exactly `subject` and returns each match, classified and sorted so the
// likeliest current owner (active, most recently acked) leads. A cluster that
// fails to enumerate is skipped rather than failing the whole resolution — the
// answer should reflect every cluster it could reach.
//
// Subject comparison is exact against the consumer's normalized filter (see
// filterSubject), matching the same-subject semantics rename detection uses;
// pass the filter_subject exactly as `scan` reports it.
func ResolveOwner(ctx context.Context, conns []*natsctx.Conn, subject string, opts Options) ([]OwnerMatch, error) {
	now := time.Now()

	var matches []OwnerMatch

	for _, nc := range conns {
		mgr, err := jsm.New(nc.Conn)
		if err != nil {
			return nil, fmt.Errorf("jsm.New(%s): %w", nc.Name, err)
		}

		streamNames, err := mgr.StreamNames(nil)
		if err != nil {
			continue
		}

		for _, sn := range streamNames {
			if ctx.Err() != nil {
				return matches, ctx.Err()
			}

			stream, err := mgr.LoadStream(sn)
			if err != nil {
				continue
			}

			consumerNames, err := stream.ConsumerNames()
			if err != nil {
				continue
			}

			for _, cn := range consumerNames {
				r := classify(mgr, sn, cn, nc.Name, now, opts)
				if r.FilterSubject != subject {
					continue
				}

				matches = append(matches, OwnerMatch{
					Cluster:    nc.Name,
					Stream:     sn,
					Consumer:   cn,
					Status:     r.Status,
					NumPending: r.NumPending,
					LastAck:    r.LastAck,
					Idle:       r.Idle,
				})
			}
		}
	}

	sortOwnerMatches(matches)

	return matches, nil
}

// sortOwnerMatches orders matches with active consumers first, then by most
// recent ack, then by cluster/stream/consumer for a stable result.
func sortOwnerMatches(matches []OwnerMatch) {
	sort.Slice(matches, func(i, j int) bool {
		ai, aj := matches[i].Status == StatusActive, matches[j].Status == StatusActive
		if ai != aj {
			return ai
		}

		if !matches[i].LastAck.Equal(matches[j].LastAck) {
			return matches[i].LastAck.After(matches[j].LastAck)
		}

		if matches[i].Cluster != matches[j].Cluster {
			return matches[i].Cluster < matches[j].Cluster
		}

		if matches[i].Stream != matches[j].Stream {
			return matches[i].Stream < matches[j].Stream
		}

		return matches[i].Consumer < matches[j].Consumer
	})
}

// Owner returns the cluster/consumer that currently owns the subject — the
// first active match — or false when no active owner was found.
func Owner(matches []OwnerMatch) (OwnerMatch, bool) {
	for _, m := range matches {
		if m.Status == StatusActive {
			return m, true
		}
	}

	return OwnerMatch{}, false
}
