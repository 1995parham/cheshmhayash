package chatops

import (
	"context"
	"fmt"
	"strings"

	"github.com/nats-io/jsm.go"

	"github.com/1995parham/natsie/internal/infra/natsctx"
)

// clusterDetail reports server-side facts about the connected NATS
// cluster from the bot's perspective. We're a regular client (no $SYS
// account), so the view is intentionally narrow: connected server,
// discovered peers, observable stream/consumer footprint. No JetStream
// reservation numbers — those require monitor port or $SYS access.
func clusterDetail(_ context.Context, ctxName string) string {
	nc, err := natsctx.Connect(ctxName)
	if err != nil {
		return fmt.Sprintf("connect %s: %v", ctxName, err)
	}
	defer nc.Close()

	rtt, _ := nc.RTT()

	var b strings.Builder

	fmt.Fprintf(&b, "**`%s`** — connected to `%s` (%s), RTT %s\n\n",
		ctxName,
		nc.ConnectedServerName(),
		nc.ConnectedServerVersion(),
		rtt.Round(0).String(),
	)

	discovered := nc.DiscoveredServers()
	servers := nc.Servers()

	rows := [][]string{}

	for _, url := range servers {
		isCurrent := "—"
		if url == nc.ConnectedUrl() {
			isCurrent = "✅"
		}

		rows = append(rows, []string{url, isCurrent})
	}

	if len(rows) > 0 {
		b.WriteString("**Servers (known)**\n\n")
		b.WriteString(mdTable([]string{"URL", "Current"}, rows))
	}

	if len(discovered) > 0 {
		fmt.Fprintf(&b, "\n**Discovered peers** (%d): ", len(discovered))

		for i, d := range discovered {
			if i > 0 {
				b.WriteString(", ")
			}

			fmt.Fprintf(&b, "`%s`", d)
		}

		b.WriteString("\n")
	}

	// Observable JetStream footprint. Best-effort — if jsm.New or
	// StreamNames fails we just skip the footprint line rather than
	// failing the whole reply.
	mgr, jsmErr := jsm.New(nc.Conn)
	if jsmErr == nil {
		writeJSFootprint(&b, mgr)
	}

	b.WriteString("\n_Server-reported limits (memory/storage reservations) need monitor-port or $SYS access; not available from a client account._\n")

	return b.String()
}

// writeJSFootprint enumerates streams and totals their consumers. Errors
// are swallowed silently — a footprint line is a nice-to-have, not a
// guarantee.
func writeJSFootprint(b *strings.Builder, mgr *jsm.Manager) {
	streamNames, err := mgr.StreamNames(nil)
	if err != nil {
		return
	}

	var consumers int

	for _, sn := range streamNames {
		s, err := mgr.LoadStream(sn)
		if err != nil {
			continue
		}

		state, err := s.State()
		if err != nil {
			continue
		}

		consumers += state.Consumers
	}

	fmt.Fprintf(b,
		"\n**JetStream footprint** — %d streams, %d consumers (visible to this account)\n",
		len(streamNames), consumers,
	)
}
