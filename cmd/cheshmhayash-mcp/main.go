// Command cheshmhayash-mcp runs the cheshmhayash MCP server. By default it
// speaks JSON-RPC 2.0 over stdio (for local LLM tool-use, e.g. Claude
// Desktop). With -http it instead serves the MCP Streamable HTTP transport
// at /mcp on its own listener, reusing the same NATS manager, config, and
// auth model as the dashboard binary.
//
// Both transports share mcp.NewServer; write tools are gated by the
// CHESHMHAYASH_MCP_WRITE env var (startup-time), not by identity.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/1995parham/cheshmhayash/internal/auth"
	"github.com/1995parham/cheshmhayash/internal/config"
	"github.com/1995parham/cheshmhayash/internal/handler"
	"github.com/1995parham/cheshmhayash/internal/mcp"
	"github.com/1995parham/cheshmhayash/internal/natsx"
)

const shutdownTimeout = 10 * time.Second

// mcpKeyMatchers converts the config slice into the loose auth.KeyMatcher
// shape — keeps the auth package independent of config.
func mcpKeyMatchers(in []config.MCPKey) []auth.KeyMatcher {
	out := make([]auth.KeyMatcher, 0, len(in))
	for _, k := range in {
		out = append(out, auth.KeyMatcher{Name: k.Name, Value: k.Value})
	}
	return out
}

func main() {
	httpMode := flag.Bool("http", false, "serve the MCP Streamable HTTP transport instead of stdio")
	addr := flag.String("addr", "", "listen address for -http mode (default: server.host:server.port from config)")
	flag.Parse()

	logLevel := slog.LevelInfo
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		_ = logLevel.UnmarshalText([]byte(v))
	}
	// stdout is the JSON-RPC channel in stdio mode — every byte of log noise
	// has to go to stderr instead.
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(log)
	natsx.Logger = log

	var err error
	if *httpMode {
		err = runHTTP(log, *addr)
	} else {
		err = runStdio(log)
	}
	if err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func runStdio(log *slog.Logger) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	settings, err := config.Load()
	if err != nil {
		return err
	}
	mgr, err := natsx.NewManager(ctx, settings.NATS)
	if err != nil {
		return err
	}
	defer mgr.Close()

	write := os.Getenv("CHESHMHAYASH_MCP_WRITE") == "1"
	log.Info("starting mcp server",
		"transport", "stdio",
		"clusters", len(settings.NATS),
		"write_enabled", write,
	)
	srv := mcp.NewServer(mgr, log, write)
	if err := srv.RunStdio(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func runHTTP(log *slog.Logger, addr string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	settings, err := config.Load()
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

	write := os.Getenv("CHESHMHAYASH_MCP_WRITE") == "1"
	mcpServer := mcp.NewServer(mgr, log, write)

	// Optional OIDC. When Auth.Enabled is false this stays nil and /mcp is
	// reachable without a token (unless static MCP keys are configured).
	var authn *auth.Authenticator
	if settings.Auth.Enabled {
		authn, err = auth.New(ctx, settings.Auth, log)
		if err != nil {
			return err
		}
	}
	mcpKeys := mcpKeyMatchers(settings.Auth.MCPKeys)
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

	if addr == "" {
		addr = settings.Server.Addr()
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler.MCPMux(mcpServer, authn, mcpKeys, log),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("starting mcp server",
			"transport", "http",
			"address", addr,
			"path", "/mcp",
			"clusters", len(settings.NATS),
			"write_enabled", write,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("shutdown", "err", err)
	}
	return nil
}
