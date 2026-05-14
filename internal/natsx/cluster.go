// Package natsx wraps the NATS client with cheshmhayash's request patterns:
// a single-reply request_json for targeted endpoints and a multi-reply
// discover for PING-style fan-out. Connections are long-lived and reused
// across HTTP handlers via Manager.
package natsx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/1995parham/cheshmhayash/internal/config"
)

// Cluster is a live NATS connection plus the operator-configured timeouts
// for this cluster.
type Cluster struct {
	name             string
	nc               *nats.Conn
	requestTimeout   time.Duration
	discoveryTimeout time.Duration
}

func (c *Cluster) Name() string                    { return c.name }
func (c *Cluster) Conn() *nats.Conn                { return c.nc }
func (c *Cluster) RequestTimeout() time.Duration   { return c.requestTimeout }
func (c *Cluster) DiscoveryTimeout() time.Duration { return c.discoveryTimeout }

// Connect dials a NATS server using the supplied config.
func Connect(ctx context.Context, cfg config.NATS) (*Cluster, error) {
	opts := []nats.Option{
		nats.Name("cheshmhayash/" + cfg.Name),
		nats.Timeout(cfg.RequestTimeout()),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			// Surface async errors via the package's slog default. The
			// caller can elevate / filter via the slog handler.
			defaultLogger().Warn("nats async error", "cluster", cfg.Name, "err", err)
		}),
	}
	if cfg.CredsFile != "" {
		opts = append(opts, nats.UserCredentials(cfg.CredsFile))
	}
	if cfg.User != "" && cfg.Password != "" {
		opts = append(opts, nats.UserInfo(cfg.User, cfg.Password))
	}

	// Honor the caller's context for the initial dial via a dial timeout
	// derived from the deadline (if any). nats.go does not yet take a
	// Context for Connect.
	_ = ctx
	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", cfg.URL, err)
	}
	return &Cluster{
		name:             cfg.Name,
		nc:               nc,
		requestTimeout:   cfg.RequestTimeout(),
		discoveryTimeout: cfg.DiscoveryTimeout(),
	}, nil
}

// Close drains the connection if it's still open. Safe to call multiple times.
func (c *Cluster) Close() {
	if c == nil || c.nc == nil {
		return
	}
	_ = c.nc.Drain()
}

// Manager holds the named clusters configured at startup.
type Manager struct {
	clusters map[string]*Cluster
}

func NewManager(ctx context.Context, cfgs []config.NATS) (*Manager, error) {
	m := &Manager{clusters: make(map[string]*Cluster, len(cfgs))}
	for _, cfg := range cfgs {
		c, err := Connect(ctx, cfg)
		if err != nil {
			m.Close()
			return nil, fmt.Errorf("cluster %s: %w", cfg.Name, err)
		}
		m.clusters[cfg.Name] = c
	}
	return m, nil
}

// Get returns the cluster by name, or nil if not configured.
func (m *Manager) Get(name string) *Cluster { return m.clusters[name] }

// Names returns configured cluster names in non-deterministic order.
func (m *Manager) Names() []string {
	out := make([]string, 0, len(m.clusters))
	for n := range m.clusters {
		out = append(out, n)
	}
	return out
}

// Close drains every connection. Concurrent-safe; pkg-init style.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	var wg sync.WaitGroup
	for _, c := range m.clusters {
		wg.Add(1)
		go func(c *Cluster) { defer wg.Done(); c.Close() }(c)
	}
	wg.Wait()
}

// ErrEmptyReply is returned when discover/request gets no replies in the
// allotted window. Callers can choose to surface this differently from
// transport errors.
var ErrEmptyReply = errors.New("no replies within window")

// requestJSON performs a single NATS request and parses the reply as JSON.
// The reply payload is returned as json.RawMessage so handlers can stream
// it back verbatim without re-marshalling.
func (c *Cluster) requestJSON(subject string, payload []byte) (json.RawMessage, error) {
	msg, err := c.nc.Request(subject, payload, c.requestTimeout)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", subject, err)
	}
	if !json.Valid(msg.Data) {
		return nil, fmt.Errorf("request %s: reply not valid JSON", subject)
	}
	return json.RawMessage(msg.Data), nil
}

// discover publishes a request and collects every reply that lands on the
// inbox until the discovery window elapses. Replies that aren't valid JSON
// are dropped with a warn log — matches the Rust behavior.
func (c *Cluster) discover(subject string, payload []byte, window time.Duration) ([]json.RawMessage, error) {
	inbox := c.nc.NewRespInbox()
	sub, err := c.nc.SubscribeSync(inbox)
	if err != nil {
		return nil, fmt.Errorf("subscribe inbox: %w", err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	if err := c.nc.PublishRequest(subject, inbox, payload); err != nil {
		return nil, fmt.Errorf("publish %s: %w", subject, err)
	}
	if err := c.nc.Flush(); err != nil {
		return nil, fmt.Errorf("flush: %w", err)
	}

	deadline := time.Now().Add(window)
	var replies []json.RawMessage
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		msg, err := sub.NextMsg(remaining)
		if err != nil {
			break // timeout or sub closed
		}
		if !json.Valid(msg.Data) {
			defaultLogger().Warn("discarding malformed sys reply", "subject", subject)
			continue
		}
		replies = append(replies, json.RawMessage(msg.Data))
	}
	return replies, nil
}
