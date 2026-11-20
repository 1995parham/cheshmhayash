// Package render prints decoded NATS messages for a person (pretty) or
// for a program (one JSON object per line).
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/tidwall/pretty"

	"github.com/1995parham/cheshmhayash/internal/codec"
)

// Mode selects the output format.
type Mode string

// The output modes.
const (
	ModePretty Mode = "pretty"
	ModeJSON   Mode = "json"
	ModeRaw    Mode = "raw"
)

// ParseMode validates s.
func ParseMode(s string) (Mode, error) {
	switch m := Mode(s); m {
	case ModePretty, ModeJSON, ModeRaw:
		return m, nil
	default:
		return "", fmt.Errorf("unknown output %q (pretty, json or raw)", s)
	}
}

// Envelope is one received message with its decoded payload.
type Envelope struct {
	Time    time.Time
	Subject string
	Reply   string
	Header  codec.Header
	Data    []byte
	Result  codec.Result
}

// Printer writes envelopes to W.
type Printer struct {
	W           io.Writer
	Mode        Mode
	Color       bool
	ShowHeaders bool
}

// IsTerminal reports whether w is a character device, which is where
// colors help.
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && isCharDevice(f)
}

// IsTerminalInput reports whether r is a character device, so a command
// does not wait for input that a person did not pipe in.
func IsTerminalInput(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && isCharDevice(f)
}

func isCharDevice(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

const (
	ansiReset = "\x1b[0m"
	ansiDim   = "\x1b[2m"
	ansiBold  = "\x1b[1m"
	ansiCyan  = "\x1b[36m"
	ansiYell  = "\x1b[33m"
)

// Print writes one envelope.
func (p *Printer) Print(e Envelope) error {
	switch p.Mode {
	case ModeJSON:
		return p.printJSON(e)
	case ModeRaw:
		_, err := fmt.Fprintf(p.W, "%s\n", e.Data)
		return err
	default:
		return p.printPretty(e)
	}
}

type jsonLine struct {
	Time    time.Time       `json:"time"`
	Subject string          `json:"subject"`
	Reply   string          `json:"reply,omitempty"`
	Size    int             `json:"size"`
	Header  codec.Header    `json:"headers,omitempty"`
	Format  codec.Format    `json:"format"`
	Type    string          `json:"type,omitempty"`
	Notes   []string        `json:"notes,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

func (p *Printer) printJSON(e Envelope) error {
	line, err := json.Marshal(jsonLine{
		Time:    e.Time,
		Subject: e.Subject,
		Reply:   e.Reply,
		Size:    len(e.Data),
		Header:  e.Header,
		Format:  e.Result.Format,
		Type:    e.Result.Type,
		Notes:   e.Result.Notes,
		Payload: e.Result.Body,
	})
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	_, err = fmt.Fprintf(p.W, "%s\n", line)
	return err
}

func (p *Printer) printPretty(e Envelope) error {
	var b strings.Builder

	kind := string(e.Result.Format)
	if e.Result.Type != "" {
		kind = e.Result.Type
	}
	parts := []string{
		p.paint(ansiDim, e.Time.Format("15:04:05.000")),
		p.paint(ansiBold, e.Subject),
		p.paint(ansiCyan, kind),
		p.paint(ansiDim, size(len(e.Data))),
	}
	if e.Reply != "" {
		parts = append(parts, p.paint(ansiDim, "reply "+e.Reply))
	}
	b.WriteString(strings.Join(parts, "  "))
	b.WriteByte('\n')

	if p.ShowHeaders {
		for _, k := range slices.Sorted(maps.Keys(e.Header)) {
			for _, v := range e.Header[k] {
				fmt.Fprintf(&b, "%s %s: %s\n", p.paint(ansiDim, "header"), k, v)
			}
		}
	}
	for _, n := range e.Result.Notes {
		fmt.Fprintf(&b, "%s %s\n", p.paint(ansiYell, "note"), n)
	}

	body := pretty.Pretty(e.Result.Body)
	if p.Color {
		body = pretty.Color(body, nil)
	}
	b.Write(body)
	if len(body) == 0 || body[len(body)-1] != '\n' {
		b.WriteByte('\n')
	}

	_, err := io.WriteString(p.W, b.String())
	return err
}

func (p *Printer) paint(code, s string) string {
	if !p.Color {
		return s
	}
	return code + s + ansiReset
}

func size(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
	}
}
