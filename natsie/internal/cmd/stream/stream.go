package stream

import "github.com/urfave/cli/v3"

// Command returns the `stream` subcommand group.
func Command() *cli.Command {
	return &cli.Command{
		Name:  "stream",
		Usage: "Operate on JetStream streams",
		Commands: []*cli.Command{
			reportCommand(),
		},
	}
}
