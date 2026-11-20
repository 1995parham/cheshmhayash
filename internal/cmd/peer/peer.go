// Package peer implements `cheshmhayash peer check`: find ghost peers that
// are offline in every Raft group and lead none.
package peer

import "github.com/urfave/cli/v3"

// Command returns the `peer` subcommand group.
func Command() *cli.Command {
	return &cli.Command{
		Name:  "peer",
		Usage: "inspect JetStream cluster peers",
		Commands: []*cli.Command{
			checkCommand(),
		},
	}
}
