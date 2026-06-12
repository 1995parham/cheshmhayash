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
	"slices"
	"strings"
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
	Cluster       string        `json:"cluster"`
	Stream        string        `json:"stream"`
	Consumer      string        `json:"consumer"`
	Status        Status        `json:"status"`
	NumPending    int64         `json:"num_pending"`
	NumWaiting    int           `json:"num_waiting"`
	PushBound     bool          `json:"push_bound"`
	LastAck       time.Time     `json:"last_ack,omitzero"`
	Idle          time.Duration `json:"idle"`
	PeerStatus    Status        `json:"peer_status,omitempty"`
	FilterSubject string        `json:"filter_subject,omitempty"`
	// RenamedTo names an ACTIVE consumer on the same stream that filters the
	// same subject as this (STALE) consumer — i.e. this looks like the old
	// name of a rename migration rather than an abandoned consumer. Empty
	// when no such successor was found.
	RenamedTo string `json:"renamed_to,omitempty"`
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

	var all []Row

	for _, sn := range streamNames {
		if opts.Stream != "" && sn != opts.Stream {
			continue
		}

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		stream, err := mgr.LoadStream(sn)
		if err != nil {
			all = append(all, Row{Cluster: nc.Name, Stream: sn, Status: StatusError})

			continue
		}

		consumerNames, err := stream.ConsumerNames()
		if err != nil {
			all = append(all, Row{Cluster: nc.Name, Stream: sn, Status: StatusError})

			continue
		}

		for _, cn := range consumerNames {
			all = append(all, classify(mgr, sn, cn, nc.Name, now, opts))
		}
	}

	// Rename detection runs over the full classified set — before the output
	// thresholds — so a stale consumer can be matched to its active successor
	// even when that successor wouldn't otherwise be reported.
	detectRenames(all)

	var rows []Row

	for _, r := range all {
		// Stream-level errors carry no consumer name and are always surfaced.
		if r.Status == StatusError && r.Consumer == "" {
			rows = append(rows, r)

			continue
		}

		if !meetsThresholds(r, opts) {
			continue
		}

		if peerMgr != nil {
			r.PeerStatus = peerStatus(peerMgr, r.Stream, r.Consumer, now, opts)
		}

		rows = append(rows, r)
	}

	return rows, nil
}

// detectRenames annotates each STALE row that looks like the old name of a
// rename migration: another consumer on the same stream filters the same
// subject and is still ACTIVE. When several active consumers qualify, the
// most recently active one wins.
func detectRenames(rows []Row) {
	type key struct{ stream, subject string }

	active := map[key]Row{}

	for _, r := range rows {
		if r.Status != StatusActive || r.FilterSubject == "" {
			continue
		}

		k := key{r.Stream, r.FilterSubject}
		if cur, ok := active[k]; !ok || r.LastAck.After(cur.LastAck) {
			active[k] = r
		}
	}

	if len(active) == 0 {
		return
	}

	for i := range rows {
		if rows[i].Status != StatusStale || rows[i].FilterSubject == "" {
			continue
		}

		if t, ok := active[key{rows[i].Stream, rows[i].FilterSubject}]; ok && t.Consumer != rows[i].Consumer {
			rows[i].RenamedTo = t.Consumer
		}
	}
}

// filterSubject normalizes a consumer's subject filter into a single,
// order-independent string so two consumers with the same filter set compare
// equal regardless of how the slice was ordered.
func filterSubject(cfg api.ConsumerConfig) string {
	if cfg.FilterSubject != "" {
		return cfg.FilterSubject
	}

	if len(cfg.FilterSubjects) == 0 {
		return ""
	}

	subs := slices.Clone(cfg.FilterSubjects)
	slices.Sort(subs)

	return strings.Join(subs, ",")
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
	r.FilterSubject = filterSubject(info.Config)

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
