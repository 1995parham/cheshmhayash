package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// webhook is the shared HTTP-POST-{text} implementation used by
// Slack, Mattermost, and Matrix (via a Slack-compatible bridge).
type webhook struct {
	name     string // "slack" | "mattermost" | "matrix"
	url      string
	channel  string
	username string
	httpc    *http.Client
}

func newWebhook(c ProviderConfig, httpc *http.Client) *webhook {
	return &webhook{
		name:     c.Provider,
		url:      c.URL,
		channel:  c.Channel,
		username: c.Username,
		httpc:    httpc,
	}
}

func (w *webhook) Name() string { return w.name }

func (w *webhook) Send(ctx context.Context, n Notification) error {
	// Text body — Markdown-ish; Slack/Mattermost render it, Matrix
	// hookshot/maubot bridges typically render it as plain text but
	// keep the structure readable.
	text := fmt.Sprintf("*[%s]* %s\n%s", n.Cluster, n.Title, n.Body)

	payload := map[string]any{"text": text}
	if w.channel != "" {
		payload["channel"] = w.channel
	}
	if w.username != "" {
		payload["username"] = w.username
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := w.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("status %d from %s", resp.StatusCode, w.name)
	}
	return nil
}
