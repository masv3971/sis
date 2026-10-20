package sweeper

import (
	"io"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

func TestSweepDeletesExpired(t *testing.T) {
	ctx := t.Context()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	c := &readmodel.Client{
		Persons:          service.New(memory.NewPersonRepository().WithClock(clock), nil, clock, log),
		Organizations:    service.NewResource(memory.NewDocRepo[domain.Organization]().WithClock(clock), nil, clock, log),
		AcademicSessions: service.NewResource(memory.NewDocRepo[domain.AcademicSession]().WithClock(clock), nil, clock, log),
		Courses:          service.NewResource(memory.NewDocRepo[domain.Course]().WithClock(clock), nil, clock, log),
		Offerings:        service.NewResource(memory.NewDocRepo[domain.Offering]().WithClock(clock), nil, clock, log),
		Enrollments:      service.NewResource(memory.NewDocRepo[domain.Enrollment]().WithClock(clock), nil, clock, log),
		Programs:         service.NewResource(memory.NewDocRepo[domain.Program]().WithClock(clock), nil, clock, log),
		Results:          service.NewResource(memory.NewDocRepo[domain.Result]().WithClock(clock), nil, clock, log),
	}
	exp := now.Add(time.Minute)
	_, err := c.Populate(ctx, &domain.Bundle{
		Persons: []domain.Person{{SourcedID: "p", Status: domain.StatusActive, GivenName: "A", FamilyName: "B"}},
		Courses: []*domain.Course{{SourcedID: "c", Title: "C"}},
		Results: []*domain.Result{{SourcedID: "r", PersonID: "p", CourseID: "c"}},
	}, &exp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Organizations.Create(ctx, &domain.Organization{Name: "permanent"}); err != nil {
		t.Fatal(err)
	}

	cfg := &model.Cfg{}
	cfg.SIS.Adapter.Sweeper.Interval = time.Hour
	s, err := New(ctx, cfg, c, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(t.Context()) })

	if n := s.Sweep(ctx); n != 0 {
		t.Fatalf("nothing expired yet, swept %d", n)
	}
	now = exp
	if n := s.Sweep(ctx); n != 3 {
		t.Fatalf("swept %d, want 3", n)
	}
	if n := s.Sweep(ctx); n != 0 {
		t.Fatalf("second sweep found %d", n)
	}
}

func TestNewRejectsZeroInterval(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	if _, err := New(t.Context(), &model.Cfg{}, &readmodel.Client{}, log); err == nil {
		t.Fatal("zero interval must be rejected")
	}
}

func TestCloseStopsLoop(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	cfg := &model.Cfg{}
	cfg.SIS.Adapter.Sweeper.Interval = time.Hour
	s, err := New(t.Context(), cfg, &readmodel.Client{}, log)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.done:
	default:
		t.Fatal("loop still running after Close")
	}
}
