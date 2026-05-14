// cheshmhayash is a NATS administration dashboard exposed over HTTP. It
// reuses one long-lived NATS connection per configured cluster and proxies
// requests through `$SYS.REQ.*` and `$JS.API.*` so the same auth model
// natscli uses also applies here.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/1995parham/cheshmhayash/internal/config"
	"github.com/1995parham/cheshmhayash/internal/handler"
	"github.com/1995parham/cheshmhayash/internal/mcp"
	"github.com/1995parham/cheshmhayash/internal/natsx"
	"github.com/1995parham/cheshmhayash/internal/notify"
)

// notifyConfigs converts the typed config.Notify slice into the loose
// notify.ProviderConfig shape (decoupled to keep notify free of a config
// import cycle).
func notifyConfigs(in []config.Notify) []notify.ProviderConfig {
	out := make([]notify.ProviderConfig, 0, len(in))
	for _, n := range in {
		out = append(out, notify.ProviderConfig{
			Provider: n.Provider,
			URL:      n.URL,
			Channel:  n.Channel,
			Username: n.Username,
		})
	}
	return out
}

const (
	frontendDir     = "frontend/dist"
	shutdownTimeout = 10 * time.Second
)

func main() {
	mcpMode := flag.Bool("mcp", false, "run as a stdio MCP server instead of the HTTP API")
	flag.Parse()

	logLevel := slog.LevelInfo
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		_ = logLevel.UnmarshalText([]byte(v))
	}
	// In MCP mode, stdout is the JSON-RPC channel — every byte of log noise
	// has to go to stderr instead.
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(log)
	natsx.Logger = log

	var err error
	if *mcpMode {
		err = runMCP(log)
	} else {
		err = run(log)
	}
	if err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
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

	// Webhook notifier — subscribes to JS advisory subjects on every
	// configured cluster and fans events out to the providers in
	// settings.Notify. Empty list means "no notifications".
	notifier, err := notify.New(log, notifyConfigs(settings.Notify))
	if err != nil {
		return fmt.Errorf("notify init: %w", err)
	}
	if notifier.Enabled() {
		for _, c := range settings.NATS {
			cluster := mgr.Get(c.Name)
			if cluster == nil {
				continue
			}
			if err := notifier.Subscribe(cluster); err != nil {
				log.Warn("notify subscribe failed", "cluster", c.Name, "err", err)
			}
		}
		log.Info("notify enabled", "providers", len(settings.Notify))
	}
	defer notifier.Close()

	// Expose MCP over HTTP at /mcp using the same NATS manager. Write mode
	// is opt-in via env so the HTTP endpoint defaults to read-only — the
	// surface is currently un-authenticated, matching the rest of the API.
	mcpWrite := os.Getenv("CHESHMHAYASH_MCP_WRITE") == "1"
	mcpServer := mcp.NewServer(mgr, log, mcpWrite)
	log.Info("mcp http transport enabled", "path", "/mcp", "write_enabled", mcpWrite)

	srv := &http.Server{
		Addr:              settings.Server.Addr(),
		Handler:           handler.Mux(mgr, frontendDir, log, mcpServer),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("starting cheshmhayash",
			"address", srv.Addr,
			"clusters", len(settings.NATS),
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

func runMCP(log *slog.Logger) error {
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
		"clusters", len(settings.NATS),
		"write_enabled", write,
	)
	srv := mcp.NewServer(mgr, log, write)
	if err := srv.RunStdio(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
