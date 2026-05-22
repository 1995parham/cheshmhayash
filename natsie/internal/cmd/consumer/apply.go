package consumer

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/nats-io/nats.go"
	"github.com/urfave/cli/v3"

	"github.com/1995parham/natsie/internal/cleanup"
	"github.com/1995parham/natsie/internal/infra/natsctx"
	"github.com/1995parham/natsie/internal/manifest"
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

			result, err := cleanup.Apply(ctx, m, cmd.Bool("dry-run"), cliConnector)
			for _, ev := range result.Events {
				logEvent(ev)
			}
			fmt.Fprintf(os.Stderr, "\nsummary: %s\n", result.Summary())
			return err
		},
	}
}

func cliConnector(cluster string) (*nats.Conn, func(), error) {
	nc, err := natsctx.Connect(cluster)
	if err != nil {
		return nil, nil, fmt.Errorf("connect %s: %w", cluster, err)
	}
	return nc.Conn, nc.Close, nil
}

func logEvent(ev cleanup.Event) {
	tag := fmt.Sprintf("%s | %s/%s", ev.Cluster, ev.Stream, ev.Consumer)
	if ev.Detail != "" {
		fmt.Fprintf(os.Stderr, "%-7s %s (%s)\n", ev.Action, tag, ev.Detail)
	} else {
		fmt.Fprintf(os.Stderr, "%-7s %s\n", ev.Action, tag)
	}
}
