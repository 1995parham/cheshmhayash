// Package serve implements `cheshmhayash serve`: the HTTP dashboard. The
// --components flag selects the parts that run in this process, so the
// same image can run the full dashboard, the API alone, or a notifier
// with no HTTP API.
package serve

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/1995parham/cheshmhayash/internal/auth"
	"github.com/1995parham/cheshmhayash/internal/cliutil"
	"github.com/1995parham/cheshmhayash/internal/config"
	"github.com/1995parham/cheshmhayash/internal/handler"
	"github.com/1995parham/cheshmhayash/internal/httpserver"
	"github.com/1995parham/cheshmhayash/internal/natsx"
	"github.com/1995parham/cheshmhayash/internal/notify"
)

// Component is one part of the dashboard that --components can turn on.
type Component string

const (
	// ComponentAPI is the JSON API under /api, with the overview cache.
	ComponentAPI Component = "api"
	// ComponentUI is the built React SPA, served from --static-dir.
	ComponentUI Component = "ui"
	// ComponentNotify forwards JetStream advisories to chat webhooks.
	ComponentNotify Component = "notify"
)

// Components lists every component in the order that help text shows them.
func Components() []Component {
	return []Component{ComponentAPI, ComponentUI, ComponentNotify}
}

// ParseComponents validates names and returns the set of components they
// select. An unknown or empty name is an error, so a typo in a Helm value
// fails at start instead of silently disabling a component.
func ParseComponents(names []string) (map[Component]bool, error) {
	set := make(map[Component]bool, len(names))
	for _, raw := range names {
		// A comma-separated env var arrives as one element on some paths.
		for name := range strings.SplitSeq(raw, ",") {
			c := Component(strings.TrimSpace(name))
			if !slices.Contains(Components(), c) {
				return nil, fmt.Errorf("unknown component %q (known: %s)", name, joinComponents(Components()))
			}
			set[c] = true
		}
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("no component selected (known: %s)", joinComponents(Components()))
	}
	return set, nil
}

func joinComponents(cs []Component) string {
	names := make([]string, 0, len(cs))
	for _, c := range cs {
		names = append(names, string(c))
	}
	return strings.Join(names, ", ")
}

// Command returns the `serve` command.
func Command() *cli.Command {
	return &cli.Command{
		Name:  "serve",
		Usage: "run the HTTP dashboard (API, SPA and webhook notifier)",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:    "components",
				Usage:   "components to run: " + joinComponents(Components()),
				Value:   []string{string(ComponentAPI), string(ComponentUI), string(ComponentNotify)},
				Sources: cli.EnvVars("CHESHMHAYASH_COMPONENTS"),
			},
			&cli.StringFlag{
				Name:  "static-dir",
				Usage: "directory of the built SPA (the ui component)",
				Value: "frontend/dist",
			},
			&cli.DurationFlag{
				Name:    "overview-period",
				Usage:   "refresh interval of the JetStream overview cache",
				Value:   natsx.DefaultOverviewPeriod,
				Sources: cli.EnvVars("CHESHMHAYASH_OVERVIEW_PERIOD"),
			},
		},
		Action: run,
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	log := slog.Default()

	components, err := ParseComponents(cmd.StringSlice("components"))
	if err != nil {
		return fmt.Errorf("--components: %w", err)
	}
	period := cmd.Duration("overview-period")
	if period <= 0 {
		return fmt.Errorf("--overview-period must be positive, got %s", period)
	}

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

	if components[ComponentNotify] {
		notifier, err := startNotifier(log, mgr, settings) //nolint:contextcheck // sends happen in NATS callbacks, each with its own timeout
		if err != nil {
			return err
		}
		defer notifier.Close()
	}

	opts := handler.Options{API: components[ComponentAPI]}
	if components[ComponentUI] {
		opts.StaticDir = cmd.String("static-dir")
	}

	var cache *natsx.OverviewCache
	var authn *auth.Authenticator
	if opts.API {
		authn, err = newAuthenticator(ctx, log, settings.Auth)
		if err != nil {
			return err
		}
		cache = startOverviewCache(ctx, log, mgr, period)
	}

	// The listener always runs, also for a notify-only process, so that
	// Kubernetes probes have /healthz to call.
	srv := httpserver.New(settings.Server.Addr(), handler.Mux(mgr, cache, opts, log, authn))
	log.Info("starting cheshmhayash",
		"address", srv.Addr,
		"clusters", len(settings.NATS),
		"components", selected(components),
	)
	return httpserver.Run(ctx, srv, log)
}

// selected returns the enabled components in their canonical order, for
// the start-up log line.
func selected(set map[Component]bool) []Component {
	return slices.DeleteFunc(Components(), func(c Component) bool { return !set[c] })
}

// startNotifier subscribes the webhook notifier to the JetStream advisory
// subjects on every configured cluster. An empty [[notify]] list gives a
// notifier that does nothing.
func startNotifier(log *slog.Logger, mgr *natsx.Manager, settings *config.Settings) (*notify.Manager, error) {
	notifier, err := notify.New(log, notifyConfigs(settings.Notify))
	if err != nil {
		return nil, fmt.Errorf("notify init: %w", err)
	}
	if !notifier.Enabled() {
		log.Info("notify component on, but no [[notify]] provider is configured")
		return notifier, nil
	}
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
	return notifier, nil
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

// newAuthenticator returns nil when auth is off, so the API behaves as it
// did before the auth feature.
func newAuthenticator(ctx context.Context, log *slog.Logger, cfg config.Auth) (*auth.Authenticator, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	authn, err := auth.New(ctx, cfg, log)
	if err != nil {
		return nil, fmt.Errorf("auth init: %w", err)
	}
	admin := cfg.Access.Admin
	log.Info("auth enabled",
		"mode", cfg.ModeOrDefault(),
		"issuer", cfg.OIDC.Issuer,
		"allowed_emails", len(cfg.Access.AllowedEmails),
		"allowed_domains", len(cfg.Access.AllowedDomains),
		"allowed_groups", len(cfg.Access.AllowedGroups),
		"admin_emails", len(admin.AllowedEmails),
		"admin_domains", len(admin.AllowedDomains),
		"admin_groups", len(admin.AllowedGroups),
		// When the admin allowlist is empty every signed-in user is an
		// admin (legacy behaviour); log it so the tier model is obvious.
		"admin_allowlist_set", !admin.IsEmpty(),
	)
	return authn, nil
}

// startOverviewCache starts the background JSZ cache that
// /api/jsm/.../overview reads and /overview/stream subscribes to.
func startOverviewCache(ctx context.Context, log *slog.Logger, mgr *natsx.Manager, period time.Duration) *natsx.OverviewCache {
	cache := natsx.NewOverviewCache(mgr, log, period)
	cache.Start(ctx)
	log.Info("overview cache started", "period", period)
	return cache
}
