package bot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/urfave/cli/v3"

	"github.com/1995parham/natsie/internal/audit"
	"github.com/1995parham/natsie/internal/chatops"
	"github.com/1995parham/natsie/internal/infra/config"
	"github.com/1995parham/natsie/internal/infra/httpsrv"
	"github.com/1995parham/natsie/internal/infra/mattermost"
	"github.com/1995parham/natsie/internal/infra/natsctx"
	"github.com/1995parham/natsie/internal/infra/notify"
	"github.com/1995parham/natsie/internal/infra/scheduler"
	"github.com/1995parham/natsie/internal/infra/store"
	"github.com/1995parham/natsie/internal/manifest"
	"github.com/1995parham/natsie/internal/owners"
	"github.com/1995parham/natsie/internal/scanner"
	"github.com/1995parham/natsie/internal/version"
)

// dispatcher fans manifest notifications out to (a) the configured global
// notify sinks and (b) any owner-specific sinks that match individual
// entries. The master message — the one that carries the signed approve
// URL — always goes to the global sinks; owner sinks receive an
// informational subset of the manifest for awareness.
type dispatcher struct {
	global   []notify.Notifier
	router   *owners.Router
	perOwner map[string][]notify.Notifier
}

// botConnector is the cleanup.Connector for the running bot — same shape
// as the CLI's, but kept here so the bot package owns its NATS dialer.
func botConnector(cluster string) (*nats.Conn, func(), error) {
	nc, err := natsctx.Connect(cluster)
	if err != nil {
		return nil, nil, fmt.Errorf("connect %s: %w", cluster, err)
	}

	return nc.Conn, nc.Close, nil
}

const (
	scanTimeout     = 2 * time.Minute
	shutdownTimeout = 30 * time.Second
)

func serveCommand() *cli.Command {
	return &cli.Command{
		Name:  "serve",
		Usage: "Run scheduled scans and post results to configured notify sinks",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg, err := config.Load(cmd.Root().String("config"))
			if err != nil {
				return err
			}

			return serve(ctx, cfg)
		},
	}
}

func serve(ctx context.Context, cfg *config.Config) error {
	if err := validateBotConfig(&cfg.Bot); err != nil {
		return err
	}

	manifestStore, err := store.Dial(cfg.Bot.Store)
	if err != nil {
		return fmt.Errorf("dial store: %w", err)
	}

	dispatch, err := buildDispatcher(cfg.Bot)
	if err != nil {
		return err
	}

	auditLog, err := audit.NewLogger(cfg.Bot.AuditLog)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer func() { _ = auditLog.Close() }()

	logger := log.New(os.Stderr, "natsie ", log.LstdFlags|log.Lmsgprefix)

	sched := scheduler.New(logger)
	for _, s := range cfg.Bot.Schedules {
		job := buildScanJob(s, manifestStore, dispatch, cfg.Bot.HTTP.BaseURL, cfg.Bot.SigningKey, auditLog, logger)
		if err := sched.Add(job); err != nil { //nolint:contextcheck // job carries its own ctx via Job.Run
			return fmt.Errorf("add schedule %s: %w", s.Name, err)
		}
	}

	sched.Start()
	logger.Printf("bot started: schedules=%d notify=%d owners=%d store=%s",
		len(cfg.Bot.Schedules), len(dispatch.global), len(dispatch.perOwner), manifestStore.Name())

	httpErrCh := make(chan error, 1)

	// Deps the chat command dispatcher needs. Both transports use the
	// same set — HTTP slash and the Mattermost WebSocket listener both
	// route through chatops.Dispatch.
	deps := chatops.Deps{
		Store:      manifestStore,
		Audit:      auditLog,
		BaseURL:    cfg.Bot.HTTP.BaseURL,
		SigningKey: cfg.Bot.SigningKey,
	}

	if cfg.Bot.HTTP.Listen != "" {
		server := httpsrv.New(cfg.Bot.HTTP.Listen, manifestStore,
			httpsrv.Options{
				SigningKey: cfg.Bot.SigningKey,
				Connector:  botConnector,
				Audit:      auditLog,
				BaseURL:    cfg.Bot.HTTP.BaseURL,
			},
			logger,
		)
		go func() {
			logger.Printf("http listener: %s", cfg.Bot.HTTP.Listen)

			httpErrCh <- server.Start(ctx)
		}()
	} else {
		logger.Print("http listener: disabled (no bot.http.listen configured)")
	}

	if cfg.Bot.Mattermost.Enabled {
		// A misconfigured block (missing token, empty server) is still
		// fatal — there's no point pretending to listen when the
		// config is unusable. But a *Mattermost-side* failure (REST
		// unreachable, team not yet joined) is recoverable: the
		// listener logs and retries forever in its own goroutine, and
		// the scheduler + HTTP listener keep running without it.
		if err := startMattermostListener(ctx, cfg.Bot.Mattermost, deps, logger); err != nil {
			return fmt.Errorf("mattermost listener: %w", err)
		}
	}

	select {
	case <-ctx.Done():
	case err := <-httpErrCh:
		if err != nil {
			logger.Printf("http server stopped early: %v", err)
		}
	}

	logger.Print("shutting down...")

	// Use a fresh ctx for shutdown — the parent has already been canceled
	// (that's how we got here) and we still want shutdownTimeout to apply.
	stopCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	sched.Stop(stopCtx) //nolint:contextcheck // shutdown deliberately uses a fresh deadline, see comment above
	logger.Print("stopped")

	return nil
}

