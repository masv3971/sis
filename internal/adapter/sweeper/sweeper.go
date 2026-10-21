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
	catalog *readmodel.Client
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
	s := &Service{catalog: catalog, timeout: cfg.SIS.Adapter.Sweeper.Timeout, cancel: cancel, done: make(chan struct{}), log: log.New("sweeper")}
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

// Sweep runs one pass over every entity type and returns the number of
// records deleted. Failures are logged and the pass continues with the next
// type; the next tick retries.
func (s *Service) Sweep(ctx context.Context) int {
	catalog := s.catalog
	// Dependants first, so a pass never leaves a reference to a record it
	// just deleted while the referrer survives until the next tick. (Records
	// populated together share an expiry, so they normally go together.)
	sweeps := []sweepFunc{
		{"result", catalog.Results.DeleteExpired},
		{"enrollment", catalog.Enrollments.DeleteExpired},
		{"person", catalog.Persons.DeleteExpiredPersons},
		{"offering", catalog.Offerings.DeleteExpired},
		{"program", catalog.Programs.DeleteExpired},
		{"course", catalog.Courses.DeleteExpired},
		{"academicSession", catalog.AcademicSessions.DeleteExpired},
		{"organization", catalog.Organizations.DeleteExpired},
	}
	total := 0
	for _, sweep := range sweeps {
		if err := ctx.Err(); err != nil {
			// Close cancels silently; running out of time is worth a note.
			if errors.Is(err, context.DeadlineExceeded) {
				s.log.Warn("sweep timed out; the next one continues", "next_type", sweep.typ)
			}
			break
		}
		ids, err := sweep.run(ctx)
		total += len(ids)
		if err != nil {
			s.log.Error("sweep failed", "type", sweep.typ, "err", err)
			continue
		}
		if len(ids) > 0 {
			s.log.Info("expired records deleted", "type", sweep.typ, "count", len(ids))
		}
	}
	return total
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
