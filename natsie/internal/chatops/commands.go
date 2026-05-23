// Package chatops is the chat-frontend-agnostic command dispatcher.
//
// Both transports — the legacy /slash HTTP handler and the pull-mode
// WebSocket listener that natsie uses against Mattermost servers without
// outbound slash-command support — feed text through Dispatch and render
// the returned reply verbatim. Anything that wants to add a new sink
// (Slack RTM, Matrix, an IRC bridge) only needs to plug into Dispatch.
package chatops

import (
	"context"
	"fmt"
	"strings"

	"github.com/1995parham/natsie/internal/infra/store"
)

// Help is the canned usage message. It's identical across transports so
// users who hop between web slash commands and chat triggers learn one
// vocabulary.
func Help(trigger string) string {
	if trigger == "" {
		trigger = "/natsie"
	}

	return "natsie commands:\n" +
		fmt.Sprintf("- `%s list` — list stored manifest IDs\n", trigger) +
		fmt.Sprintf("- `%s show <id>` — preview a manifest\n", trigger) +
		fmt.Sprintf("- `%s help` — this message", trigger)
}

const (
	listLimit  = 20
	showLimit  = 10
	timeLayout = "2006-01-02T15:04:05Z"
)

// Dispatch parses the user's argv and returns the reply text to render.
// trigger is purely cosmetic — used to echo the right prefix in help.
func Dispatch(ctx context.Context, st store.Store, trigger string, argv []string) string {
	if len(argv) == 0 {
		return Help(trigger)
	}

	switch argv[0] {
	case "list":
		return list(ctx, st)
	case "show":
		if len(argv) < 2 {
			return fmt.Sprintf("usage: `%s show <manifest-id>`", trigger)
		}

		return show(ctx, st, argv[1])
	case "help":
		return Help(trigger)
	default:
		return fmt.Sprintf("unknown subcommand `%s`\n\n%s", argv[0], Help(trigger))
	}
}

func list(ctx context.Context, st store.Store) string {
	ids, err := st.List(ctx)
	if err != nil {
		return "list failed: " + err.Error()
	}

	if len(ids) == 0 {
		return "no manifests in store"
	}

	var b strings.Builder
	b.WriteString("Manifests:\n")

	for i, id := range ids {
		if i >= listLimit {
			fmt.Fprintf(&b, "...and %d more\n", len(ids)-listLimit)

			break
		}

		fmt.Fprintf(&b, "- `%s`\n", id)
	}

	return b.String()
}

func show(ctx context.Context, st store.Store, id string) string {
	m, err := st.Get(ctx, id)
	if err != nil {
		return fmt.Sprintf("manifest `%s` not found: %v", id, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Manifest `%s` (%d entries, generated %s):\n",
		id, len(m.Entries), m.GeneratedAt.Format(timeLayout))

	for i, e := range m.Entries {
		if i >= showLimit {
			fmt.Fprintf(&b, "...and %d more\n", len(m.Entries)-showLimit)

			break
		}

		fmt.Fprintf(&b, "- `%s/%s` (pending=%d, idle=%s)\n",
			e.Stream, e.Consumer, e.NumPending, e.Idle)
	}

	return b.String()
}
