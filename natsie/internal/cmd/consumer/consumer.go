package consumer

import "github.com/urfave/cli/v3"

// Command returns the `consumer` subcommand group.
func Command() *cli.Command {
	return &cli.Command{
		Name:  "consumer",
		Usage: "Operate on JetStream consumers",
		Commands: []*cli.Command{
			scanCommand(),
			applyCommand(),
		},
	}
}
