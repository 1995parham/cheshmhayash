// cheshmhayash is a NATS administration dashboard exposed over HTTP. It
// reuses one long-lived NATS connection per configured cluster and proxies
// requests through `$SYS.REQ.*` and `$JS.API.*` so the same auth model
// natscli uses also applies here.
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

	"github.com/1995parham/cheshmhayash/internal/config"
	"github.com/1995parham/cheshmhayash/internal/handler"
	"github.com/1995parham/cheshmhayash/internal/mcp"
	"github.com/1995parham/cheshmhayash/internal/natsx"
)

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

	srv := &http.Server{
		Addr:              settings.Server.Addr(),
		Handler:           handler.Mux(mgr, frontendDir, log),
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
