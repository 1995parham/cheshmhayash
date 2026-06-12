package peer

import (
	"io"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

	"github.com/1995parham/natsie/internal/scanner"
)

// renderPretty writes a colored table of peer rows. GHOST peers are red,
// LAGGING yellow, OK default — designed for ad-hoc operator use; tsv/json
// stay machine-friendly.
func renderPretty(w io.Writer, rows []scanner.PeerRow) {
	t := table.NewWriter()
	t.SetOutputMirror(w)
	t.SetStyle(table.StyleRounded)
	t.Style().Color.Header = text.Colors{text.Bold}

	t.AppendHeader(table.Row{
		"peer", "health", "groups", "offline", "current", "leads", "sample",
	})

	for _, r := range rows {
		t.AppendRow(table.Row{
			r.Peer,
			colorize(r.Health),
			r.Groups,
			r.Offline,
			r.Current,
			r.Leads,
			strings.Join(r.Sample, ", "),
		})
	}

	t.Render()
}

func colorize(h scanner.PeerHealth) string {
	switch h {
	case scanner.PeerGhost:
		return text.Colors{text.FgRed, text.Bold}.Sprint(string(h))
	case scanner.PeerLagging:
		return text.Colors{text.FgYellow}.Sprint(string(h))
	case scanner.PeerOK:
		return text.Colors{text.FgGreen}.Sprint(string(h))
	default:
		return string(h)
	}
}
