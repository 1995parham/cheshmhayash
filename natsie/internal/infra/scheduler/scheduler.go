// Package scheduler wraps robfig/cron/v3 with the natsie job shape.
//
// A Job carries the name (for logs), the cron spec, and a Run function
// that takes a context. The wrapper is a thin layer: cron spec parsing,
// concurrency control, and shutdown semantics all delegate to upstream.
package scheduler

import (
	"context"
	"errors"
	"fmt"

	"github.com/robfig/cron/v3"
)

// Job is one scheduled task. Run is called every time the cron Spec fires;
// errors are logged via the Scheduler's logger but do not cancel the
// schedule.
type Job struct {
	Name string
	Spec string
	Run  func(context.Context) error
}

// Logger is the minimal interface the scheduler uses to log job
// outcomes. Pass nil to silence.
type Logger interface {
	Printf(format string, args ...any)
}

// Scheduler runs Jobs on a shared cron clock.
type Scheduler struct {
	cron *cron.Cron
	log  Logger
}

// New constructs a Scheduler. Cron specs use the standard 5-field syntax
// (minute hour dom month dow); descriptors like @daily are also accepted.
func New(log Logger) *Scheduler {
	return &Scheduler{
		cron: cron.New(),
		log:  log,
	}
}

// Add registers a Job. The cron spec is validated immediately; an invalid
// spec returns an error and the Job is not added.
func (s *Scheduler) Add(j Job) error {
	if j.Name == "" {
		return errors.New("scheduler: job name is required")
	}
	if j.Run == nil {
		return errors.New("scheduler: job Run is required")
	}
	_, err := s.cron.AddFunc(j.Spec, s.wrap(j))
	if err != nil {
		return fmt.Errorf("schedule %s: %w", j.Name, err)
	}
	return nil
}

func (s *Scheduler) wrap(j Job) func() {
	return func() {
		ctx := context.Background()
		if err := j.Run(ctx); err != nil && s.log != nil {
			s.log.Printf("job %s: %v", j.Name, err)
		}
	}
}

// Start begins ticking. Non-blocking; jobs run in their own goroutines.
func (s *Scheduler) Start() { s.cron.Start() }

// Stop blocks until all in-flight jobs finish, or ctx is canceled.
func (s *Scheduler) Stop(ctx context.Context) {
	stopped := s.cron.Stop().Done()
	select {
	case <-stopped:
	case <-ctx.Done():
	}
}
