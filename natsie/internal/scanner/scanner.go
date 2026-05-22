// Package scanner walks JetStream streams and consumers and classifies them.
//
// A consumer is classified as:
//
//	ACTIVE — push-bound, has pull waiters, or its last delivery is within
//	         Options.MinIdle.
//	STALE  — exists but is past Options.MinIdle and exceeds Options.MinPending.
//	OK     — exists and is healthy but does not meet the reporting thresholds.
//	ABSENT — only used in PeerStatus; consumer does not exist on the peer.
//	ERR    — could not be queried.
package scanner

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"

	"github.com/1995parham/natsie/internal/infra/natsctx"
)

type Status string

const (
	StatusActive Status = "ACTIVE"
	StatusStale  Status = "STALE"
	StatusOK     Status = "OK"
	StatusAbsent Status = "ABSENT"
	StatusError  Status = "ERR"
)

// consumerNotFoundCode is the JetStream API error code for "consumer not found".
// See: github.com/nats-io/jsm.go/consumers.go.
const consumerNotFoundCode = 10014

type Options struct {
	Stream     string
	MinPending int64
	MinIdle    time.Duration
}

type Row struct {
	Cluster    string        `json:"cluster"`
	Stream     string        `json:"stream"`
	Consumer   string        `json:"consumer"`
	Status     Status        `json:"status"`
	NumPending int64         `json:"num_pending"`
	NumWaiting int           `json:"num_waiting"`
	PushBound  bool          `json:"push_bound"`
	LastAck    time.Time     `json:"last_ack,omitzero"`
	Idle       time.Duration `json:"idle"`
	PeerStatus Status        `json:"peer_status,omitempty"`
}

// Scan enumerates streams and consumers on nc, classifies them per opts, and
// (if peer is non-nil) annotates each row with the peer-cluster status of the
// same (stream, consumer) pair.
func Scan(ctx context.Context, nc, peer *natsctx.Conn, opts Options) ([]Row, error) {
	mgr, err := jsm.New(nc.Conn)
	if err != nil {
		return nil, fmt.Errorf("jsm.New: %w", err)
	}

	var peerMgr *jsm.Manager
	if peer != nil {
		peerMgr, err = jsm.New(peer.Conn)
		if err != nil {
			return nil, fmt.Errorf("jsm.New(peer): %w", err)
		}
	}

	streamNames, err := mgr.StreamNames(nil)
	if err != nil {
		return nil, fmt.Errorf("stream names: %w", err)
	}

	now := time.Now()

	var rows []Row

	for _, sn := range streamNames {
		if opts.Stream != "" && sn != opts.Stream {
			continue
		}

		if ctx.Err() != nil {
			return rows, ctx.Err()
		}

		stream, err := mgr.LoadStream(sn)
		if err != nil {
			rows = append(rows, Row{Cluster: nc.Name, Stream: sn, Status: StatusError})

			continue
		}

		consumerNames, err := stream.ConsumerNames()
		if err != nil {
			rows = append(rows, Row{Cluster: nc.Name, Stream: sn, Status: StatusError})

			continue
		}

		for _, cn := range consumerNames {
			r := classify(mgr, sn, cn, nc.Name, now, opts)
			if !meetsThresholds(r, opts) {
				continue
			}

			if peerMgr != nil {
				r.PeerStatus = peerStatus(peerMgr, sn, cn, now, opts)
			}

			rows = append(rows, r)
		}
	}

	return rows, nil
}

func classify(mgr *jsm.Manager, stream, consumer, cluster string, now time.Time, opts Options) Row {
	r := Row{Cluster: cluster, Stream: stream, Consumer: consumer, Status: StatusError}

	c, err := mgr.LoadConsumer(stream, consumer)
	if err != nil {
		return r
	}

	info, err := c.LatestState()
	if err != nil {
		return r
	}

	r.NumPending = int64(info.NumPending) //nolint:gosec // counts are bounded by stream depth, well under int64
	r.NumWaiting = info.NumWaiting

	r.PushBound = info.PushBound
	if info.AckFloor.Last != nil {
		r.LastAck = *info.AckFloor.Last
	}

	switch {
	case !r.LastAck.IsZero():
		r.Idle = now.Sub(r.LastAck)
	case !info.Created.IsZero():
		r.Idle = now.Sub(info.Created)
	}

	switch {
	case info.PushBound || info.NumWaiting > 0:
		r.Status = StatusActive
	case !r.LastAck.IsZero() && now.Sub(r.LastAck) < opts.MinIdle:
		r.Status = StatusActive
	case r.NumPending == 0 && r.LastAck.IsZero():
		// Never delivered, no backlog — too quiet to call stale.
		r.Status = StatusOK
	default:
		r.Status = StatusStale
	}

	return r
}

func peerStatus(mgr *jsm.Manager, stream, consumer string, now time.Time, opts Options) Status {
	c, err := mgr.LoadConsumer(stream, consumer)
	if err != nil {
		if api.IsNatsError(err, consumerNotFoundCode) {
			return StatusAbsent
		}

		return StatusError
	}

	info, err := c.LatestState()
	if err != nil {
		return StatusError
	}

	switch {
	case info.PushBound || info.NumWaiting > 0:
		return StatusActive
	case info.AckFloor.Last != nil && now.Sub(*info.AckFloor.Last) < opts.MinIdle:
		return StatusActive
	default:
		return StatusStale
	}
}

func meetsThresholds(r Row, opts Options) bool {
	if r.Status == StatusActive {
		return true
	}

	if r.NumPending < opts.MinPending {
		return false
	}

	return true
}