// startMattermostListener resolves the bot token (file > inline),
// constructs the Listener, and launches its Run goroutine. Only
// configuration errors (missing/unreadable token, missing required
// field) are fatal — Listener.Run handles transient Mattermost
// failures itself with retry + backoff, so a Mattermost outage at
// pod start no longer crashes the scheduler.
func startMattermostListener(ctx context.Context, cfg config.Mattermost, deps chatops.Deps, logger *log.Logger) error {
	token := cfg.Token
	if cfg.TokenFile != "" {
		raw, err := os.ReadFile(cfg.TokenFile)
		if err != nil {
			return fmt.Errorf("read token_file: %w", err)
		}

		token = strings.TrimSpace(string(raw))
	}

	listener, err := mattermost.New(mattermost.Config{
		Server:  cfg.Server,
		Token:   token,
		Team:    cfg.Team,
		Channel: cfg.Channel,
		Trigger: cfg.Trigger,
	}, deps, logger)
	if err != nil {
		return err
	}

	go func() {
		_ = listener.Run(ctx)
	}()

	return nil
}

func validateBotConfig(b *config.Bot) error {
	if len(b.Schedules) == 0 {
		return errors.New("bot.schedules is empty — define at least one schedule")
	}

	if b.Store == "" {
		return errors.New("bot.store is required (e.g. file:///var/lib/natsie/manifests)")
	}

	if len(b.Notify) == 0 {
		return errors.New("bot.notify is empty — add at least one URL (use stdout:// for testing)")
	}

	for i, s := range b.Schedules {
		if s.Name == "" || s.Cron == "" || s.Context == "" {
			return fmt.Errorf("bot.schedules[%d]: name, cron, and context are required", i)
		}
	}

	return nil
}

func dialNotifiers(urls []string) ([]notify.Notifier, error) {
	out := make([]notify.Notifier, 0, len(urls))
	for _, u := range urls {
		n, err := notify.Dial(u)
		if err != nil {
			return nil, fmt.Errorf("notify url %q: %w", u, err)
		}

		out = append(out, n)
	}

	return out, nil
}

// buildDispatcher wires the global notify list, the owner router, and a
// per-owner notifier map. Owner notify lists are validated and dialed up
// front so a typo crashes serve at startup, not at the first scan.
func buildDispatcher(b config.Bot) (*dispatcher, error) {
	global, err := dialNotifiers(b.Notify)
	if err != nil {
		return nil, err
	}

	router, err := owners.NewRouter(b.Owners)
	if err != nil {
		return nil, fmt.Errorf("owners: %w", err)
	}

	perOwner := map[string][]notify.Notifier{}

	for _, o := range b.Owners {
		ns, err := dialNotifiers(o.Notify)
		if err != nil {
			return nil, fmt.Errorf("owner %q notify: %w", o.Name, err)
		}

		perOwner[o.Name] = ns
	}

	return &dispatcher{global: global, router: router, perOwner: perOwner}, nil
}

// buildScanJob captures one schedule's settings and returns the function
// the scheduler will fire on the cron clock.
func buildScanJob(s config.Schedule, manifestStore store.Store, dispatch *dispatcher, baseURL, signingKey string, auditLog *audit.Logger, logger *log.Logger) scheduler.Job {
	return scheduler.Job{
		Name: s.Name,
		Spec: s.Cron,
		Run: func(ctx context.Context) error {
			return runScan(ctx, s, manifestStore, dispatch, baseURL, signingKey, auditLog, logger)
		},
	}
}

func runScan(ctx context.Context, s config.Schedule, manifestStore store.Store, dispatch *dispatcher, baseURL, signingKey string, auditLog *audit.Logger, logger *log.Logger) error {
	scanCtx, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()

	nc, err := natsctx.Connect(s.Context)
	if err != nil {
		return fmt.Errorf("connect %s: %w", s.Context, err)
	}
	defer nc.Close()

	var peer *natsctx.Conn
	if s.PeerContext != "" {
		peer, err = natsctx.Connect(s.PeerContext)
		if err != nil {
			return fmt.Errorf("connect peer %s: %w", s.PeerContext, err)
		}
		defer peer.Close()
	}

	opts := scanner.Options{
		Stream:     s.Stream,
		MinPending: s.MinPending,
		MinIdle:    s.MinIdle,
	}

	rows, err := scanner.Scan(scanCtx, nc, peer, opts)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	m := buildManifest(s, rows)
	logger.Printf("schedule=%s entries=%d", s.Name, len(m.Entries))

	if len(m.Entries) == 0 {
		_ = auditLog.Log(audit.Event{Kind: "scan", Schedule: s.Name, Entries: 0})
		// Nothing to do; don't generate noise. Future: optional "all clear" ping.
		return nil
	}

	id := fmt.Sprintf("%s-%s", s.Name, time.Now().UTC().Format("20060102T150405Z"))
	if err := manifestStore.Put(ctx, id, m); err != nil {
		return fmt.Errorf("store manifest %s: %w", id, err)
	}

	_ = auditLog.Log(audit.Event{Kind: "scan", Schedule: s.Name, Manifest: id, Entries: len(m.Entries)})

	dispatch.post(ctx, s, m, id, baseURL, signingKey, logger)

	return nil
}

