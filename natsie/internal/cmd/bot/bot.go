// Package bot wires the natsie CLI to the long-lived bot daemon
// (scheduled scans, chat notifications, manifest store, HTTP listener).
package bot

import "github.com/urfave/cli/v3"

// Command returns the `bot` subcommand group.
func Command() *cli.Command {
	return &cli.Command{
		Name:  "bot",
		Usage: "Run natsie as a long-lived bot for scheduled scans and chat integration",
		Commands: []*cli.Command{
			serveCommand(),
		},
	}
}
