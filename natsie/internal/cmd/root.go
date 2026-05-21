package cmd

import (
	"context"
	"os"

	"github.com/carlmjohnson/versioninfo"
	"github.com/urfave/cli/v3"

	"github.com/1995parham/natsie/internal/cmd/consumer"
)

// Execute is the entry point invoked from main.
func Execute() error {
	root := &cli.Command{
		Name:    "natsie",
		Usage:   "A Swiss-army knife for NATS operations",
		Version: versioninfo.Short(),
		Description: "natsie scans, reports on, and (with explicit human approval) cleans up consumers, " +
			"streams, and cluster state across one or many JetStream clusters.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "path to a YAML config file (default: $NATSIE_CONFIG or ~/.config/natsie/config.yaml)",
				Sources: cli.EnvVars("NATSIE_CONFIG"),
			},
		},
		Commands: []*cli.Command{
			consumer.Command(),
		},
	}
	return root.Run(context.Background(), os.Args)
}
