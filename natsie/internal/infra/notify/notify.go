// Package notify abstracts over chat / webhook destinations the bot can
// post messages to. Each implementation is selected by URL scheme:
//
//	stdout://                            — local debugging
//	mattermost://host/hooks/<token>      — Mattermost incoming webhook
//	slack://hooks.slack.com/services/... — Slack incoming webhook
//	webhook://host/path                  — generic JSON POST
//
// Implementations live in their own files (stdout.go, mattermost.go, ...).
// Dial in dial.go dispatches based on URL scheme.
package notify

import "context"

// Notifier sends Messages to a destination. Implementations must be safe
// for concurrent Post calls.
type Notifier interface {
	Post(ctx context.Context, msg Message) error

	// Name identifies the notifier in logs and the audit trail (e.g.
	// "mattermost#nats-cleanup"). Two notifiers of the same scheme
	// should return distinct names if they target different channels.
	Name() string
}

// Message is the payload to render. Body is pre-formatted plain text; the
// Notifier may wrap it (e.g. add backticks in Slack) but is not expected to
// re-format it. ManifestID and Link are optional pointers back to a stored
// manifest the message refers to.
//
// json tags exist so the webhook notifier can marshal it directly.
type Message struct {
	Title      string `json:"title,omitempty"`
	Body       string `json:"body,omitempty"`
	ManifestID string `json:"manifest_id,omitempty"`
	Link       string `json:"link,omitempty"`
}
