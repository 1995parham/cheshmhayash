package peer

import "github.com/urfave/cli/v3"

// Command returns the `peer` subcommand group.
func Command() *cli.Command {
	return &cli.Command{
		Name:  "peer",
		Usage: "Inspect JetStream cluster peers",
		Commands: []*cli.Command{
			checkCommand(),
		},
	}
}
