// Package consumer implements `cheshmhayash consumer`: scan consumers for
// stale ones, apply a reviewed cleanup manifest, and find which consumer owns
// a filter subject across clusters.
package consumer

import "github.com/urfave/cli/v3"

// Command returns the `consumer` subcommand group.
func Command() *cli.Command {
	return &cli.Command{
		Name:  "consumer",
		Usage: "operate on JetStream consumers",
		Commands: []*cli.Command{
			scanCommand(),
			applyCommand(),
			ownerCommand(),
		},
	}
}
