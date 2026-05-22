package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
)

// Webhook posts a structured JSON payload to any HTTPS URL. URL form:
//
//	webhook://host/path[?...]
//
// The scheme is rewritten to https for the POST. Unlike the Mattermost
// and Slack notifiers, the body carries the message as discrete fields
// (title, body, manifest_id, link) so downstream tooling can route or
// re-render however it likes — useful for n8n, custom internal hooks, etc.
type Webhook struct {
	URL    string
	Client *http.Client
}

func NewWebhook(u *neturl.URL) (*Webhook, error) {
	if u.Host == "" {
		return nil, fmt.Errorf("webhook url missing host: %s", u.Redacted())
	}

	hook := *u
	hook.Scheme = "https"

	return &Webhook{
		URL:    hook.String(),
		Client: &http.Client{Timeout: defaultHTTPTimeout},
	}, nil
}

func (w *Webhook) Name() string { return "webhook" }

// webhookPayload is a structural alias of Message so the json tags
// determine the on-wire field names without an extra copy step.
type webhookPayload Message

func (w *Webhook) Post(ctx context.Context, msg Message) error {
	body, err := json.Marshal(webhookPayload(msg))
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := w.Client
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

		return fmt.Errorf("webhook responded %d: %s", resp.StatusCode, string(preview))
	}

	return nil
}
