package chatops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/1995parham/natsie/internal/audit"
	"github.com/1995parham/natsie/internal/infra/natsctx"
	"github.com/1995parham/natsie/internal/manifest"
	"github.com/1995parham/natsie/internal/scanner"
	"github.com/1995parham/natsie/internal/version"
)

const (
	// On-demand chat scans use the same default thresholds as the
	// scheduled cron path. An operator who wants something custom can
	// pass --min-pending / --min-idle on the CLI; the chat command
	// intentionally keeps a tiny surface area.
	scanDefaultMinPending = 10000
	scanDefaultMinIdle    = 24 * time.Hour
	scanChatTimeout       = 90 * time.Second
)

// scan runs a one-shot scan against ctxName (optionally filtered to a
// single stream), stores the resulting manifest if any stale rows are
// found, and returns a chat-formatted summary with a signed approve URL
// when the bot is configured with HTTP base URL + signing key.
//
// Run inline on the WebSocket handler goroutine — this is fine on chat
// timescales because the listener has no synchronous 3 s deadline like
// HTTP slash commands. Capped by scanChatTimeout above to avoid a
// runaway scan blocking the goroutine indefinitely.
func scan(parent context.Context, deps Deps, ctxName, streamFilter string) string {
	ctx, cancel := context.WithTimeout(parent, scanChatTimeout)
	defer cancel()

	nc, err := natsctx.Connect(ctxName)
	if err != nil {
		return fmt.Sprintf("connect `%s`: %v", ctxName, err)
	}
	defer nc.Close()

	opts := scanner.Options{
		Stream:     streamFilter,
		MinPending: scanDefaultMinPending,
		MinIdle:    scanDefaultMinIdle,
	}

	rows, err := scanner.Scan(ctx, nc, nil, opts)
	if err != nil {
		return fmt.Sprintf("scan failed: %v", err)
	}

	stale := 0

	for _, r := range rows {
		if r.Status == scanner.StatusStale {
			stale++
		}
	}

	if stale == 0 {
		_ = deps.Audit.Log(audit.Event{
			Kind: "scan.adhoc", Schedule: "chat:" + ctxName, Entries: 0,
		})

		return fmt.Sprintf("**`%s`** scan: %d total consumers checked, **0 stale** — nothing to clean up.", ctxName, len(rows))
	}

	m := buildScanManifest(ctxName, opts, rows)
	id := fmt.Sprintf("chat-%s-%s", ctxName, time.Now().UTC().Format("20060102T150405Z"))

	if err := deps.Store.Put(ctx, id, m); err != nil {
		return fmt.Sprintf("scan found %d stale but **storing the manifest failed**: %v", stale, err)
	}

	_ = deps.Audit.Log(audit.Event{
		Kind: "scan.adhoc", Schedule: "chat:" + ctxName, Manifest: id, Entries: stale,
	})

	return formatScanReply(deps, ctxName, id, m, stale)
}

func formatScanReply(deps Deps, ctxName, id string, m *manifest.Manifest, stale int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "**`%s`** scan: **%d stale consumers** found (manifest `%s`)\n\n", ctxName, stale, id)

	rows := make([][]string, 0, len(m.Entries))

	for i, e := range m.Entries {
		if i >= showLimit {
			rows = append(rows, []string{fmt.Sprintf("…and %d more", len(m.Entries)-showLimit), "", "", ""})

			break
		}

		rows = append(rows, []string{
			"`" + e.Stream + "`",
			"`" + e.Consumer + "`",
			humanInt(uint64(e.NumPending)), //nolint:gosec // NumPending is non-negative
			e.Idle.String(),
		})
	}

	b.WriteString(mdTable([]string{"Stream", "Consumer", "Pending", "Idle"}, rows))

	base := strings.TrimSuffix(deps.BaseURL, "/")
	if base != "" && deps.SigningKey != "" {
		token := signApprovalToken(deps.SigningKey, id)
		fmt.Fprintf(&b, "\n**Approve**: %s/approve/%s?token=%s\n", base, id, token)
	} else {
		fmt.Fprintf(&b, "\n_(approve URL not generated — bot has no base_url / signing_key configured)_\n")
	}

	return b.String()
}

// buildScanManifest is a small copy of cmd/bot/serve.go's buildManifest —
// kept independent because plumbing the same helper through to chatops
// would mean making the bot package importable here, and right now bot
// imports chatops already. Two copies of ~15 lines is cheaper than the
// dependency inversion.
func buildScanManifest(ctxName string, opts scanner.Options, rows []scanner.Row) *manifest.Manifest {
	m := &manifest.Manifest{
		Version:     manifest.Version,
		GeneratedAt: time.Now().UTC(),
		GeneratedBy: "natsie " + version.Short() + " (chat)",
		Scan: manifest.ScanInfo{
			Context:    ctxName,
			Stream:     opts.Stream,
			MinPending: opts.MinPending,
			MinIdle:    opts.MinIdle,
		},
	}

	for _, r := range rows {
		if r.Status != scanner.StatusStale {
			continue
		}

		m.Entries = append(m.Entries, manifest.Entry{
			Cluster:    r.Cluster,
			Stream:     r.Stream,
			Consumer:   r.Consumer,
			Status:     string(r.Status),
			PeerStatus: string(r.PeerStatus),
			NumPending: r.NumPending,
			Idle:       r.Idle.Truncate(time.Second),
			LastAck:    r.LastAck.UTC(),
		})
	}

	return m
}
