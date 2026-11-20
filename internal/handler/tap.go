package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/1995parham/cheshmhayash/internal/auth"
	"github.com/1995parham/cheshmhayash/internal/codec"
	"github.com/1995parham/cheshmhayash/internal/natsx"
	"github.com/1995parham/cheshmhayash/internal/schema"
)

// Tap is the configuration of the live tap. A nil *Tap in Options leaves
// the tap routes unregistered.
type Tap struct {
	// Allow lists the subject patterns a tap may subscribe to; empty
	// allows any subject.
	Allow []string
	// MaxRate is the most messages per second sent to one browser.
	MaxRate int
	// MaxDuration closes a tap after this long.
	MaxDuration time.Duration
	// MaxTaps is the most taps this process serves at a time.
	MaxTaps int
	// ReadOnlyAllowed lets readonly users tap when auth is on; otherwise
	// only admins may.
	ReadOnlyAllowed bool
	// Chains builds the decoder chain for one tap; force is the protobuf
	// type for every payload, or "".
	Chains func(force string) codec.Chain
}

const (
	tapBuffer        = 4096
	tapStatsInterval = 2 * time.Second
	tapHeartbeat     = 15 * time.Second
)

type tapHandler struct {
	mgr  *natsx.Manager
	cfg  *Tap
	log  *slog.Logger
	open atomic.Int64
}

func (t *tapHandler) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tap/config", t.config)
	mux.HandleFunc("GET /api/tap/clusters/{cluster}/stream", t.stream)
}

type tapConfigBody struct {
	Allow              []string `json:"allow"`
	MaxRate            int      `json:"max_rate"`
	MaxDurationSeconds int      `json:"max_duration_seconds"`
	CanTap             bool     `json:"can_tap"`
}

func (t *tapHandler) config(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, tapConfigBody{
		Allow:              append([]string{}, t.cfg.Allow...),
		MaxRate:            t.cfg.MaxRate,
		MaxDurationSeconds: int(t.cfg.MaxDuration.Seconds()),
		CanTap:             t.permitted(r) == nil,
	})
}

// permitted applies the role rule. With auth off there is no identity and
// the dashboard is open, so the tap is too.
func (t *tapHandler) permitted(r *http.Request) error {
	id, ok := auth.FromContext(r.Context())
	if !ok {
		return nil
	}
	if id.Role == auth.RoleAdmin || t.cfg.ReadOnlyAllowed {
		return nil
	}
	return errors.New("the tap needs the admin role")
}

func (t *tapHandler) allowed(subject string) bool {
	return len(t.cfg.Allow) == 0 || slices.ContainsFunc(t.cfg.Allow, func(a string) bool {
		return schema.PatternCovers(a, subject)
	})
}

// tapEvent is one message sent to the browser.
type tapEvent struct {
	Seq     int64           `json:"seq"`
	Time    time.Time       `json:"time"`
	Subject string          `json:"subject"`
	Reply   string          `json:"reply,omitempty"`
	Size    int             `json:"size"`
	Headers codec.Header    `json:"headers,omitempty"`
	Format  codec.Format    `json:"format"`
	Type    string          `json:"type,omitempty"`
	Notes   []string        `json:"notes,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

type tapStats struct {
	Received int64 `json:"received"`
	Sent     int64 `json:"sent"`
	Skipped  int64 `json:"skipped"`
	Dropped  int64 `json:"dropped"`
}

func (t *tapHandler) stream(w http.ResponseWriter, r *http.Request) {
	if err := t.permitted(r); err != nil {
		writeJSON(w, http.StatusForbidden, apiError{Message: err.Error()})
		return
	}
	subject := r.URL.Query().Get("subject")
	if err := schema.ValidatePattern(subject); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: fmt.Sprintf("subject %q: %v", subject, err)})
		return
	}
	if !t.allowed(subject) {
		writeJSON(w, http.StatusForbidden, apiError{Message: fmt.Sprintf("subject %q is not in the tap allow list", subject)})
		return
	}
	c := t.mgr.Get(r.PathValue("cluster"))
	if c == nil {
		writeJSON(w, http.StatusNotFound, apiError{Message: "unknown cluster"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	if n := t.open.Add(1); n > int64(t.cfg.MaxTaps) {
		t.open.Add(-1)
		writeJSON(w, http.StatusTooManyRequests, apiError{Message: "too many taps are open; close one and retry"})
		return
	}
	defer t.open.Add(-1)

	msgs := make(chan *nats.Msg, tapBuffer)
	sub, err := c.Conn().ChanSubscribe(subject, msgs)
	if err != nil {
		upstreamError(w, t.log, err)
		return
	}
	defer func() { _ = sub.Unsubscribe() }()

	who := "anonymous"
	if id, ok := auth.FromContext(r.Context()); ok {
		who = id.Email
	}
	t.log.Info("tap opened", "who", who, "cluster", c.Name(), "subject", subject)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx, cancel := context.WithTimeout(r.Context(), t.cfg.MaxDuration)
	defer cancel()

	var st tapStats
	reason := t.pump(ctx, w, flusher, msgs, sub, t.cfg.Chains(r.URL.Query().Get("type")), &st)
	t.log.Info("tap closed", "who", who, "cluster", c.Name(), "subject", subject, "reason", reason,
		"received", st.Received, "sent", st.Sent, "skipped", st.Skipped, "dropped", st.Dropped)
}

// pump sends messages until the tap ends and returns why it ended.
func (t *tapHandler) pump(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	msgs <-chan *nats.Msg,
	sub *nats.Subscription,
	chain codec.Chain,
	st *tapStats,
) string {
	stats := time.NewTicker(tapStatsInterval)
	defer stats.Stop()
	hb := time.NewTicker(tapHeartbeat)
	defer hb.Stop()

	window := time.Now()
	inWindow := 0
	send := func(event string, v any) bool {
		b, err := json.Marshal(v)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) && send("end", map[string]string{"reason": "max_duration"}) {
				return "max_duration"
			}
			return "client"
		case <-hb.C:
			if _, err := w.Write([]byte(": heartbeat\n\n")); err != nil {
				return "client"
			}
			flusher.Flush()
		case <-stats.C:
			if n, err := sub.Dropped(); err == nil {
				st.Dropped = int64(n)
			}
			if !send("stats", st) {
				return "client"
			}
		case m := <-msgs:
			st.Received++
			if now := time.Now(); now.Sub(window) >= time.Second {
				window, inWindow = now, 0
			}
			if inWindow >= t.cfg.MaxRate {
				st.Skipped++
				continue
			}
			inWindow++
			msg := codec.Message{Subject: m.Subject, Header: codec.Header(m.Header), Data: m.Data}
			d := chain.Decode(msg)
			st.Sent++
			if !send("message", tapEvent{
				Seq: st.Received, Time: time.Now(), Subject: m.Subject, Reply: m.Reply, Size: len(m.Data),
				Headers: msg.Header, Format: d.Format, Type: d.Type, Notes: d.Notes, Payload: d.Body,
			}) {
				return "client"
			}
		}
	}
}
