package chatops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/jsm.go"
	"github.com/nats-io/jsm.go/api"

	"github.com/1995parham/natsie/internal/infra/natsctx"
)

// streamLast renders metadata for the most recent message in a stream.
// With an empty subject we look up the stream's LastSeq and fetch by
// sequence; with a non-empty subject we use the per-subject variant,
// which is the only way to ask "last for this subject" without scanning.
func streamLast(_ context.Context, ctxName, name, subject string) string {
	nc, err := natsctx.Connect(ctxName)
	if err != nil {
		return fmt.Sprintf("connect %s: %v", ctxName, err)
	}
	defer nc.Close()

	mgr, err := jsm.New(nc.Conn)
	if err != nil {
		return fmt.Sprintf("jsm: %v", err)
	}

	s, err := mgr.LoadStream(name)
	if err != nil {
		return fmt.Sprintf("load stream `%s`: %v", name, err)
	}

	msg, err := loadLastMessage(s, subject)
	if err != nil {
		return fmt.Sprintf("stream `%s` / `%s`: %v", ctxName, name, err)
	}

	var b strings.Builder

	if subject != "" {
		fmt.Fprintf(&b, "**`%s` / `%s`** — last message on `%s`\n\n", ctxName, name, subject)
	} else {
		fmt.Fprintf(&b, "**`%s` / `%s`** — last message\n\n", ctxName, name)
	}

	age := time.Since(msg.Time).Truncate(time.Second).String()

	b.WriteString(mdTable(
		[]string{"Subject", "Seq", "Time", "Age", "Payload"},
		[][]string{{
			"`" + msg.Subject + "`",
			humanInt(msg.Sequence),
			msg.Time.UTC().Format(timeLayout),
			age,
			humanBytes(uint64(len(msg.Data))), //nolint:gosec // len() ≥ 0
		}},
	))

	headers := parseNATSHeaders(msg.Header)
	if len(headers) > 0 {
		fmt.Fprintf(&b, "\n**Headers** (%d)\n\n", len(headers))

		rows := make([][]string, 0, len(headers))
		for _, h := range headers {
			rows = append(rows, []string{"`" + h.key + "`", h.value})
		}

		b.WriteString(mdTable([]string{"Key", "Value"}, rows))
	}

	return b.String()
}

// loadLastMessage fetches the last stored message in a stream, either
// the absolute last (LastSeq) when subject is empty, or the last on a
// specific subject otherwise.
func loadLastMessage(s *jsm.Stream, subject string) (*api.StoredMsg, error) {
	if subject != "" {
		msg, err := s.ReadLastMessageForSubject(subject)
		if err != nil {
			return nil, fmt.Errorf("read last for subject %q: %w", subject, err)
		}

		return msg, nil
	}

	info, err := s.Information()
	if err != nil {
		return nil, fmt.Errorf("info: %w", err)
	}

	if info.State.Msgs == 0 {
		return nil, errStreamEmpty
	}

	msg, err := s.ReadMessage(info.State.LastSeq)
	if err != nil {
		return nil, fmt.Errorf("read seq %d: %w", info.State.LastSeq, err)
	}

	return msg, nil
}

var errStreamEmpty = errors.New("stream is empty")

type natsHeader struct {
	key, value string
}

// parseNATSHeaders decodes the raw NATS header block. The wire format
// is "NATS/1.0\r\nKey: Value\r\n...\r\n\r\n"; we tolerate plain LF as
// well and skip the version preamble (which may carry an inline status).
func parseNATSHeaders(raw []byte) []natsHeader {
	if len(raw) == 0 {
		return nil
	}

	out := []natsHeader{}

	for line := range bytes.SplitSeq(raw, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if len(line) == 0 {
			continue
		}

		if bytes.HasPrefix(line, []byte("NATS/")) {
			continue
		}

		idx := bytes.IndexByte(line, ':')
		if idx <= 0 {
			continue
		}

		out = append(out, natsHeader{
			key:   string(bytes.TrimSpace(line[:idx])),
			value: string(bytes.TrimSpace(line[idx+1:])),
		})
	}

	return out
}
