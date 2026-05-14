package notify

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Event is the rendered, human-friendly form of a NATS advisory.
type Event struct {
	Title string
	Body  string
}

// classify turns one NATS advisory message into a renderable Event.
// Returns nil when the subject is one we don't have a formatter for —
// the caller drops those rather than spamming a generic notification.
//
// Subjects handled (account-local and $SYS-bridged forms):
//   - $JS.EVENT.ADVISORY.STREAM.{CREATED,UPDATED,DELETED}.<stream>
//   - $JS.EVENT.ADVISORY.STREAM.LEADER_ELECTED.<stream>
//   - $JS.EVENT.ADVISORY.STREAM.QUORUM_LOST.<stream>
//   - $JS.EVENT.ADVISORY.CONSUMER.{CREATED,DELETED}.<stream>.<consumer>
//   - $JS.EVENT.ADVISORY.CONSUMER.LEADER_ELECTED.<stream>.<consumer>
//   - $JS.EVENT.ADVISORY.CONSUMER.QUORUM_LOST.<stream>.<consumer>
func classify(subject string, raw []byte) *Event {
	suffix := jsAdvisorySuffix(subject)
	if suffix == "" {
		return nil
	}
	parts := strings.Split(suffix, ".")
	if len(parts) < 2 {
		return nil
	}

	var payload map[string]any
	_ = json.Unmarshal(raw, &payload) // best-effort; some advisories ship empty bodies

	leader := str(payload, "leader")
	replicas := strSlice(payload, "replicas")
	account := str(payload, "account")

	kind, action := parts[0], parts[1]
	switch kind {
	case "STREAM":
		if len(parts) < 3 {
			return nil
		}
		stream := parts[2]
		return streamEvent(action, account, stream, leader, replicas)
	case "CONSUMER":
		if len(parts) < 4 {
			return nil
		}
		stream, consumer := parts[2], parts[3]
		return consumerEvent(action, account, stream, consumer, leader, replicas)
	}
	return nil
}

// jsAdvisorySuffix returns the part of the subject after the advisory
// prefix, or "" if it isn't a JetStream advisory we care about.
//
// $JS.EVENT.ADVISORY.<rest>                          → <rest>
// $SYS.ACCOUNT.<acct>.JETSTREAM.EVENT.ADVISORY.<rest> → <rest>
func jsAdvisorySuffix(subj string) string {
	if s, ok := strings.CutPrefix(subj, "$JS.EVENT.ADVISORY."); ok {
		return s
	}
	if s, ok := strings.CutPrefix(subj, "$SYS.ACCOUNT."); ok {
		// "<acct>.JETSTREAM.EVENT.ADVISORY.<rest>"
		_, rest, ok := strings.Cut(s, ".JETSTREAM.EVENT.ADVISORY.")
		if !ok {
			return ""
		}
		return rest
	}
	return ""
}

func streamEvent(action, account, stream, leader string, replicas []string) *Event {
	acctTag := ""
	if account != "" {
		acctTag = fmt.Sprintf(" (account %s)", account)
	}
	switch action {
	case "CREATED":
		return &Event{
			Title: fmt.Sprintf("Stream `%s` created", stream),
			Body:  fmt.Sprintf("New stream `%s`%s.", stream, acctTag),
		}
	case "UPDATED":
		return &Event{
			Title: fmt.Sprintf("Stream `%s` updated", stream),
			Body:  fmt.Sprintf("Stream config changed for `%s`%s.", stream, acctTag),
		}
	case "DELETED":
		return &Event{
			Title: fmt.Sprintf("Stream `%s` deleted", stream),
			Body:  fmt.Sprintf("Stream `%s`%s was removed.", stream, acctTag),
		}
	case "LEADER_ELECTED":
		return &Event{
			Title: fmt.Sprintf("Stream `%s` leader elected", stream),
			Body: fmt.Sprintf("New leader: `%s`%s%s.",
				leader, acctTag, replicaTail(replicas)),
		}
	case "QUORUM_LOST":
		return &Event{
			Title: fmt.Sprintf("⚠ Stream `%s` lost quorum", stream),
			Body:  fmt.Sprintf("Stream `%s`%s lost quorum and is unavailable for writes until a new leader is elected.", stream, acctTag),
		}
	}
	return nil
}

func consumerEvent(action, account, stream, consumer, leader string, replicas []string) *Event {
	acctTag := ""
	if account != "" {
		acctTag = fmt.Sprintf(" (account %s)", account)
	}
	switch action {
	case "CREATED":
		return &Event{
			Title: fmt.Sprintf("Consumer `%s/%s` created", stream, consumer),
			Body:  fmt.Sprintf("New consumer `%s` on stream `%s`%s.", consumer, stream, acctTag),
		}
	case "DELETED":
		return &Event{
			Title: fmt.Sprintf("Consumer `%s/%s` deleted", stream, consumer),
			Body:  fmt.Sprintf("Consumer `%s` on stream `%s`%s was removed.", consumer, stream, acctTag),
		}
	case "LEADER_ELECTED":
		return &Event{
			Title: fmt.Sprintf("Consumer `%s/%s` leader elected", stream, consumer),
			Body: fmt.Sprintf("New leader: `%s`%s%s.",
				leader, acctTag, replicaTail(replicas)),
		}
	case "QUORUM_LOST":
		return &Event{
			Title: fmt.Sprintf("⚠ Consumer `%s/%s` lost quorum", stream, consumer),
			Body:  fmt.Sprintf("Consumer `%s` on stream `%s`%s lost quorum.", consumer, stream, acctTag),
		}
	}
	return nil
}

func replicaTail(replicas []string) string {
	if len(replicas) == 0 {
		return ""
	}
	return fmt.Sprintf(" · replicas: %s", strings.Join(replicas, ", "))
}

// str returns the named string field if present.
func str(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// strSlice extracts a string slice — JS advisories ship `replicas` as
// `[{name: "..."}, ...]` so we accept either []string or []map.
func strSlice(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		switch x := v.(type) {
		case string:
			out = append(out, x)
		case map[string]any:
			if name, ok := x["name"].(string); ok {
				out = append(out, name)
			}
		}
	}
	return out
}
