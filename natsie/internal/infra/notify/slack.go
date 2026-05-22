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

// Slack posts to a Slack incoming webhook. URL form:
//
//	slack://hooks.slack.com/services/T.../B.../...
//
// The scheme is rewritten to https for the POST. Channel and username are
// configured in the Slack webhook integration itself; there's no payload
// field for them in incoming webhooks.
type Slack struct {
	WebhookURL string
	Client     *http.Client
}

func NewSlack(u *neturl.URL) (*Slack, error) {
	if u.Host == "" {
		return nil, fmt.Errorf("slack url missing host: %s", u.Redacted())
	}

	hook := *u
	hook.Scheme = "https"

	return &Slack{
		WebhookURL: hook.String(),
		Client:     &http.Client{Timeout: defaultHTTPTimeout},
	}, nil
}

func (s *Slack) Name() string { return "slack" }

type slackPayload struct {
	Text string `json:"text"`
}

func (s *Slack) Post(ctx context.Context, msg Message) error {
	body, err := json.Marshal(slackPayload{Text: renderText(msg)})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := s.Client
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

		return fmt.Errorf("slack responded %d: %s", resp.StatusCode, string(preview))
	}

	return nil
}
