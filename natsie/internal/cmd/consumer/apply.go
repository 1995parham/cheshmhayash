package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/urfave/cli/v3"

	"github.com/1995parham/natsie/internal/infra/natsctx"
	"github.com/1995parham/natsie/internal/manifest"
)

const (
	jsAPIInfoSubject   = "$JS.API.CONSUMER.INFO.%s.%s"
	jsAPIDeleteSubject = "$JS.API.CONSUMER.DELETE.%s.%s"
	jsAPITimeout       = 15 * time.Second
	errCodeNotFound    = 10014
)

func applyCommand() *cli.Command {
	return &cli.Command{
		Name:  "apply",
		Usage: "Apply a cleanup manifest, re-verifying each consumer before deletion",
		UsageText: "natsie consumer apply <manifest.yaml> [--dry-run]\n\n" +
			"Reads the manifest produced by `natsie consumer scan --emit-manifest`,\n" +
			"re-queries each consumer's current state, and deletes those that are\n" +
			"still stale. Consumers that have been active since the manifest was\n" +
			"generated are preserved without modification.",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Re-verify and log decisions, but don't delete anything",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.NArg() != 1 {
				return errors.New("usage: natsie consumer apply <manifest.yaml>")
			}
			m, err := manifest.Read(cmd.Args().First())
			if err != nil {
				return err
			}
			return apply(ctx, m, cmd.Bool("dry-run"))
		},
	}
}

type counters struct{ Deleted, Skipped, Preserved, Gone, Failed int }

// print uses a pointer receiver so `defer totals.print()` reflects the final
// counts, not the zero value captured at defer registration.
func (c *counters) print() {
	fmt.Fprintf(os.Stderr,
		"\nsummary: %d deleted, %d preserved (active), %d already gone, %d skipped, %d failed\n",
		c.Deleted, c.Preserved, c.Gone, c.Skipped, c.Failed)
}

func apply(ctx context.Context, m *manifest.Manifest, dryRun bool) error {
	byCluster := map[string][]manifest.Entry{}
	for _, e := range m.Entries {
		byCluster[e.Cluster] = append(byCluster[e.Cluster], e)
	}

	var totals counters
	defer totals.print()

	for cluster, entries := range byCluster {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		nc, err := natsctx.Connect(cluster)
		if err != nil {
			return fmt.Errorf("connect %s: %w", cluster, err)
		}
		applyOnCluster(nc.Conn, cluster, entries, m.GeneratedAt, dryRun, &totals)
		nc.Close()
	}
	return nil
}

func applyOnCluster(nc *nats.Conn, cluster string, entries []manifest.Entry, manifestTime time.Time, dryRun bool, totals *counters) {
	for _, e := range entries {
		tag := fmt.Sprintf("%s | %s/%s", cluster, e.Stream, e.Consumer)

		if e.Skip {
			fmt.Fprintf(os.Stderr, "SKIP    %s (skip: true in manifest)\n", tag)
			totals.Skipped++
			continue
		}

		info, gone, err := fetchConsumerInfo(nc, e.Stream, e.Consumer)
		switch {
		case gone:
			fmt.Fprintf(os.Stderr, "GONE    %s (already deleted)\n", tag)
			totals.Gone++
			continue
		case err != nil:
			fmt.Fprintf(os.Stderr, "FAIL    %s: %v\n", tag, err)
			totals.Failed++
			continue
		}

		if reason := isActive(info, manifestTime); reason != "" {
			fmt.Fprintf(os.Stderr, "ACTIVE  %s (%s)\n", tag, reason)
			totals.Preserved++
			continue
		}

		if dryRun {
			fmt.Fprintf(os.Stderr, "WOULD   %s\n", tag)
			totals.Deleted++
			continue
		}

		if err := deleteConsumer(nc, e.Stream, e.Consumer); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL    %s: delete: %v\n", tag, err)
			totals.Failed++
			continue
		}
		fmt.Fprintf(os.Stderr, "DELETED %s\n", tag)
		totals.Deleted++
	}
}

// consumerInfo is the subset of $JS.API.CONSUMER.INFO that apply needs.
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