// post fans the manifest out: per-owner subset messages to each owner's
// notify list (no approve URL — owners get visibility, not approval),
// and the full manifest with the signed approve URL to the global list.
func (d *dispatcher) post(ctx context.Context, s config.Schedule, m *manifest.Manifest, id, baseURL, signingKey string, logger *log.Logger) {
	if len(d.perOwner) > 0 {
		grouped := d.router.Group(m.Entries)
		for ownerName, entries := range grouped {
			sinks := d.perOwner[ownerName]
			if len(sinks) == 0 || len(entries) == 0 {
				continue
			}

			msg := buildOwnerMessage(s, ownerName, entries)
			for _, n := range sinks {
				if err := n.Post(ctx, msg); err != nil {
					logger.Printf("notify owner=%s sink=%s: %v", ownerName, n.Name(), err)
				}
			}
		}
	}

	master := buildMessage(s, m, id, baseURL, signingKey)
	for _, n := range d.global {
		if err := n.Post(ctx, master); err != nil {
			logger.Printf("notify global sink=%s: %v", n.Name(), err)
		}
	}
}

func buildManifest(s config.Schedule, rows []scanner.Row) *manifest.Manifest {
	m := &manifest.Manifest{
		Version:     manifest.Version,
		GeneratedAt: time.Now().UTC(),
		GeneratedBy: "natsie " + version.Short(),
		Scan: manifest.ScanInfo{
			Context:     s.Context,
			PeerContext: s.PeerContext,
			Stream:      s.Stream,
			MinPending:  s.MinPending,
			MinIdle:     s.MinIdle,
		},
	}

	for _, r := range rows {
		if r.Status != scanner.StatusStale {
			continue
		}

		m.Entries = append(m.Entries, manifest.Entry{
			Cluster:    r.Cluster,
			Stream:     r.Stream,
			Consumer:   r.Consumer,
			Status:     string(r.Status),
			PeerStatus: string(r.PeerStatus),
			NumPending: r.NumPending,
			Idle:       r.Idle.Truncate(time.Second),
			LastAck:    r.LastAck.UTC(),
		})
	}

	return m
}

func buildMessage(s config.Schedule, m *manifest.Manifest, id, baseURL, signingKey string) notify.Message {
	var body strings.Builder
	fmt.Fprintf(&body, "Schedule **%s** found %d stale consumer", s.Name, len(m.Entries))

	if len(m.Entries) != 1 {
		body.WriteString("s")
	}

	body.WriteString(":\n")

	for i, e := range m.Entries {
		if i >= 10 {
			fmt.Fprintf(&body, "...and %d more\n", len(m.Entries)-10)

			break
		}

		fmt.Fprintf(&body, "- `%s/%s` (pending=%d, idle=%s)\n", e.Stream, e.Consumer, e.NumPending, e.Idle)
	}

	base := strings.TrimSuffix(baseURL, "/")

	link := ""
	if base != "" {
		link = base + "/manifest/" + id
	}

	if base != "" && signingKey != "" {
		token := httpsrv.SignApprovalToken(signingKey, id)
		fmt.Fprintf(&body, "\nApprove: %s/approve/%s?token=%s\n", base, id, token)
	}

	return notify.Message{
		Title:      fmt.Sprintf("natsie cleanup candidates (%s)", s.Name),
		Body:       body.String(),
		ManifestID: id,
		Link:       link,
	}
}

// buildOwnerMessage renders the owner-scoped subset of a manifest. No
// approve URL: owners get visibility so they can flag entries that
// shouldn't be deleted; the operator with the global notification holds
// the actual approval URL.
func buildOwnerMessage(s config.Schedule, ownerName string, entries []manifest.Entry) notify.Message {
	var body strings.Builder
	fmt.Fprintf(&body, "Schedule **%s** flagged %d consumer", s.Name, len(entries))

	if len(entries) != 1 {
		body.WriteString("s")
	}

	fmt.Fprintf(&body, " owned by **%s**:\n", ownerName)

	for i, e := range entries {
		if i >= 10 {
			fmt.Fprintf(&body, "...and %d more\n", len(entries)-10)

			break
		}

		fmt.Fprintf(&body, "- `%s/%s` (pending=%d, idle=%s)\n", e.Stream, e.Consumer, e.NumPending, e.Idle)
	}

	body.WriteString("\n_Approval happens centrally — flag any of these to your operator if they should be preserved._")

	return notify.Message{
		Title: fmt.Sprintf("natsie cleanup candidates for %s (%s)", ownerName, s.Name),
		Body:  body.String(),
	}
}
