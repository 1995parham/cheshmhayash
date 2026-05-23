package chatops

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/nats-io/jsm.go"

	"github.com/1995parham/natsie/internal/infra/natsctx"
)

const usageTopN = 10

// usage renders an "where's the capacity going" summary per context.
// With no target it fans out across every known context so one chat
// reply tells you the whole bot's footprint.
func usage(_ context.Context, target string) string {
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
			return fmt.Sprintf("no context named `%s`. Try `usage` with no argument.", target)
		}

		infos = filtered
	}

	if len(infos) == 0 {
		return "no NATS contexts configured"
	}

	var b strings.Builder

	for _, info := range infos {
		b.WriteString(usageFor(info.Name))
		b.WriteString("\n")
	}

	return b.String()
}

func usageFor(name string) string {
	nc, err := natsctx.Connect(name)
	if err != nil {
		return fmt.Sprintf("**`%s`** — connect failed: %v\n", name, err)
	}
	defer nc.Close()

	mgr, err := jsm.New(nc.Conn)
	if err != nil {
		return fmt.Sprintf("**`%s`** — jsm: %v\n", name, err)
	}

	streamNames, err := mgr.StreamNames(nil)
	if err != nil {
		return fmt.Sprintf("**`%s`** — list streams: %v\n", name, err)
	}

	type entry struct {
		name      string
		msgs      uint64
		bytes     uint64
		consumers int
		replicas  int
	}

	entries := make([]entry, 0, len(streamNames))

	var (
		totalMsgs, totalBytes uint64
		totalConsumers        int
	)

	for _, sn := range streamNames {
		s, err := mgr.LoadStream(sn)
		if err != nil {
			continue
		}

		state, err := s.State()
		if err != nil {
			continue
		}

		cfg := s.Configuration()

		entries = append(entries, entry{
			name: sn, msgs: state.Msgs, bytes: state.Bytes,
			consumers: state.Consumers, replicas: cfg.Replicas,
		})

		totalMsgs += state.Msgs
		totalBytes += state.Bytes
		totalConsumers += state.Consumers
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].bytes > entries[j].bytes })

	var b strings.Builder

	fmt.Fprintf(&b,
		"**`%s`** — %d streams, %s msgs, %s, %d consumers\n\n",
		name, len(entries), humanInt(totalMsgs), humanBytes(totalBytes), totalConsumers,
	)

	if len(entries) == 0 {
		b.WriteString("_no streams_\n")

		return b.String()
	}

	rows := make([][]string, 0, usageTopN)

	for i, e := range entries {
		if i >= usageTopN {
			rows = append(rows, []string{
				fmt.Sprintf("…and %d more", len(entries)-usageTopN),
				"", "", "", "",
			})

			break
		}

		rows = append(rows, []string{
			"`" + e.name + "`",
			humanInt(e.msgs),
			humanBytes(e.bytes),
			intStr(e.consumers),
			fmt.Sprintf("R=%d", e.replicas),
		})
	}

	b.WriteString("Top streams by bytes:\n\n")
	b.WriteString(mdTable(
		[]string{"Stream", "Msgs", "Bytes", "Consumers", "Repl"},
		rows,
	))

	return b.String()
}
