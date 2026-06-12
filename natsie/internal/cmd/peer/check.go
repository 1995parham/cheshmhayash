package peer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/1995parham/natsie/internal/infra/config"
	"github.com/1995parham/natsie/internal/infra/natsctx"
	"github.com/1995parham/natsie/internal/scanner"
)

func checkCommand() *cli.Command {
	return &cli.Command{
		Name:  "check",
		Usage: "Report cluster peers and flag likely ghosts (offline in every Raft group)",
		UsageText: "natsie peer check --context <ctx> [--format tsv|json|pretty] [--ghosts-only]\n\n" +
			"Walks every stream and consumer Raft group on the cluster and aggregates\n" +
			"each server's standing. A peer that is offline in every group that\n" +
			"references it and leads none is flagged GHOST — a server that was removed\n" +
			"without `peer-remove` and is still pinned into replica sets.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "context",
				Usage:    "NATS context name to inspect",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "format",
				Usage: "Output format: tsv | json | pretty",
			},
			&cli.BoolFlag{
				Name:  "ghosts-only",
				Usage: "Only report peers classified GHOST",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg, err := config.Load(cmd.Root().String("config"))
			if err != nil {
				return err
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

			ctxName := cmd.String("context")

			nc, err := natsctx.Connect(ctxName)
			if err != nil {
				return fmt.Errorf("connect %s: %w", ctxName, err)
			}
			defer nc.Close()

			rows, err := scanner.CheckPeers(scanCtx, nc)
			if err != nil {
				return err
			}

			if cmd.Bool("ghosts-only") {
				rows = filterGhosts(rows)
			}

			return render(format, rows)
		},
	}
}

func filterGhosts(rows []scanner.PeerRow) []scanner.PeerRow {
	return scanner.GhostPeers(rows)
}

func render(format string, rows []scanner.PeerRow) error {
	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")

		return enc.Encode(rows)
	case "tsv":
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(w, "peer\thealth\tgroups\toffline\tcurrent\tleads\tsample"); err != nil {
			return err
		}

		for _, r := range rows {
			if _, err := fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%d\t%s\n",
				r.Peer, r.Health, r.Groups, r.Offline, r.Current, r.Leads, strings.Join(r.Sample, ",")); err != nil {
				return err
			}
		}

		return w.Flush()
	case "pretty":
		renderPretty(os.Stdout, rows)

		return nil
	default:
		return fmt.Errorf("unknown --format %q (want tsv|json|pretty)", format)
	}
}
