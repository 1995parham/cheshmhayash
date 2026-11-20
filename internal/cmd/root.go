// Package cmd builds the cheshmhayash command tree with urfave/cli v3.
// Each component is a subcommand, so one image can run any of them and a
// deployment turns a component on by the command it runs.
package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"

	"github.com/1995parham/cheshmhayash/internal/cliutil"
	"github.com/1995parham/cheshmhayash/internal/cmd/bot"
	"github.com/1995parham/cheshmhayash/internal/cmd/consumer"
	"github.com/1995parham/cheshmhayash/internal/cmd/mcp"
	"github.com/1995parham/cheshmhayash/internal/cmd/peer"
	"github.com/1995parham/cheshmhayash/internal/cmd/pub"
	"github.com/1995parham/cheshmhayash/internal/cmd/schemacmd"
	"github.com/1995parham/cheshmhayash/internal/cmd/serve"
	"github.com/1995parham/cheshmhayash/internal/cmd/stream"
	"github.com/1995parham/cheshmhayash/internal/cmd/sub"
	"github.com/1995parham/cheshmhayash/internal/version"
)

// Root returns the top-level command with every subcommand registered.
func Root() *cli.Command {
	return &cli.Command{
		Name:    "cheshmhayash",
		Usage:   "a Swiss Army knife for NATS: dashboard, MCP server, protobuf-aware pub/sub, and JetStream operations",
		Version: version.String(),
		Flags: append(cliutil.RootFlags(),
			&cli.StringFlag{
				Name:    "log-level",
				Usage:   "log level: debug, info, warn or error",
				Value:   "info",
				Sources: cli.EnvVars("LOG_LEVEL"),
			},
		),
		Before: setupLogger,
		Commands: []*cli.Command{
			serve.Command(),
			mcp.Command(),
			sub.Command(),
			pub.Command(),
			schemacmd.Command(),
			consumer.Command(),
			peer.Command(),
			stream.Command(),
			bot.Command(),
		},
	}
}

// Execute runs the command tree with the process arguments. SIGINT and
// SIGTERM cancel the context that every subcommand receives.
func Execute() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	err := Root().Run(ctx, os.Args)
	cancel()
	if err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// setupLogger installs the process-wide slog logger. Logs always go to
// stderr, because stdout is the JSON-RPC channel of `mcp` in stdio mode.
func setupLogger(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(cmd.String("log-level"))); err != nil {
		return ctx, fmt.Errorf("--log-level: %w", err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	// natsx falls back to slog.Default, so setting the default is enough.
	slog.SetDefault(log)
	return ctx, nil
}
