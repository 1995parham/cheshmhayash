package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/1995parham/natsie/internal/infra/config"
	"github.com/1995parham/natsie/internal/infra/natsctx"
	"github.com/1995parham/natsie/internal/scanner"
)

func reportCommand() *cli.Command {
	return &cli.Command{
		Name:  "report",
		Usage: "Report per-stream size, retention, replication, and placement",
		UsageText: "natsie stream report --context <ctx> [--stream <name>] [--format tsv|json|pretty]\n\n" +
			"Lists each stream's retention policy and limits, replication factor,\n" +
			"message/byte count, consumer count, and which servers host its replicas.\n" +
			"The pretty format also prints a placement-skew summary: how many streams\n" +
			"each server leads and hosts, to surface lopsided per-pod placement.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "context",
				Usage:    "NATS context name to report on",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "stream",
				Usage: "Only report this stream (default: all streams)",
			},
			&cli.StringFlag{
				Name:  "format",
				Usage: "Output format: tsv | json | pretty",
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

			rows, err := scanner.ReportStreams(scanCtx, nc, cmd.String("stream"))
			if err != nil {
				return err
			}

			return render(format, rows)
		},
	}
}

func render(format string, rows []scanner.StreamReportRow) error {
	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")

		return enc.Encode(rows)
	case "tsv":
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(w, "stream\tretention\tR\tmsgs\tbytes\tmax_age\tmax_bytes\tmax_msgs\tconsumers\tleader\tpeers"); err != nil {
			return err
		}

		for _, r := range rows {
			if _, err := fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%s\t%s\t%s\t%d\t%s\t%s\n",
				r.Stream, r.Retention, r.Replicas, r.Messages, r.Bytes,
				fmtDuration(r.MaxAge), fmtLimit(r.MaxBytes), fmtLimit(r.MaxMsgs),
				r.Consumers, r.Leader, strings.Join(r.Peers, ",")); err != nil {
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

// fmtDuration renders a retention age limit, "-" when unset (unbounded).
func fmtDuration(d time.Duration) string {
	if d <= 0 {
		return "-"
	}

	return d.String()
}

// fmtLimit renders a byte/message limit, "-" when unset (unbounded).
func fmtLimit(n int64) string {
	if n <= 0 {
		return "-"
	}

	return strconv.FormatInt(n, 10)
}
