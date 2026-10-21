// Package sweeper deletes expired records (see domain.Base.ExpiresAt) on an
// interval. Expired records are already invisible to every API, so the
// sweeper only reclaims their storage and emits <type>.expired events.
//
// Service vs Client: this is a Service because it owns a background
// goroutine that must be stopped on shutdown, via Close.
package sweeper

import (
	"context"
	"errors"
	"time"

	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// Service runs the sweep loop.
type Service struct {
	// sweeps is one pass, dependants first (see sweepsOf).
	sweeps []sweepFunc
	// next is where the next pass starts: 0 (dependants first) unless the
	// last pass ran out of time. Only the loop goroutine touches it.
	next int
	// timeout bounds one pass of the loop; 0 means no limit.
	timeout time.Duration
	cancel  context.CancelFunc
	done    chan struct{}
	log     *logger.Log
}

// sweepFunc deletes one entity type's expired records, returning their ids.
type sweepFunc struct {
	typ string
	run func(context.Context) ([]string, error)
}

// New starts sweeping every sis.adapter.sweeper.interval. The loop outlives
// ctx's cancellation and stops only on Close.
func New(ctx context.Context, cfg *model.Cfg, catalog *readmodel.Client, log *logger.Log) (*Service, error) {
	interval := cfg.SIS.Adapter.Sweeper.Interval
	if interval <= 0 {
		return nil, errors.New("sweeper: sis.adapter.sweeper.interval must be positive")
	}
	loopCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s := &Service{sweeps: sweepsOf(catalog), timeout: cfg.SIS.Adapter.Sweeper.Timeout, cancel: cancel, done: make(chan struct{}), log: log.New("sweeper")}
	go s.loop(loopCtx, interval)
	s.log.Info("sweeper started", "interval", interval.String())
	return s, nil
}

func (s *Service) loop(ctx context.Context, interval time.Duration) {
	defer close(s.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepBounded(ctx)
		}
	}
}

// sweepBounded runs one Sweep within sis.adapter.sweeper.timeout, so a slow
// database cannot stall the loop (and Close) indefinitely. What is left is
// deleted by the next pass.
func (s *Service) sweepBounded(ctx context.Context) int {
	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}
	return s.Sweep(ctx)
}

// sweepsOf lists one pass over every entity type, dependants first, so a
// pass never leaves a reference to a record it just deleted while the
// referrer survives until the next tick. (Records populated together share
// an expiry, so they normally go together.)
func sweepsOf(catalog *readmodel.Client) []sweepFunc {
	return []sweepFunc{
		{"result", catalog.Results.DeleteExpired},
		{"enrollment", catalog.Enrollments.DeleteExpired},
		{"person", catalog.Persons.DeleteExpiredPersons},
		{"offering", catalog.Offerings.DeleteExpired},
		{"program", catalog.Programs.DeleteExpired},
		{"course", catalog.Courses.DeleteExpired},
		{"academicSession", catalog.AcademicSessions.DeleteExpired},
		{"organization", catalog.Organizations.DeleteExpired},
	}
}

// Sweep runs one pass over every entity type and returns the number of
// records deleted. Failures are logged and the pass continues with the next
// type; the next tick retries.
//
// A pass that runs out of time stops, and the next one starts at the type
// after the one it stopped in (wrapping around), so a type that always uses
// up the timeout cannot starve the types after it: they get their turn
// first and it goes last. A full pass returns to dependants first. A
// rotated pass may delete a record before its expired referrers; that is
// harmless, as expired records are already invisible to every API and
// readers drop dangling references.
//
// Not safe for concurrent use; the loop is its only caller.
func (s *Service) Sweep(ctx context.Context) int {
	total := 0
	start, count := s.next, len(s.sweeps)
	s.next = 0
	for step := range count {
		index := (start + step) % count
		sweep := s.sweeps[index]
		if ctx.Err() != nil {
			s.stopped(ctx, index)
			break
		}
		ids, err := sweep.run(ctx)
		total += len(ids)
		switch {
		case ctx.Err() != nil:
			// Out of time (or closed) in this type: it goes last next time.
			s.stopped(ctx, (index+1)%count)
			return total
		case err != nil:
			s.log.Error("sweep failed", "type", sweep.typ, "err", err)
		case len(ids) > 0:
			s.log.Info("expired records deleted", "type", sweep.typ, "count", len(ids))
		}
	}
	return total
}

// stopped records that a pass ended early and the next one starts at
// s.sweeps[next].
func (s *Service) stopped(ctx context.Context, next int) {
	s.next = next
	// Close cancels silently; running out of time is worth a note.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		s.log.Warn("sweep timed out; the next one continues", "next_type", s.sweeps[next].typ)
	}
}

// Close stops the loop, waiting for an in-progress sweep until ctx is done.
func (s *Service) Close(ctx context.Context) error {
	s.log.Info("stopping sweeper")
	s.cancel()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
