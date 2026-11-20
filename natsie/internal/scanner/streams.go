package scanner

import (
	"context"
	"fmt"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"

	"github.com/1995parham/natsie/internal/infra/natsctx"
)

// StreamRow describes one stream flagged by ScanUnlimitedStreams.
type StreamRow struct {
	Cluster  string `json:"cluster"`
	Stream   string `json:"stream"`
	Replicas int    `json:"replicas"`
	Messages uint64 `json:"messages"`
	Bytes    uint64 `json:"bytes"`
}

// IsUnlimited reports whether a stream can grow without bound: a Limits
// retention policy with no cap on age, size, or message count. WorkQueue and
// Interest streams self-trim on ack/interest, so a missing limit there is not
// a growth risk and is deliberately not flagged.
func IsUnlimited(cfg api.StreamConfig) bool {
	if cfg.Retention != api.LimitsPolicy {
		return false
	}

	return cfg.MaxAge <= 0 && cfg.MaxBytes <= 0 && cfg.MaxMsgs <= 0
}

// ScanUnlimitedStreams lists every stream on nc and returns those that can
// grow without bound (see IsUnlimited). A stream that fails to load is
// skipped rather than failing the whole scan — a report should surface what
// it can.
func ScanUnlimitedStreams(ctx context.Context, nc *natsctx.Conn) ([]StreamRow, error) {
	mgr, err := jsm.New(nc.Conn)
	if err != nil {
		return nil, fmt.Errorf("jsm.New: %w", err)
	}

	names, err := mgr.StreamNames(nil)
	if err != nil {
		return nil, fmt.Errorf("stream names: %w", err)
	}

	var rows []StreamRow

	for _, sn := range names {
		if ctx.Err() != nil {
			return rows, ctx.Err()
		}

		stream, err := mgr.LoadStream(sn)
		if err != nil {
			continue
		}

		if !IsUnlimited(stream.Configuration()) {
			continue
		}

		row := StreamRow{
			Cluster:  nc.Name,
			Stream:   sn,
			Replicas: stream.Configuration().Replicas,
		}

		if state, err := stream.State(); err == nil {
			row.Messages = state.Msgs
			row.Bytes = state.Bytes
		}

		rows = append(rows, row)
	}

	return rows, nil
}
