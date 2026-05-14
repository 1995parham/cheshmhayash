package natsx

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// DefaultOverviewPeriod is how often each cluster's JSZ overview is
// refreshed in the background when the cache is enabled.
const DefaultOverviewPeriod = 10 * time.Second

// OverviewCache keeps the most recent JSZ overview per cluster fresh in
// the background. HTTP handlers read from it instead of hammering NATS on
// every browser tab, and SSE subscribers receive a snapshot on every
// refresh tick.
type OverviewCache struct {
	mgr    *Manager
	log    *slog.Logger
	period time.Duration

	mu      sync.RWMutex
	entries map[string]*OverviewSnapshot

	subMu sync.Mutex
	subs  map[string][]chan OverviewSnapshot
}

// OverviewSnapshot is the rendered overview at one point in time.
// `Data` is the marshalled JSON array `[reply, reply, …]` that callers
// can write straight to the wire. On a refresh error, Data is nil and
// Err carries the description.
type OverviewSnapshot struct {
	Data    json.RawMessage
	Updated time.Time
	Err     string
}

// NewOverviewCache constructs a cache bound to the given Manager. Use
// Start to spawn the background refreshers.
func NewOverviewCache(mgr *Manager, log *slog.Logger, period time.Duration) *OverviewCache {
	if period <= 0 {
		period = DefaultOverviewPeriod
	}
	return &OverviewCache{
		mgr:     mgr,
		log:     log,
		period:  period,
		entries: make(map[string]*OverviewSnapshot),
		subs:    make(map[string][]chan OverviewSnapshot),
	}
}

// Start spawns one refresher goroutine per configured cluster. Refreshers
// exit when ctx is cancelled.
func (oc *OverviewCache) Start(ctx context.Context) {
	for _, name := range oc.mgr.Names() {
		go oc.refresh(ctx, name)
	}
}

func (oc *OverviewCache) refresh(ctx context.Context, name string) {
	c := oc.mgr.Get(name)
	if c == nil {
		return
	}

	tick := func() {
		replies, err := c.JSMOverview()
		snap := OverviewSnapshot{Updated: time.Now()}
		if err != nil {
			snap.Err = err.Error()
			oc.log.Warn("overview refresh failed", "cluster", name, "err", err)
		} else {
			snap.Data = marshalArray(replies)
		}
		oc.store(name, snap)
		oc.broadcast(name, snap)
	}

	// Initial refresh — don't block the caller.
	tick()

	t := time.NewTicker(oc.period)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			oc.closeSubs(name)
			return
		case <-t.C:
			tick()
		}
	}
}

// marshalArray concatenates raw JSON replies into a single JSON array
// without re-parsing each element.
func marshalArray(items []json.RawMessage) json.RawMessage {
	if len(items) == 0 {
		return json.RawMessage("[]")
	}
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, r := range items {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(r)
	}
	buf.WriteByte(']')
	return buf.Bytes()
}

func (oc *OverviewCache) store(name string, snap OverviewSnapshot) {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	oc.entries[name] = &snap
}

// Get returns the latest snapshot for the named cluster. The bool is
// false when no refresh has completed yet (e.g. immediately after Start).
func (oc *OverviewCache) Get(name string) (OverviewSnapshot, bool) {
	oc.mu.RLock()
	defer oc.mu.RUnlock()
	s, ok := oc.entries[name]
	if !ok {
		return OverviewSnapshot{}, false
	}
	return *s, true
}

// Subscribe returns a channel that receives every refresh snapshot for
// the cluster. The current snapshot is delivered immediately if one is
// already cached. Callers MUST call Unsubscribe on close.
//
// The channel is buffered; a slow consumer drops snapshots rather than
// blocking the refresher.
func (oc *OverviewCache) Subscribe(name string) chan OverviewSnapshot {
	ch := make(chan OverviewSnapshot, 4)
	oc.subMu.Lock()
	oc.subs[name] = append(oc.subs[name], ch)
	oc.subMu.Unlock()

	if snap, ok := oc.Get(name); ok {
		select {
		case ch <- snap:
		default:
		}
	}
	return ch
}

// Unsubscribe removes the given channel from the cluster's subscriber
// list and closes it. Safe to call with a channel that's already been
// unsubscribed (no-op).
func (oc *OverviewCache) Unsubscribe(name string, ch chan OverviewSnapshot) {
	oc.subMu.Lock()
	defer oc.subMu.Unlock()
	list := oc.subs[name]
	for i, c := range list {
		if c == ch {
			oc.subs[name] = append(list[:i], list[i+1:]...)
			close(ch)
			return
		}
	}
}

func (oc *OverviewCache) broadcast(name string, snap OverviewSnapshot) {
	// Snapshot the subscriber list under the lock, then send outside so
	// a slow channel doesn't block other subscribers.
	oc.subMu.Lock()
	list := append([]chan OverviewSnapshot(nil), oc.subs[name]...)
	oc.subMu.Unlock()
	for _, ch := range list {
		select {
		case ch <- snap:
		default:
			// slow subscriber — drop this snapshot
		}
	}
}

func (oc *OverviewCache) closeSubs(name string) {
	oc.subMu.Lock()
	defer oc.subMu.Unlock()
	for _, ch := range oc.subs[name] {
		close(ch)
	}
	delete(oc.subs, name)
}
