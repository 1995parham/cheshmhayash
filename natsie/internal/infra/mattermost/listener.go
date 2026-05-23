// Package mattermost implements pull-mode bot transport for Mattermost
// instances that don't support outbound slash commands.
//
// The bot opens an outbound WebSocket to the Mattermost server using a
// bot-account token, filters posted events to a configured channel,
// matches the trigger prefix, and replies via the REST API. No inbound
// HTTP route on the bot side is needed — useful when natsie sits behind
// a private ingress that chat.example.com can't reach.
package mattermost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/1995parham/natsie/internal/chatops"
)

// Config is the listener's configuration block. Server is the Mattermost
// HTTPS endpoint (no trailing slash). Token is a bot-account personal
// access token. Team is the team slug the channel lives on. Channel is
// the channel name (without leading #). Trigger is the bang-prefix the
// listener reacts to, e.g. "!natsie".
type Config struct {
	Server  string
	Token   string
	Team    string
	Channel string
	Trigger string
}

// Validate returns nil iff every required field is set. Called from
// serve.go before the listener goroutine starts so a typo crashes the
// process at boot, not on the first event.
func (c Config) Validate() error {
	if c.Server == "" {
		return errors.New("mattermost.server is required (e.g. https://chat.example.com)")
	}

	if c.Token == "" {
		return errors.New("mattermost.token is required (set via env or token_file)")
	}

	if c.Team == "" {
		return errors.New("mattermost.team is required")
	}

	if c.Channel == "" {
		return errors.New("mattermost.channel is required")
	}

	if c.Trigger == "" {
		return errors.New("mattermost.trigger is required (e.g. !natsie)")
	}

	return nil
}

// Listener owns the long-lived bot connection. New does NO I/O — every
// REST and WebSocket call lives inside Run, so a Mattermost outage at
// process startup logs + backs off instead of crashing the pod. The
// scheduler and HTTP listener keep running independently.
type Listener struct {
	cfg  Config
	deps chatops.Deps
	log  *log.Logger
}

// session holds the per-WebSocket-life state: the REST client + the
// channel / bot ids resolved at the start of this session. A new
// session is created on every reconnect because IDs *could* change
// (channel renamed, bot user re-created) and re-resolving them is
// nearly free compared to a WebSocket lifetime.
type session struct {
	rest      *model.Client4
	teamID    string
	channelID string
	botUserID string
}

const (
	// reconnect backoff bounds — Mattermost's docs recommend ≥1s, and the
	// upper bound keeps us from sleeping the whole night through if the
	// server is in a long outage.
	backoffMin = 2 * time.Second
	backoffMax = 60 * time.Second
)

// New constructs a Listener. Pure: validates config, allocates nothing
// network-bound. The actual REST handshake runs inside Run so a
// Mattermost API failure at boot is recoverable.
func New(cfg Config, deps chatops.Deps, logger *log.Logger) (*Listener, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &Listener{cfg: cfg, deps: deps, log: logger}, nil
}

// Run blocks until ctx is canceled, reconnecting on every REST or
// WebSocket failure with exponential backoff. Each iteration is one
// "session": resolve IDs, open WS, pump events until the WS dies.
//
// Run never returns a non-nil error — failures are logged inline and
// retried forever. Returning nil on ctx.Done() lets callers drop the
// goroutine cleanly during shutdown.
func (l *Listener) Run(ctx context.Context) error {
	backoff := backoffMin

	for {
		if ctx.Err() != nil {
			return nil //nolint:nilerr // ctx cancellation is normal shutdown, not a Run error
		}

		err := l.session(ctx)
		switch {
		case err == nil:
			// Clean shutdown via ctx — exit immediately, no backoff.
			return nil
		case ctx.Err() != nil:
			return nil //nolint:nilerr // shutdown wins over a stale session error
		default:
			l.log.Printf("mattermost: session ended: %v (reconnecting in %s)", err, backoff)
		}

		select {
		case <-ctx.Done():
			return nil //nolint:nilerr // shutdown wins over the session error we just logged
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > backoffMax {
			backoff = backoffMax
		}
	}
}

// session is one REST handshake + one WebSocket lifetime. Errors from
// either phase bubble up to Run for backoff + retry. Returns nil only
// when ctx was canceled mid-session.
func (l *Listener) session(ctx context.Context) error {
	s, err := l.handshake(ctx)
	if err != nil {
		return err
	}

	wsURL := websocketURL(l.cfg.Server)

	wsc, err := model.NewWebSocketClient4(wsURL, l.cfg.Token)
	if err != nil {
		return fmt.Errorf("new ws client: %w", err)
	}

	wsc.Listen()
	defer wsc.Close()

	l.log.Printf("mattermost: websocket listening on %s", wsURL)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-wsc.PingTimeoutChannel:
			return errors.New("ping timeout")
		case ev, ok := <-wsc.EventChannel:
			if !ok {
				return errors.New("event channel closed")
			}

			l.handleEvent(ctx, s, ev)
		}
	}
}

