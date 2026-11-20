package chatops

import (
	"fmt"
	"strconv"
	"strings"
)

// mdTable returns a GitHub-flavored markdown pipe table. Mattermost
// renders these inline (and in threads) which is what we want for chat
// replies. Cell contents are not escaped beyond a backtick-safe rewrite
// — callers are responsible for keeping things printable.
func mdTable(headers []string, rows [][]string) string {
	if len(headers) == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString("| ")
	b.WriteString(strings.Join(headers, " | "))
	b.WriteString(" |\n|")

	for range headers {
		b.WriteString(" --- |")
	}

	b.WriteString("\n")

	for _, r := range rows {
		b.WriteString("| ")

		for i, c := range r {
			if i > 0 {
				b.WriteString(" | ")
			}

			b.WriteString(cellEscape(c))
		}

		// Pad short rows so the column count matches the header.
		for i := len(r); i < len(headers); i++ {
			b.WriteString(" | ")
		}

		b.WriteString(" |\n")
	}

	return b.String()
}

// cellEscape neutralizes the only character that breaks a markdown pipe
// table (the pipe itself) and trims newlines that would split a row.
func cellEscape(s string) string {
	if s == "" {
		return ""
	}

	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")

	return s
}

// humanInt renders a count with K / M / G suffixes for chat tables.
// Below 1000 the number is shown as-is; above that we keep one decimal.
func humanInt(n uint64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fG", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return strconv.FormatUint(n, 10)
	}
}

// humanBytes renders a byte count in IEC units (KiB / MiB / GiB). The
// chat tables use this for stream byte totals.
func humanBytes(n uint64) string {
	const k = 1024
	switch {
	case n >= k*k*k:
		return fmt.Sprintf("%.1f GiB", float64(n)/(k*k*k))
	case n >= k*k:
		return fmt.Sprintf("%.1f MiB", float64(n)/(k*k))
	case n >= k:
		return fmt.Sprintf("%.1f KiB", float64(n)/k)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
