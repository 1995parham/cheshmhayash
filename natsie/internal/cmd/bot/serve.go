package bot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/carlmjohnson/versioninfo"
	"github.com/urfave/cli/v3"

	"github.com/1995parham/natsie/internal/infra/config"
	"github.com/1995parham/natsie/internal/infra/httpsrv"
	"github.com/1995parham/natsie/internal/infra/natsctx"
	"github.com/1995parham/natsie/internal/infra/notify"
	"github.com/1995parham/natsie/internal/infra/scheduler"
	"github.com/1995parham/natsie/internal/infra/store"
	"github.com/1995parham/natsie/internal/manifest"
	"github.com/1995parham/natsie/internal/scanner"
)

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

	notifiers, err := dialNotifiers(cfg.Bot.Notify)
	if err != nil {
		return err
	}

	logger := log.New(os.Stderr, "natsie ", log.LstdFlags|log.Lmsgprefix)
	sched := scheduler.New(logger)
	for _, s := range cfg.Bot.Schedules {
		job := buildScanJob(s, manifestStore, notifiers, cfg.Bot.HTTP.BaseURL, logger)
		if err := sched.Add(job); err != nil {
			return fmt.Errorf("add schedule %s: %w", s.Name, err)
		}
	}

	sched.Start()
	logger.Printf("bot started: schedules=%d notify=%d store=%s",
		len(cfg.Bot.Schedules), len(notifiers), manifestStore.Name())

	httpErrCh := make(chan error, 1)
	if cfg.Bot.HTTP.Listen != "" {
		server := httpsrv.New(cfg.Bot.HTTP.Listen, manifestStore, logger)
		go func() {
			logger.Printf("http listener: %s", cfg.Bot.HTTP.Listen)
			httpErrCh <- server.Start(ctx)
		}()
	} else {
		logger.Print("http listener: disabled (no bot.http.listen configured)")
	}

	select {
	case <-ctx.Done():
	case err := <-httpErrCh:
		if err != nil {
			logger.Printf("http server stopped early: %v", err)
		}
	}
	logger.Print("shutting down...")
	stopCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	sched.Stop(stopCtx)
	logger.Print("stopped")
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

// buildScanJob captures one schedule's settings and returns the function
// the scheduler will fire on the cron clock.
func buildScanJob(s config.Schedule, manifestStore store.Store, notifiers []notify.Notifier, baseURL string, logger *log.Logger) scheduler.Job {
	return scheduler.Job{
		Name: s.Name,
		Spec: s.Cron,
		Run: func(ctx context.Context) error {
			return runScan(ctx, s, manifestStore, notifiers, baseURL, logger)
		},
	}
}

func runScan(ctx context.Context, s config.Schedule, manifestStore store.Store, notifiers []notify.Notifier, baseURL string, logger *log.Logger) error {
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
		// Nothing to do; don't generate noise. Future: optional "all clear" ping.
		return nil
	}

	id := fmt.Sprintf("%s-%s", s.Name, time.Now().UTC().Format("20060102T150405Z"))
	if err := manifestStore.Put(ctx, id, m); err != nil {
		return fmt.Errorf("store manifest %s: %w", id, err)
	}

	msg := buildMessage(s, m, id, baseURL)
	for _, n := range notifiers {
		if err := n.Post(ctx, msg); err != nil {
			logger.Printf("notify %s: %v", n.Name(), err)
		}
	}
	return nil
}

func buildManifest(s config.Schedule, rows []scanner.Row) *manifest.Manifest {
	m := &manifest.Manifest{
		Version:     manifest.Version,
		GeneratedAt: time.Now().UTC(),
		GeneratedBy: "natsie " + versioninfo.Short(),
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

func buildMessage(s config.Schedule, m *manifest.Manifest, id, baseURL string) notify.Message {
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
	link := ""
	if baseURL != "" {
		link = strings.TrimSuffix(baseURL, "/") + "/manifest/" + id
	}
	return notify.Message{
		Title:      fmt.Sprintf("natsie cleanup candidates (%s)", s.Name),
		Body:       body.String(),
		ManifestID: id,
		Link:       link,
	}
}
