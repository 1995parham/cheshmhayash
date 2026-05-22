// Package audit appends one JSON object per line to a long-lived file.
//
// Two kinds of records dominate: scan-completed events emitted by the bot
// scheduler, and apply/approval events emitted by either the CLI or the
// HTTP handler. The format is intentionally schema-light — `kind` plus a
// handful of optional fields — so log rotators and grep can keep working
// as the bot grows new event types.
//
// A nil *Logger is valid and discards Log calls; this keeps the bot
// configuration "audit_log: <empty>" path free of nil checks.
package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// Event is the union type. Unset fields are omitted from the JSONL output.
type Event struct {
	Timestamp  time.Time      `json:"timestamp"`
	Kind       string         `json:"kind"`
	Manifest   string         `json:"manifest,omitempty"`
	Schedule   string         `json:"schedule,omitempty"`
	Source     string         `json:"source,omitempty"`
	Entries    int            `json:"entries,omitempty"`
	Result     string         `json:"result,omitempty"`
	DryRun     bool           `json:"dry_run,omitempty"`
	Error      string         `json:"error,omitempty"`
	Extra      map[string]any `json:"extra,omitempty"`
}

// Logger appends Event records to a file. The zero value is unusable;
// construct with NewLogger. The nil *Logger silently discards Log calls.
type Logger struct {
	mu sync.Mutex
	f  *os.File
}

// NewLogger opens path in append mode (creating with 0600 if missing).
// An empty path returns (nil, nil) so callers can opt out of audit
// logging without conditional construction.
func NewLogger(path string) (*Logger, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open audit log %s: %w", path, err)
	}
	return &Logger{f: f}, nil
}

// Log appends one event. Safe for concurrent use. A nil receiver is a no-op.
func (l *Logger) Log(e Event) error {
	if l == nil {
		return nil
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	if e.Kind == "" {
		return errors.New("audit: event Kind required")
	}
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("audit marshal: %w", err)
	}
	b = append(b, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := l.f.Write(b); err != nil {
		return fmt.Errorf("audit write: %w", err)
	}
	return nil
}

// Close flushes and closes the underlying file. Safe to call on nil.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	return l.f.Close()
}
