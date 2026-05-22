package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestLogAppendsJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	l, err := NewLogger(path)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	t.Cleanup(func() { _ = l.Close() })

	want := []Event{
		{Kind: "scan", Schedule: "daily", Entries: 3},
		{Kind: "approve.apply", Manifest: "m-1", Result: "1 deleted"},
	}
	for _, e := range want {
		if err := l.Log(e); err != nil {
			t.Fatalf("Log: %v", err)
		}
	}

	f, err := os.Open(path) //nolint:gosec // path is test-controlled
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()

	var got []Event

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("unmarshal: %v line=%q", err, sc.Text())
		}

		got = append(got, e)
	}

	if len(got) != len(want) {
		t.Fatalf("got %d lines want %d", len(got), len(want))
	}

	for i, w := range want {
		if got[i].Kind != w.Kind || got[i].Schedule != w.Schedule || got[i].Entries != w.Entries {
			t.Errorf("event %d: got %+v want %+v", i, got[i], w)
		}

		if got[i].Timestamp.IsZero() {
			t.Errorf("event %d: zero timestamp", i)
		}
	}
}

func TestLogRejectsMissingKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	l, _ := NewLogger(path)

	t.Cleanup(func() { _ = l.Close() })

	if err := l.Log(Event{Schedule: "x"}); err == nil {
		t.Fatal("expected error for missing Kind")
	}
}

func TestNilLoggerIsNoOp(t *testing.T) {
	var l *Logger // zero value
	if err := l.Log(Event{Kind: "x"}); err != nil {
		t.Fatalf("nil Log should be no-op, got: %v", err)
	}

	if err := l.Close(); err != nil {
		t.Fatalf("nil Close should be no-op, got: %v", err)
	}
}

func TestNewLoggerEmptyPathReturnsNil(t *testing.T) {
	l, err := NewLogger("")
	if err != nil {
		t.Fatalf("NewLogger(\"\"): %v", err)
	}

	if l != nil {
		t.Fatalf("expected nil, got %+v", l)
	}
}

func TestConcurrentLogs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	l, _ := NewLogger(path)

	t.Cleanup(func() { _ = l.Close() })

	var wg sync.WaitGroup

	const n = 100
	wg.Add(n)

	for i := range n {
		go func(i int) {
			defer wg.Done()

			_ = l.Log(Event{Kind: "scan", Entries: i, Timestamp: time.Now()})
		}(i)
	}

	wg.Wait()

	f, _ := os.Open(path) //nolint:gosec // path is test-controlled
	defer func() { _ = f.Close() }()

	count := 0

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("line %d invalid json: %v", count, err)
		}

		count++
	}

	if count != n {
		t.Fatalf("got %d lines want %d (mutex broken?)", count, n)
	}
}
