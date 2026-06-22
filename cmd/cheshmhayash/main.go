// Command cheshmhayash is a NATS administration dashboard exposed over HTTP.
// It reuses one long-lived NATS connection per configured cluster and proxies
// requests through `$SYS.REQ.*` and `$JS.API.*` so the same auth model
// natscli uses also applies here. The MCP server (stdio and HTTP transports)
// lives in the separate cheshmhayash-mcp binary.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/1995parham/cheshmhayash/internal/auth"
	"github.com/1995parham/cheshmhayash/internal/config"
	"github.com/1995parham/cheshmhayash/internal/handler"
	"github.com/1995parham/cheshmhayash/internal/natsx"
	"github.com/1995parham/cheshmhayash/internal/notify"
)

// overviewPeriod resolves the background JSZ-cache refresh interval.
// Overrideable via CHESHMHAYASH_OVERVIEW_PERIOD as a Go duration string
// (e.g. "5s", "30s"). Defaults to natsx.DefaultOverviewPeriod.
func overviewPeriod() time.Duration {
	if v := os.Getenv("CHESHMHAYASH_OVERVIEW_PERIOD"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return natsx.DefaultOverviewPeriod
}

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
	logLevel := slog.LevelInfo
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		_ = logLevel.UnmarshalText([]byte(v))
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(log)
	natsx.Logger = log

	if err := run(log); err != nil {
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

	// Optional OIDC. When Auth.Enabled is false this stays nil and the API
	// behaves like it did before — backward-compatible default.
	var authn *auth.Authenticator
	if settings.Auth.Enabled {
		authn, err = auth.New(ctx, settings.Auth, log)
		if err != nil {
			return fmt.Errorf("auth init: %w", err)
		}
		admin := settings.Auth.Access.Admin
		log.Info("auth enabled",
			"mode", settings.Auth.ModeOrDefault(),
			"issuer", settings.Auth.OIDC.Issuer,
			"allowed_emails", len(settings.Auth.Access.AllowedEmails),
			"allowed_domains", len(settings.Auth.Access.AllowedDomains),
			"allowed_groups", len(settings.Auth.Access.AllowedGroups),
			"admin_emails", len(admin.AllowedEmails),
			"admin_domains", len(admin.AllowedDomains),
			"admin_groups", len(admin.AllowedGroups),
			// When the admin allowlist is empty every signed-in user is an
			// admin (legacy behaviour); log it so the tier model is obvious.
			"admin_allowlist_set", !admin.IsEmpty(),
		)
	}

	// Background JSZ overview cache. /api/jsm/.../overview reads from
	// here, /api/jsm/.../overview/stream subscribes to refresh ticks.
	overviewCache := natsx.NewOverviewCache(mgr, log, overviewPeriod())
	overviewCache.Start(ctx)
	log.Info("overview cache started", "period", overviewPeriod())

	srv := &http.Server{
		Addr:              settings.Server.Addr(),
		Handler:           handler.Mux(mgr, overviewCache, frontendDir, log, authn),
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
