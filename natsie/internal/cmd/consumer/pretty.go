package consumer

import (
	"io"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

	"github.com/1995parham/natsie/internal/scanner"
)

// renderPretty writes a colored ASCII table of scan rows. STALE rows are
// red, ACTIVE_PEER (stale locally but live on peer) yellow, ACTIVE
// green, everything else default. Designed for ad-hoc operator use —
// for piping or programmatic use, --format tsv|json stays untouched.
func renderPretty(w io.Writer, rows []scanner.Row) {
	t := table.NewWriter()
	t.SetOutputMirror(w)
	t.SetStyle(table.StyleRounded)
	t.Style().Color.Header = text.Colors{text.Bold}

	t.AppendHeader(table.Row{
		"stream", "consumer", "status", "pending", "idle", "peer",
	})

	for _, r := range rows {
		t.AppendRow(table.Row{
			r.Stream,
			r.Consumer,
			colorize(r.Status),
			r.NumPending,
			r.Idle.Truncate(time.Second),
			colorize(r.PeerStatus),
		})
	}

	t.Render()
}

func colorize(s scanner.Status) string {
	switch s {
	case scanner.StatusActive:
		return text.Colors{text.FgGreen}.Sprint(string(s))
	case scanner.StatusStale:
		return text.Colors{text.FgRed, text.Bold}.Sprint(string(s))
	case scanner.StatusAbsent:
		return text.Colors{text.FgYellow}.Sprint(string(s))
	case scanner.StatusError:
		return text.Colors{text.FgRed}.Sprint(string(s))
	case scanner.StatusOK, "":
		return string(s)
	default:
		return string(s)
	}
}
