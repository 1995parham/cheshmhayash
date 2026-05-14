// Package notify fans out NATS advisory events (stream/consumer
// create/update/delete/leader-elected, plus system-account events) to
// chat webhooks (Slack, Mattermost, Matrix-via-bridge).
//
// All three providers currently accept the same `{"text": "..."}` JSON
// shape — Matrix support assumes a webhook bridge such as matrix-hookshot
// or maubot/webhook that translates Slack-style payloads. Switching to
// native Matrix Client-Server API is a per-provider extension.
//
// Subscriptions are best-effort: if the connection's account lacks
// permission to read `$JS.EVENT.ADVISORY.>` or the bridged
// `$SYS.ACCOUNT.*.JETSTREAM.EVENT.ADVISORY.>` subjects, the failure is
// logged and the manager keeps running. Operators can grant the relevant
// permissions later without changing config.
package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/1995parham/cheshmhayash/internal/natsx"
)

// ProviderConfig is the shape of a single [[notify]] entry.
type ProviderConfig struct {
	Provider string `koanf:"provider"`
	URL      string `koanf:"url"`
	// Slack/Mattermost — optional channel/username overrides.
	Channel  string `koanf:"channel,omitempty"`
	Username string `koanf:"username,omitempty"`
}

// Manager owns the configured providers and the live subscriptions.
type Manager struct {
	log       *slog.Logger
	providers []Provider
	httpc     *http.Client

	mu   sync.Mutex
	subs []*nats.Subscription
}

// New builds a Manager from the configured provider entries. Returns an
// empty (no-op) manager when cfgs is nil so callers don't need to gate
// every Subscribe call.
func New(log *slog.Logger, cfgs []ProviderConfig) (*Manager, error) {
	m := &Manager{
		log:   log,
		httpc: &http.Client{Timeout: 10 * time.Second},
	}
	for i, c := range cfgs {
		if c.URL == "" {
			return nil, fmt.Errorf("notify[%d]: url is required", i)
		}
		switch c.Provider {
		case "slack", "mattermost", "matrix":
			m.providers = append(m.providers, newWebhook(c, m.httpc))
		case "":
			return nil, fmt.Errorf("notify[%d]: provider is required (slack|mattermost|matrix)", i)
		default:
			return nil, fmt.Errorf("notify[%d]: unknown provider %q", i, c.Provider)
		}
	}
	return m, nil
}

// Enabled reports whether any providers were configured.
func (m *Manager) Enabled() bool { return len(m.providers) > 0 }

// Subscribe wires advisory subscriptions for a single cluster. Idempotent
// per call but the manager won't deduplicate; call once per cluster after
// it connects.
func (m *Manager) Subscribe(cluster *natsx.Cluster) error {
	if !m.Enabled() {
		return nil
	}
	nc := cluster.Conn()
	for _, subj := range advisorySubjects {
		sub, err := nc.Subscribe(subj, m.handlerFor(cluster, subj))
		if err != nil {
			m.log.Warn("notify subscribe failed",
				"cluster", cluster.Name(),
				"subject", subj,
				"err", err,
			)
			continue
		}
		m.mu.Lock()
		m.subs = append(m.subs, sub)
		m.mu.Unlock()
		m.log.Info("notify subscribed", "cluster", cluster.Name(), "subject", subj)
	}
	return nil
}

// Close drains every active subscription. Safe to call multiple times.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.subs {
		_ = s.Unsubscribe()
	}
	m.subs = nil
}

// advisorySubjects covers the broad set NATS publishes. We subscribe to
// both the account-local form (`$JS.EVENT.ADVISORY.>`) and the system-
// account bridged form (`$SYS.ACCOUNT.*.JETSTREAM.EVENT.ADVISORY.>`).
// Whichever the operator's permissions allow will land.
var advisorySubjects = []string{
	"$JS.EVENT.ADVISORY.>",
	"$SYS.ACCOUNT.*.JETSTREAM.EVENT.ADVISORY.>",
}

func (m *Manager) handlerFor(cluster *natsx.Cluster, _ string) nats.MsgHandler {
	clusterName := cluster.Name()
	return func(msg *nats.Msg) {
		ev := classify(msg.Subject, msg.Data)
		if ev == nil {
			// Subjects we subscribed to but don't yet have a formatter for
			// — drop silently to keep noise down.
			return
		}
		notification := Notification{
			Cluster: clusterName,
			Title:   ev.Title,
			Body:    ev.Body,
			Subject: msg.Subject,
		}
		// Fan out concurrently — one slow webhook can't block the others
		// and we don't want to apply back-pressure to NATS.
		for _, p := range m.providers {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := p.Send(ctx, notification); err != nil && !errors.Is(err, context.Canceled) {
					m.log.Warn("notify send failed",
						"cluster", clusterName,
						"provider", p.Name(),
						"err", err,
					)
				}
			}()
		}
	}
}

// Provider is implemented by each webhook flavor.
type Provider interface {
	Name() string
	Send(ctx context.Context, n Notification) error
}

// Notification is the rendered event payload handed to providers.
type Notification struct {
	Cluster string
	Title   string
	Body    string
	Subject string
}
