package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAddInvalidSpec(t *testing.T) {
	s := New(nil)
	err := s.Add(Job{Name: "bad", Spec: "not a cron spec", Run: func(context.Context) error { return nil }})
	if err == nil {
		t.Fatal("expected error for invalid cron spec")
	}
}

func TestAddMissingName(t *testing.T) {
	s := New(nil)
	if err := s.Add(Job{Spec: "@every 1s", Run: func(context.Context) error { return nil }}); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestAddMissingRun(t *testing.T) {
	s := New(nil)
	if err := s.Add(Job{Name: "x", Spec: "@every 1s"}); err == nil {
		t.Fatal("expected error for missing Run")
	}
}

// TestJobFires uses the @every descriptor with a sub-second interval so we
// can verify the wiring without sleeping for minutes.
func TestJobFires(t *testing.T) {
	var fired int32
	var wg sync.WaitGroup
	wg.Add(1)
	once := sync.Once{}

	s := New(nil)
	if err := s.Add(Job{
		Name: "tick",
		Spec: "@every 200ms",
		Run: func(context.Context) error {
			if atomic.AddInt32(&fired, 1) == 1 {
				once.Do(wg.Done)
			}
			return nil
		},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	s.Start()
	defer s.Stop(context.Background())

	waitCh := make(chan struct{})
	go func() { wg.Wait(); close(waitCh) }()
	select {
	case <-waitCh:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not fire within 2s")
	}
	if atomic.LoadInt32(&fired) < 1 {
		t.Fatalf("fired=%d, want >= 1", atomic.LoadInt32(&fired))
	}
}
