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
	s := &Service{catalog: catalog, cancel: cancel, done: make(chan struct{}), log: log.New("sweeper")}
	go s.loop(loopCtx, interval)
	s.log.Info("sweeper started", "interval", interval.String())
	return s, nil
}

func (s *Service) loop(ctx context.Context, interval time.Duration) {
	defer close(s.done)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sweep(ctx)
		}
	}
}

// Sweep runs one pass over every entity type and returns the number of
// records deleted. Failures are logged and the pass continues with the next
// type; the next tick retries.
func (s *Service) Sweep(ctx context.Context) int {
	c := s.catalog
	// Dependants first, so a pass never leaves a reference to a record it
	// just deleted while the referrer survives until the next tick. (Records
	// populated together share an expiry, so they normally go together.)
	funcs := []sweepFunc{
		{"result", c.Results.DeleteExpired},
		{"enrollment", c.Enrollments.DeleteExpired},
		{"person", c.Persons.DeleteExpiredPersons},
		{"offering", c.Offerings.DeleteExpired},
		{"program", c.Programs.DeleteExpired},
		{"course", c.Courses.DeleteExpired},
		{"academicSession", c.AcademicSessions.DeleteExpired},
		{"organization", c.Organizations.DeleteExpired},
	}
	total := 0
	for _, f := range funcs {
		if ctx.Err() != nil {
			break
		}
		ids, err := f.run(ctx)
		total += len(ids)
		if err != nil {
			s.log.Error("sweep failed", "type", f.typ, "err", err)
			continue
		}
		if len(ids) > 0 {
			s.log.Info("expired records deleted", "type", f.typ, "count", len(ids))
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
