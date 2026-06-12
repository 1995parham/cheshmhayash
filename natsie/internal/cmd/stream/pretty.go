package stream

import (
	"fmt"
	"io"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"

	"github.com/1995parham/natsie/internal/scanner"
)

// renderPretty writes the per-stream table followed by a placement-skew
// summary (servers ordered by how many streams they lead and host).
func renderPretty(w io.Writer, rows []scanner.StreamReportRow) {
	t := table.NewWriter()
	t.SetOutputMirror(w)
	t.SetStyle(table.StyleRounded)
	t.Style().Color.Header = text.Colors{text.Bold}

	t.AppendHeader(table.Row{
		"stream", "retention", "R", "msgs", "size", "max_age", "max_bytes", "max_msgs", "consumers", "leader", "peers",
	})

	for _, r := range rows {
		t.AppendRow(table.Row{
			r.Stream,
			r.Retention,
			r.Replicas,
			r.Messages,
			humanBytes(r.Bytes),
			fmtDuration(r.MaxAge),
			fmtLimit(r.MaxBytes),
			fmtLimit(r.MaxMsgs),
			r.Consumers,
			r.Leader,
			strings.Join(r.Peers, ", "),
		})
	}

	t.Render()

	skew := scanner.PlacementSkew(rows)
	if len(skew) == 0 {
		return
	}

	_, _ = fmt.Fprintln(w, "\nplacement skew:")

	st := table.NewWriter()
	st.SetOutputMirror(w)
	st.SetStyle(table.StyleRounded)
	st.Style().Color.Header = text.Colors{text.Bold}
	st.AppendHeader(table.Row{"server", "leads", "replicas"})

	for _, s := range skew {
		st.AppendRow(table.Row{s.Server, s.Leads, s.Replicas})
	}

	st.Render()
}

// humanBytes renders a byte count as a compact human-readable size.
func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}

	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
