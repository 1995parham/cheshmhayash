package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/1995parham/natsie/internal/config"
	"github.com/1995parham/natsie/internal/natsctx"
	"github.com/1995parham/natsie/internal/scanner"
)

func scanCommand() *cli.Command {
	return &cli.Command{
		Name:  "scan",
		Usage: "Enumerate and classify consumers in one cluster, optionally checking a peer",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "context",
				Usage:    "NATS context name to scan",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "peer-context",
				Usage: "Sibling cluster context for cross-region peer checks (overrides config)",
			},
			&cli.StringFlag{
				Name:  "stream",
				Usage: "Only scan this stream (default: all streams)",
			},
			&cli.StringFlag{
				Name:  "format",
				Usage: "Output format: tsv | json",
			},
			&cli.IntFlag{
				Name:  "min-pending",
				Usage: "Only report consumers with num_pending above this value",
			},
			&cli.DurationFlag{
				Name:  "min-idle",
				Usage: "Only report consumers idle longer than this",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg, err := config.Load(cmd.Root().String("config"))
			if err != nil {
				return err
			}

			opts := scanner.Options{
				Stream:     cmd.String("stream"),
				MinPending: cfg.Defaults.MinPending,
				MinIdle:    cfg.Defaults.MinIdle,
			}
			if cmd.IsSet("min-pending") {
				opts.MinPending = int64(cmd.Int("min-pending"))
			}
			if cmd.IsSet("min-idle") {
				opts.MinIdle = cmd.Duration("min-idle")
			}

			ctxName := cmd.String("context")
			peerName := cmd.String("peer-context")
			if peerName == "" {
				if c, ok := cfg.Contexts[ctxName]; ok {
					peerName = c.Peer
				}
			}

			format := cmd.String("format")
			if format == "" {
				format = cfg.Defaults.Format
			}
			if format == "" {
				format = "tsv"
			}

			scanCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()

			nc, err := natsctx.Connect(ctxName)
			if err != nil {
				return fmt.Errorf("connect %s: %w", ctxName, err)
			}
			defer nc.Close()

			var peer *natsctx.Conn
			if peerName != "" {
				peer, err = natsctx.Connect(peerName)
				if err != nil {
					return fmt.Errorf("connect peer %s: %w", peerName, err)
				}
				defer peer.Close()
			}

			rows, err := scanner.Scan(scanCtx, nc, peer, opts)
			if err != nil {
				return err
			}

			switch format {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			case "tsv":
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				fmt.Fprintln(w, "stream\tconsumer\tstatus\tpending\tidle\tpeer_status")
				for _, r := range rows {
					fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n",
						r.Stream, r.Consumer, r.Status, r.NumPending, r.Idle.Truncate(time.Second), r.PeerStatus)
				}
				return w.Flush()
			default:
				return fmt.Errorf("unknown --format %q (want tsv|json)", format)
			}
		},
	}
}
