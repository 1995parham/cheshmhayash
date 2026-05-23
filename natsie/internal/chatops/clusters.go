package chatops

import (
	"context"
	"sync"

	"github.com/1995parham/natsie/internal/infra/natsctx"
)

// clusters renders the NATS contexts this natsie pod has access to as a
// markdown table, probing each in parallel so the table is annotated
// with a ✅ / ❌ reachability cell.
//
// Returns a friendly message when no contexts are installed at all.
func clusters(ctx context.Context) string {
	infos, err := natsctx.List()
	if err != nil {
		return "list contexts failed: " + err.Error()
	}

	if len(infos) == 0 {
		return "no NATS contexts configured for this natsie instance"
	}

	type probeResult struct {
		ok  bool
		err string
	}

	results := make([]probeResult, len(infos))

	var wg sync.WaitGroup
	for i, info := range infos {
		wg.Add(1)

		go func(i int, name string) {
			defer wg.Done()

			if err := natsctx.Probe(ctx, name); err != nil {
				results[i] = probeResult{ok: false, err: err.Error()}
			} else {
				results[i] = probeResult{ok: true}
			}
		}(i, info.Name)
	}

	wg.Wait()

	rows := make([][]string, 0, len(infos))

	reachable := 0

	for i, info := range infos {
		status := "❌ " + truncate(results[i].err, 40)
		if results[i].ok {
			status = "✅ reachable"
			reachable++
		}

		rows = append(rows, []string{
			"`" + info.Name + "`",
			"`" + info.URL + "`",
			info.User,
			status,
		})
	}

	header := "**Clusters this natsie instance can dial** " +
		"(reachable: " + intStr(reachable) + " / " + intStr(len(infos)) + ")\n\n"

	return header + mdTable(
		[]string{"Name", "URL", "User", "Status"},
		rows,
	)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n-1] + "…"
}

func intStr(n int) string {
	return humanInt(uint64(n)) //nolint:gosec // n is a count of contexts, bounded by file system entries
}
