package chatops

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/nats-io/jsm.go"

	"github.com/1995parham/natsie/internal/infra/natsctx"
)

// streams lists every stream on the named context (or all contexts when
// target == ""), one table per cluster. We open a connection per
// context, fetch StreamNames, then State() each for msg/byte counts.
//
// Errors per-context are reported in-line as a row rather than crashing
// the whole listing — a chat command should never refuse to show what
// it can.
func streams(ctx context.Context, target string) string {
	infos, err := natsctx.List()
	if err != nil {
		return "list contexts failed: " + err.Error()
	}

	if target != "" {
		filtered := infos[:0]

		for _, info := range infos {
			if info.Name == target {
				filtered = append(filtered, info)
			}
		}

		if len(filtered) == 0 {
			return fmt.Sprintf("no context named `%s`. Try `streams` with no argument to list all.", target)
		}

		infos = filtered
	}

	if len(infos) == 0 {
		return "no NATS contexts configured for this natsie instance"
	}

	var b strings.Builder

	for _, info := range infos {
		b.WriteString(renderStreamsFor(ctx, info.Name))
		b.WriteString("\n")
	}

	return b.String()
}

func renderStreamsFor(_ context.Context, name string) string {
	nc, err := natsctx.Connect(name)
	if err != nil {
		return fmt.Sprintf("**`%s`** — connect failed: %v\n", name, err)
	}
	defer nc.Close()

	mgr, err := jsm.New(nc.Conn)
	if err != nil {
		return fmt.Sprintf("**`%s`** — jsm: %v\n", name, err)
	}

	names, err := mgr.StreamNames(nil)
	if err != nil {
		return fmt.Sprintf("**`%s`** — list streams: %v\n", name, err)
	}

	sort.Strings(names)

	if len(names) == 0 {
		return fmt.Sprintf("**`%s`** — no streams\n", name)
	}

	rows := make([][]string, 0, len(names))

	for _, sn := range names {
		s, err := mgr.LoadStream(sn)
		if err != nil {
			rows = append(rows, []string{"`" + sn + "`", "—", "—", "—", "ERR: " + truncate(err.Error(), 30)})

			continue
		}

		state, err := s.State()
		if err != nil {
			rows = append(rows, []string{"`" + sn + "`", "—", "—", "—", "ERR: " + truncate(err.Error(), 30)})

			continue
		}

		cfg := s.Configuration()

		rows = append(rows, []string{
			"`" + sn + "`",
			humanInt(state.Msgs),
			humanBytes(state.Bytes),
			intStr(state.Consumers),
			fmt.Sprintf("R=%d %s", cfg.Replicas, cfg.Retention),
		})
	}

	header := fmt.Sprintf("**`%s`** — %d streams\n\n", name, len(names))

	return header + mdTable(
		[]string{"Stream", "Msgs", "Bytes", "Consumers", "Config"},
		rows,
	)
}
