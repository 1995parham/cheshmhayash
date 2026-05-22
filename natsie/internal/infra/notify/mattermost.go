package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"time"
)

const (
	defaultMattermostUser = "natsie"
	defaultHTTPTimeout    = 15 * time.Second
)

// Mattermost posts to an incoming webhook. URL form:
//
//	mattermost://host/hooks/<token>[?channel=foo&username=bar]
//
// The scheme is rewritten to https when issuing the POST.
type Mattermost struct {
	WebhookURL string
	Channel    string
	Username   string
	Client     *http.Client
}

// NewMattermost parses a mattermost:// URL into a notifier.
func NewMattermost(u *neturl.URL) (*Mattermost, error) {
	if u.Host == "" {
		return nil, fmt.Errorf("mattermost url missing host: %s", u.Redacted())
	}

	hook := *u
	hook.Scheme = "https"
	q := hook.Query()
	channel := q.Get("channel")

	username := q.Get("username")
	if username == "" {
		username = defaultMattermostUser
	}

	q.Del("channel")
	q.Del("username")
	hook.RawQuery = q.Encode()

	return &Mattermost{
		WebhookURL: hook.String(),
		Channel:    channel,
		Username:   username,
		Client:     &http.Client{Timeout: defaultHTTPTimeout},
	}, nil
}

func (m *Mattermost) Name() string {
	if m.Channel != "" {
		return "mattermost#" + m.Channel
	}

	return "mattermost"
}

type mattermostPayload struct {
	Text     string `json:"text"`
	Channel  string `json:"channel,omitempty"`
	Username string `json:"username,omitempty"`
}

func (m *Mattermost) Post(ctx context.Context, msg Message) error {
	body, err := json.Marshal(mattermostPayload{
		Text:     renderText(msg),
		Channel:  m.Channel,
		Username: m.Username,
	})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := m.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		return fmt.Errorf("mattermost responded %d: %s", resp.StatusCode, string(preview))
	}

	return nil
}

// renderText is shared with other webhook notifiers — they all want plain
// text with the title bolded and the manifest link appended.
func renderText(msg Message) string {
	var buf bytes.Buffer
	if msg.Title != "" {
		fmt.Fprintf(&buf, "**%s**\n", msg.Title)
	}

	if msg.Body != "" {
		buf.WriteString(msg.Body)
	}

	if msg.Link != "" {
		fmt.Fprintf(&buf, "\nManifest: %s", msg.Link)
	} else if msg.ManifestID != "" {
		fmt.Fprintf(&buf, "\nManifest ID: `%s`", msg.ManifestID)
	}

	return buf.String()
}
