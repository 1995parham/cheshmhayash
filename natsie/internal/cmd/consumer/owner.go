package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/urfave/cli/v3"

	"github.com/1995parham/natsie/internal/infra/config"
	"github.com/1995parham/natsie/internal/infra/natsctx"
	"github.com/1995parham/natsie/internal/scanner"
)

func ownerCommand() *cli.Command {
	return &cli.Command{
		Name:  "owner",
		Usage: "Resolve which cluster/consumer currently owns a filter subject across contexts",
		UsageText: "natsie consumer owner --subject <filter_subject> [--context <ctx> ...] [--format tsv|json|pretty]\n\n" +
			"Searches every configured NATS context (or just the --context ones) for\n" +
			"consumers filtering the given subject, classifies each active/stale, and\n" +
			"reports them active-first. Use it to answer \"consumer X is stale here, but\n" +
			"who actually owns its subject now?\" — the owner may have moved clusters or\n" +
			"been renamed. Pass the filter_subject exactly as `consumer scan` reports it.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "subject",
				Usage:    "Filter subject to resolve ownership for",
				Required: true,
			},
			&cli.StringSliceFlag{
				Name:  "context",
				Usage: "Limit the search to these contexts (repeatable; default: all configured contexts)",
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

			names, err := contextNames(cmd.StringSlice("context"))
			if err != nil {
				return err
			}

			if len(names) == 0 {
				return errors.New("no NATS contexts found (install one with `nats context add` or pass --context)")
			}

			scanCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()

			conns, unreachable := connectAll(names)
			for _, nc := range conns {
				defer nc.Close()
			}

			for _, u := range unreachable {
				fmt.Fprintf(os.Stderr, "warning: skipping unreachable context %q\n", u)
			}

			opts := scanner.Options{MinIdle: cfg.Defaults.MinIdle}

			matches, err := scanner.ResolveOwner(scanCtx, conns, cmd.String("subject"), opts)
			if err != nil {
				return err
			}

			return renderOwner(format, cmd.String("subject"), matches)
		},
	}
}

// contextNames returns the explicit list when provided, otherwise every
// configured context name.
func contextNames(explicit []string) ([]string, error) {
	if len(explicit) > 0 {
		return explicit, nil
	}

	infos, err := natsctx.List()
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(infos))
	for _, i := range infos {
		names = append(names, i.Name)
	}

	return names, nil
}

// connectAll dials each context, returning the live connections and the names
// it could not reach (so the resolution can proceed on a best-effort basis).
func connectAll(names []string) ([]*natsctx.Conn, []string) {
	var (
		conns       []*natsctx.Conn
		unreachable []string
	)

	for _, name := range names {
		nc, err := natsctx.Connect(name)
		if err != nil {
			unreachable = append(unreachable, name)

			continue
		}

		conns = append(conns, nc)
	}

	return conns, unreachable
}

func renderOwner(format, subject string, matches []scanner.OwnerMatch) error {
	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")

		return enc.Encode(matches)
	case "tsv":
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(w, "cluster\tstream\tconsumer\tstatus\tpending\tidle"); err != nil {
			return err
		}

		for _, m := range matches {
			if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n",
				m.Cluster, m.Stream, m.Consumer, m.Status, m.NumPending, m.Idle.Truncate(time.Second)); err != nil {
				return err
			}
		}

		return w.Flush()
	case "pretty":
		renderOwnerPretty(subject, matches)

		return nil
	default:
		return fmt.Errorf("unknown --format %q (want tsv|json|pretty)", format)
	}
}

func renderOwnerPretty(subject string, matches []scanner.OwnerMatch) {
	if owner, ok := scanner.Owner(matches); ok {
		_, _ = fmt.Fprintf(os.Stdout, "owner of %q: consumer %s on %s\n\n",
			subject, owner.Consumer, text.Colors{text.FgGreen}.Sprint(owner.Cluster))
	} else {
		_, _ = fmt.Fprintf(os.Stdout, "no active owner found for %q (%d stale match(es))\n\n", subject, len(matches))
	}

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleRounded)
	t.Style().Color.Header = text.Colors{text.Bold}
	t.AppendHeader(table.Row{"cluster", "stream", "consumer", "status", "pending", "idle"})

	for _, m := range matches {
		t.AppendRow(table.Row{
			m.Cluster,
			m.Stream,
			m.Consumer,
			colorize(m.Status),
			m.NumPending,
			m.Idle.Truncate(time.Second),
		})
	}

	t.Render()
}
