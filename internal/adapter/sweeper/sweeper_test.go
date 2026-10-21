package sweeper

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// newCatalog is an in-memory catalog on a shared test clock.
func newCatalog(clock func() time.Time, log *logger.Log) *readmodel.Client {
	return &readmodel.Client{
		Persons:          service.New(memory.NewPersonRepository().WithClock(clock), nil, clock, log),
		Organizations:    service.NewResource(memory.NewDocRepo[domain.Organization]().WithClock(clock), nil, clock, log),
		AcademicSessions: service.NewResource(memory.NewDocRepo[domain.AcademicSession]().WithClock(clock), nil, clock, log),
		Courses:          service.NewResource(memory.NewDocRepo[domain.Course]().WithClock(clock), nil, clock, log),
		Offerings:        service.NewResource(memory.NewDocRepo[domain.Offering]().WithClock(clock), nil, clock, log),
		Enrollments:      service.NewResource(memory.NewDocRepo[domain.Enrollment]().WithClock(clock), nil, clock, log),
		Programs:         service.NewResource(memory.NewDocRepo[domain.Program]().WithClock(clock), nil, clock, log),
		Results:          service.NewResource(memory.NewDocRepo[domain.Result]().WithClock(clock), nil, clock, log),
	}
}

func TestSweepDeletesExpired(t *testing.T) {
	ctx := t.Context()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	catalog := newCatalog(func() time.Time { return now }, log)
	expiry := now.Add(time.Minute)
	_, err := catalog.Populate(ctx, &domain.Bundle{
		Persons: []domain.Person{{SourcedID: "p", Status: domain.StatusActive, GivenName: "A", FamilyName: "B"}},
		Courses: []*domain.Course{{SourcedID: "c", Title: "C"}},
		Results: []*domain.Result{{SourcedID: "r", PersonID: "p", CourseID: "c"}},
	}, &expiry)
	require.NoError(t, err)
	_, err = catalog.Organizations.Create(ctx, &domain.Organization{Name: "permanent"})
	require.NoError(t, err)

	cfg := &model.Cfg{}
	cfg.SIS.Adapter.Sweeper.Interval = time.Hour
	s, err := New(ctx, cfg, catalog, log)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(t.Context()) })

	assert.Zero(t, s.Sweep(ctx), "nothing expired yet")
	now = expiry
	assert.Equal(t, 3, s.Sweep(ctx))
	assert.Zero(t, s.Sweep(ctx), "second sweep")
}

// TestSweepBoundedStopsAtTimeout: a pass that runs out of time stops before
// the next type instead of running on.
func TestSweepBoundedStopsAtTimeout(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	catalog := newCatalog(func() time.Time { return now }, log)
	expiry := now.Add(time.Minute)
	_, err := catalog.Populate(t.Context(), &domain.Bundle{
		Courses: []*domain.Course{{SourcedID: "c", Title: "C"}},
	}, &expiry)
	require.NoError(t, err)
	now = expiry

	// The deadline has passed before the first type: nothing is swept.
	s := &Service{catalog: catalog, timeout: time.Nanosecond, log: log}
	assert.Zero(t, s.sweepBounded(t.Context()))
	// Without a limit the same pass deletes it.
	s.timeout = 0
	assert.Equal(t, 1, s.sweepBounded(t.Context()))
}

func TestNewRejectsZeroInterval(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	_, err := New(t.Context(), &model.Cfg{}, &readmodel.Client{}, log)
	assert.Error(t, err, "zero interval must be rejected")
}

func TestCloseStopsLoop(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	cfg := &model.Cfg{}
	cfg.SIS.Adapter.Sweeper.Interval = time.Hour
	s, err := New(t.Context(), cfg, &readmodel.Client{}, log)
	require.NoError(t, err)
	require.NoError(t, s.Close(t.Context()))
	select {
	case <-s.done:
	default:
		t.Fatal("loop still running after Close")
	}
}
