// Package mcp implements `cheshmhayash mcp`: the MCP server. By default it
// speaks JSON-RPC 2.0 over stdio for local LLM tool-use. With --http it
// serves the MCP Streamable HTTP transport at /mcp instead, with the same
// NATS manager, config and auth model as the dashboard.
//
// Both transports share mcp.NewServer. Write tools are gated by --write
// (CHESHMHAYASH_MCP_WRITE) at start, not by identity.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/urfave/cli/v3"

	"github.com/1995parham/cheshmhayash/internal/auth"
	"github.com/1995parham/cheshmhayash/internal/cliutil"
	"github.com/1995parham/cheshmhayash/internal/config"
	"github.com/1995parham/cheshmhayash/internal/handler"
	"github.com/1995parham/cheshmhayash/internal/httpserver"
	mcpserver "github.com/1995parham/cheshmhayash/internal/mcp"
	"github.com/1995parham/cheshmhayash/internal/natsx"
)

// Command returns the `mcp` command.
func Command() *cli.Command {
	return &cli.Command{
		Name:  "mcp",
		Usage: "run the MCP server (stdio by default, Streamable HTTP with --http)",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "http",
				Usage: "serve the MCP Streamable HTTP transport instead of stdio",
			},
			&cli.StringFlag{
				Name:  "addr",
				Usage: "listen address for --http (default: server.host:server.port from config)",
			},
			&cli.BoolFlag{
				Name:    "write",
				Usage:   "register the destructive tools (purge, delete, kick, reload, stepdown)",
				Sources: cli.EnvVars("CHESHMHAYASH_MCP_WRITE"),
			},
		},
		Action: run,
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	log := slog.Default()

	settings, err := cliutil.LoadSettings(cmd)
	if err != nil {
		return err
	}
	for _, c := range settings.NATS {
		log.Info("connecting", "cluster", c.Name, "url", c.URL)
	}
	mgr, err := natsx.NewManager(ctx, settings.NATS)
	if err != nil {
		return err
	}
	defer mgr.Close()

	write := cmd.Bool("write")
	srv := mcpserver.NewServer(mgr, log, write)

	if !cmd.Bool("http") {
		log.Info("starting mcp server",
			"transport", "stdio",
			"clusters", len(settings.NATS),
			"write_enabled", write,
		)
		if err := srv.RunStdio(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("mcp stdio: %w", err)
		}
		return nil
	}

	return runHTTP(ctx, cmd, log, settings, srv, write)
}

func runHTTP(
	ctx context.Context,
	cmd *cli.Command,
	log *slog.Logger,
	settings *config.Settings,
	srv *mcpserver.Server,
	write bool,
) error {
	// Optional OIDC. When Auth.Enabled is false this stays nil and /mcp is
	// reachable without a token (unless static MCP keys are configured).
	var authn *auth.Authenticator
	if settings.Auth.Enabled {
		var err error
		authn, err = auth.New(ctx, settings.Auth, log)
		if err != nil {
			return fmt.Errorf("auth init: %w", err)
		}
	}
	mcpKeys := keyMatchers(settings.Auth.MCPKeys)
	if len(mcpKeys) > 0 {
		log.Info("mcp http bearer auth enabled", "keys", len(mcpKeys))
	}
	if settings.Auth.MCPOAuth.Enabled {
		log.Info("mcp http oidc auth enabled",
			"issuer", settings.Auth.OIDC.Issuer,
			"resource", settings.Auth.MCPOAuth.Resource,
		)
	}
	if settings.Auth.MCPJWT.Enabled {
		// Deliberately loud: claims on /mcp are trusted without verification.
		log.Warn("mcp http unverified-jwt auth enabled — token claims are NOT verified; a gateway must validate tokens upstream")
	}

	addr := cmd.String("addr")
	if addr == "" {
		addr = settings.Server.Addr()
	}
	httpSrv := httpserver.New(addr, handler.MCPMux(srv, authn, mcpKeys, log))
	log.Info("starting mcp server",
		"transport", "http",
		"address", addr,
		"path", "/mcp",
		"clusters", len(settings.NATS),
		"write_enabled", write,
	)
	return httpserver.Run(ctx, httpSrv, log)
}

// keyMatchers converts the config slice into the loose auth.KeyMatcher
// shape, which keeps the auth package independent of config.
func keyMatchers(in []config.MCPKey) []auth.KeyMatcher {
	out := make([]auth.KeyMatcher, 0, len(in))
	for _, k := range in {
		out = append(out, auth.KeyMatcher{Name: k.Name, Value: k.Value})
	}
	return out
}
