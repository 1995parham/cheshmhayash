// Package cleanup applies a manifest against live NATS clusters.
//
// Apply is the operationally important function. It is shared by the
// `natsie consumer apply` CLI and the bot's signed-approval HTTP handler,
// so both code paths run the same re-verification and deletion logic.
//
// Two safety properties are baked in:
//
//  1. Re-verification: each entry's current state is re-queried before
//     deletion. Anything that has become active (push-bound, has pull
//     waiters, or has acked since the manifest's GeneratedAt) is
//     preserved untouched.
//
//  2. Per-entry deletes via the raw `$JS.API.CONSUMER.DELETE` subject,
//     so consumer names starting with `-` (which the nats CLI rejects
//     as flags) are handled the same way as any other name.
package cleanup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/1995parham/natsie/internal/manifest"
)

const (
	jsAPIInfoSubject   = "$JS.API.CONSUMER.INFO.%s.%s"
	jsAPIDeleteSubject = "$JS.API.CONSUMER.DELETE.%s.%s"
	jsAPITimeout       = 15 * time.Second
	errCodeNotFound    = 10014
)

// Connector returns a NATS connection for the named cluster, along with
// a function the caller invokes to release it. The CLI passes
// natsctx.Connect-backed connectors; tests can pass an in-memory fake.
type Connector func(cluster string) (conn *nats.Conn, close func(), err error)

// Action is the verdict for one manifest entry.
type Action string

const (
	ActionDeleted   Action = "DELETED"
	ActionWould     Action = "WOULD"
	ActionGone      Action = "GONE"
	ActionSkip      Action = "SKIP"
	ActionActive    Action = "ACTIVE"
	ActionFailed    Action = "FAIL"
	ActionConnFail  Action = "CONN"
)

// Event records what happened to a single entry.
type Event struct {
	Cluster  string
	Stream   string
	Consumer string
	Action   Action
	Detail   string
}

// Result is the outcome of an Apply call.
type Result struct {
	Deleted   int
	Skipped   int
	Preserved int
	Gone      int
	Failed    int
	Events    []Event
}

// Summary returns a one-line, log-friendly tally.
func (r *Result) Summary() string {
	return fmt.Sprintf("%d deleted, %d preserved (active), %d already gone, %d skipped, %d failed",
		r.Deleted, r.Preserved, r.Gone, r.Skipped, r.Failed)
}

// Apply groups the manifest entries by cluster, dials each cluster once
// via the Connector, and runs the re-verification + deletion loop.
// DryRun reports actions without sending delete requests.
func Apply(ctx context.Context, m *manifest.Manifest, dryRun bool, connect Connector) (*Result, error) {
	byCluster := map[string][]manifest.Entry{}
	for _, e := range m.Entries {
		byCluster[e.Cluster] = append(byCluster[e.Cluster], e)
	}

	result := &Result{}
	for cluster, entries := range byCluster {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		nc, closeFn, err := connect(cluster)
		if err != nil {
			// Connection failures cover *every* entry on that cluster;
			// record one event per entry so the audit trail is complete.
			for _, e := range entries {
				result.Events = append(result.Events, Event{
					Cluster:  cluster,
					Stream:   e.Stream,
					Consumer: e.Consumer,
					Action:   ActionConnFail,
					Detail:   err.Error(),
				})
				result.Failed++
			}
			continue
		}
		applyOnCluster(nc, cluster, entries, m.GeneratedAt, dryRun, result)
		closeFn()
	}
	return result, nil
}

func applyOnCluster(nc *nats.Conn, cluster string, entries []manifest.Entry, manifestTime time.Time, dryRun bool, r *Result) {
	for _, e := range entries {
		ev := Event{Cluster: cluster, Stream: e.Stream, Consumer: e.Consumer}

		if e.Skip {
			ev.Action = ActionSkip
			ev.Detail = "skip: true in manifest"
			r.Events = append(r.Events, ev)
			r.Skipped++
			continue
		}

		info, gone, err := fetchConsumerInfo(nc, e.Stream, e.Consumer)
		switch {
		case gone:
			ev.Action = ActionGone
			r.Events = append(r.Events, ev)
			r.Gone++
			continue
		case err != nil:
			ev.Action = ActionFailed
			ev.Detail = err.Error()
			r.Events = append(r.Events, ev)
			r.Failed++
			continue
		}

		if reason := isActive(info, manifestTime); reason != "" {
			ev.Action = ActionActive
			ev.Detail = reason
			r.Events = append(r.Events, ev)
			r.Preserved++
			continue
		}

		if dryRun {
			ev.Action = ActionWould
			r.Events = append(r.Events, ev)
			r.Deleted++
			continue
		}

		if err := deleteConsumer(nc, e.Stream, e.Consumer); err != nil {
			ev.Action = ActionFailed
			ev.Detail = "delete: " + err.Error()
			r.Events = append(r.Events, ev)
			r.Failed++
			continue
		}
		ev.Action = ActionDeleted
		r.Events = append(r.Events, ev)
		r.Deleted++
	}
}

// consumerInfo is the subset of $JS.API.CONSUMER.INFO that Apply needs.
type consumerInfo struct {
	Error      *jsAPIError `json:"error,omitempty"`
	PushBound  bool        `json:"push_bound,omitempty"`
	NumWaiting int         `json:"num_waiting"`
	AckFloor   struct {
		LastActive *time.Time `json:"last_active,omitempty"`
	} `json:"ack_floor"`
}

type jsAPIError struct {
	Code        int    `json:"code"`
	ErrCode     uint16 `json:"err_code"`
	Description string `json:"description"`
}

type consumerDeleteResponse struct {
	Success bool        `json:"success"`
	Error   *jsAPIError `json:"error,omitempty"`
}

func fetchConsumerInfo(nc *nats.Conn, stream, consumer string) (*consumerInfo, bool, error) {
	subject := fmt.Sprintf(jsAPIInfoSubject, stream, consumer)
	msg, err := nc.Request(subject, nil, jsAPITimeout)
	if err != nil {
		return nil, false, fmt.Errorf("request: %w", err)
	}
	var info consumerInfo
	if err := json.Unmarshal(msg.Data, &info); err != nil {
		return nil, false, fmt.Errorf("decode: %w", err)
	}
	if info.Error != nil {
		if info.Error.ErrCode == errCodeNotFound {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("nats api %d/%d: %s", info.Error.Code, info.Error.ErrCode, info.Error.Description)
	}
	return &info, false, nil
}

func deleteConsumer(nc *nats.Conn, stream, consumer string) error {
	subject := fmt.Sprintf(jsAPIDeleteSubject, stream, consumer)
	msg, err := nc.Request(subject, nil, jsAPITimeout)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	var resp consumerDeleteResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("nats api %d/%d: %s", resp.Error.Code, resp.Error.ErrCode, resp.Error.Description)
	}
	if !resp.Success {
		return errors.New("delete returned success=false")
	}
	return nil
}

// isActive returns a non-empty reason if the consumer must be preserved.
// Two checks: a binding/pull-waiter exists right now, or the ack floor has
// advanced since the manifest was written (someone consumed in the gap).
func isActive(info *consumerInfo, manifestTime time.Time) string {
	if info.PushBound {
		return "push_bound=true"
	}
	if info.NumWaiting > 0 {
		return fmt.Sprintf("num_waiting=%d", info.NumWaiting)
	}
	if info.AckFloor.LastActive != nil && info.AckFloor.LastActive.After(manifestTime) {
		return "acked since manifest at " + info.AckFloor.LastActive.UTC().Format(time.RFC3339)
	}
	return ""
}