// handshake resolves bot user / team / channel via REST. Called at the
// start of every session so an ID change (bot recreated, channel
// renamed) is picked up on the next reconnect without a pod restart.
func (l *Listener) handshake(ctx context.Context) (*session, error) {
	rest := model.NewAPIv4Client(l.cfg.Server)
	rest.SetToken(l.cfg.Token)

	me, _, err := rest.GetMe(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("get_me: %w", err)
	}

	team, _, err := rest.GetTeamByName(ctx, l.cfg.Team, "")
	if err != nil {
		return nil, fmt.Errorf("get_team_by_name(%s): %w", l.cfg.Team, err)
	}

	channel, _, err := rest.GetChannelByName(ctx, l.cfg.Channel, team.Id, "")
	if err != nil {
		return nil, fmt.Errorf("get_channel_by_name(%s/%s): %w", l.cfg.Team, l.cfg.Channel, err)
	}

	l.log.Printf("mattermost: connected as %s (user_id=%s) team=%s channel=%s",
		me.Username, me.Id, team.Name, channel.Name)

	return &session{
		rest:      rest,
		teamID:    team.Id,
		channelID: channel.Id,
		botUserID: me.Id,
	}, nil
}

// websocketURL rewrites https:// → wss:// (or http → ws) for the
// WebSocket dial. Mattermost mounts the WS endpoint at /api/v4/websocket;
// NewWebSocketClient4 appends that path itself.
func websocketURL(serverURL string) string {
	switch {
	case strings.HasPrefix(serverURL, "https://"):
		return "wss://" + strings.TrimPrefix(serverURL, "https://")
	case strings.HasPrefix(serverURL, "http://"):
		return "ws://" + strings.TrimPrefix(serverURL, "http://")
	default:
		return serverURL
	}
}

// handleEvent is the per-event hot path. It must be cheap on the common
// case (event we don't care about) so the WebSocket reader doesn't fall
// behind.
func (l *Listener) handleEvent(ctx context.Context, s *session, ev *model.WebSocketEvent) {
	if ev.EventType() != model.WebsocketEventPosted {
		return
	}

	postJSON, ok := ev.GetData()["post"].(string)
	if !ok || postJSON == "" {
		return
	}

	var post model.Post
	if err := json.Unmarshal([]byte(postJSON), &post); err != nil {
		return
	}

	// Channel scope and self-skip: avoid feedback loops where the bot
	// reacts to its own replies, and avoid leaking responses into
	// unrelated channels.
	if post.ChannelId != s.channelID {
		return
	}

	if post.UserId == s.botUserID {
		return
	}

	argv, ok := matchTrigger(l.cfg.Trigger, post.Message)
	if !ok {
		return
	}

	reply := chatops.Dispatch(ctx, l.deps, l.cfg.Trigger, argv)
	if reply == "" {
		return
	}

	l.reply(ctx, s, post.Id, reply)
}

// matchTrigger returns the argv after the trigger prefix, or ok=false
// if the message doesn't start with the trigger. Whitespace-tolerant.
func matchTrigger(trigger, message string) ([]string, bool) {
	msg := strings.TrimSpace(message)
	if !strings.HasPrefix(msg, trigger) {
		return nil, false
	}

	rest := strings.TrimSpace(strings.TrimPrefix(msg, trigger))

	return strings.Fields(rest), true
}

// reply posts the bot's response as a threaded reply on the source post.
// Errors are logged but don't crash the loop — a single failed reply
// shouldn't kill the WebSocket.
func (l *Listener) reply(ctx context.Context, s *session, rootID, text string) {
	out := &model.Post{
		ChannelId: s.channelID,
		Message:   text,
		RootId:    rootID,
	}

	if _, _, err := s.rest.CreatePost(ctx, out); err != nil {
		l.log.Printf("mattermost: reply failed: %v", err)
	}
}
