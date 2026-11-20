package chatops

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nats-io/jsm.go"

	"github.com/1995parham/natsie/internal/infra/natsctx"
)

const consumersDetailLimit = 30

// consumersDetail lists every consumer on the named stream with no
// threshold filtering. Useful for "what's reading rides right now?"
// versus the scanner output which only reports stale-ish rows.
func consumersDetail(_ context.Context, ctxName, streamName string) string {
	nc, err := natsctx.Connect(ctxName)
	if err != nil {
		return fmt.Sprintf("connect %s: %v", ctxName, err)
	}
	defer nc.Close()

	mgr, err := jsm.New(nc.Conn)
	if err != nil {
		return fmt.Sprintf("jsm: %v", err)
	}

	s, err := mgr.LoadStream(streamName)
	if err != nil {
		return fmt.Sprintf("load stream `%s`: %v", streamName, err)
	}

	names, err := s.ConsumerNames()
	if err != nil {
		return fmt.Sprintf("consumer names: %v", err)
	}

	if len(names) == 0 {
		return fmt.Sprintf("**`%s` / `%s`** has no consumers", ctxName, streamName)
	}

	sort.Strings(names)

	now := time.Now()
	rows := make([][]string, 0, len(names))

	for i, cn := range names {
		if i >= consumersDetailLimit {
			rows = append(rows, []string{fmt.Sprintf("…and %d more", len(names)-consumersDetailLimit), "", "", "", "", ""})

			break
		}

		c, err := s.LoadConsumer(cn)
		if err != nil {
			rows = append(rows, []string{"`" + cn + "`", "ERR", "—", "—", "—", err.Error()})

			continue
		}

		info, err := c.LatestState()
		if err != nil {
			rows = append(rows, []string{"`" + cn + "`", "ERR", "—", "—", "—", err.Error()})

			continue
		}

		kind := "pull"
		if info.PushBound {
			kind = "push"
		}

		idle := "—"
		if info.AckFloor.Last != nil {
			idle = now.Sub(*info.AckFloor.Last).Truncate(time.Second).String()
		}

		rows = append(rows, []string{
			"`" + cn + "`",
			kind,
			intStr(int(info.NumPending)), //nolint:gosec // counts bounded by stream depth
			intStr(info.NumWaiting),
			intStr(info.NumRedelivered),
			idle,
		})
	}

	var b strings.Builder

	fmt.Fprintf(&b, "**`%s` / `%s`** — %d consumers\n\n", ctxName, streamName, len(names))
	b.WriteString(mdTable(
		[]string{"Consumer", "Kind", "Pending", "Waiting", "Redeliv.", "Idle"},
		rows,
	))

	return b.String()
}
