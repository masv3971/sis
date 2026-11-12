package sweeper

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
	"github.com/SUNET/sis/pkg/trace"
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
		Entitlements:     service.NewResource(memory.NewDocRepo[domain.Entitlement]().WithClock(clock), nil, clock, log),

		Affiliations:        service.NewResource(memory.NewDocRepo[domain.Affiliation]().WithClock(clock), nil, clock, log),
		CollectionOfferings: service.NewResource(memory.NewDocRepo[domain.CollectionOffering]().WithClock(clock), nil, clock, log),
		ComponentTemplates:  service.NewResource(memory.NewDocRepo[domain.ComponentTemplate]().WithClock(clock), nil, clock, log),
		ComponentOfferings:  service.NewResource(memory.NewDocRepo[domain.ComponentOffering]().WithClock(clock), nil, clock, log),
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

// TestSweepDeletesNewEntities: every new type is swept too, so a populated
// bundle with a ttl leaves nothing behind.
func TestSweepDeletesNewEntities(t *testing.T) {
	ctx := t.Context()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	catalog := newCatalog(func() time.Time { return now }, log)
	expiry := now.Add(time.Minute)
	_, err := catalog.Populate(ctx, &domain.Bundle{
		Persons:             []domain.Person{{SourcedID: "p", Status: domain.StatusActive, GivenName: "A", FamilyName: "B"}},
		Organizations:       []*domain.Organization{{SourcedID: "o", Name: "O"}},
		Programs:            []*domain.Program{{SourcedID: "g", Title: "G"}},
		Courses:             []*domain.Course{{SourcedID: "c", Title: "C", ProgramIDs: []string{"g"}}},
		Offerings:           []*domain.Offering{{SourcedID: "f", CourseID: "c", CollectionOfferingIDs: []string{"k"}}},
		CollectionOfferings: []*domain.CollectionOffering{{SourcedID: "k", ProgramID: "g"}},
		ComponentTemplates:  []*domain.ComponentTemplate{{SourcedID: "t", Title: "T", CourseIDs: []string{"c"}}},
		ComponentOfferings:  []*domain.ComponentOffering{{SourcedID: "to", ComponentID: "t", CourseOfferingIDs: []string{"f"}}},
		Affiliations:        []*domain.Affiliation{{SourcedID: "a", PersonID: "p", OrgID: "o"}},
	}, &expiry)
	require.NoError(t, err)

	cfg := &model.Cfg{}
	cfg.SIS.Adapter.Sweeper.Interval = time.Hour
	s, err := New(ctx, cfg, catalog, log)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(t.Context()) })

	now = expiry
	assert.Equal(t, 9, s.Sweep(ctx), "every record of every type")
	assert.Zero(t, s.Sweep(ctx))
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
	s := &Service{sweeps: sweepsOf(catalog), timeout: time.Nanosecond, log: log}
	assert.Zero(t, s.sweepBounded(t.Context()))
	// Without a limit the same pass deletes it.
	s.timeout = 0
	assert.Equal(t, 1, s.sweepBounded(t.Context()))
}

// TestSweepRunsUnderRootSpan: the SQL of a pass needs a recording parent to
// be traced (otelpgx), and the loop has none of its own.
func TestSweepRunsUnderRootSpan(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	var recording []bool
	s := &Service{
		sweeps: []sweepFunc{{"person", func(ctx context.Context) ([]string, error) {
			recording = append(recording, oteltrace.SpanFromContext(ctx).IsRecording())
			return nil, nil
		}}},
		log: logger.NewWithWriter("test", false, "error", io.Discard),
	}
	s.sweepBounded(t.Context())

	assert.Equal(t, []bool{true}, recording, "a sweep runs under a recording span")
	ended := spans.Ended()
	require.Len(t, ended, 1, "spans")
	assert.Equal(t, "sweeper sweep", ended[0].Name())
	assert.False(t, ended[0].Parent().IsValid(), "a root span")
}

// TestSweepDoesNotInheritTheStartupSpan: New is given the startup span's
// context, and the loop outlives it. A pass must not hang under that span.
func TestSweepDoesNotInheritTheStartupSpan(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	startupCtx, endStartup := trace.Start(t.Context(), "startup")
	defer endStartup(nil)

	cfg := &model.Cfg{}
	cfg.SIS.Adapter.Sweeper.Interval = 10 * time.Millisecond
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	s, err := New(startupCtx, cfg, newCatalog(time.Now, log), log)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	require.Eventually(t, func() bool {
		for _, span := range spans.Ended() {
			if span.Name() == "sweeper sweep" {
				return true
			}
		}
		return false
	}, 5*time.Second, 5*time.Millisecond, "no sweep span")
	for _, span := range spans.Ended() {
		if span.Name() == "sweeper sweep" {
			assert.False(t, span.Parent().IsValid(), "a sweep is a root span, not a child of startup")
		}
	}
}

// TestSweepTimeoutRotatesStart: a type that always uses up the pass timeout
// does not starve the types after it. The next pass starts after it, and a
// full pass returns to the dependants-first order.
func TestSweepTimeoutRotatesStart(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	var ran []string
	sweep := func(typ string, slow bool) sweepFunc {
		return sweepFunc{typ, func(ctx context.Context) ([]string, error) {
			ran = append(ran, typ)
			if slow {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return []string{typ}, nil
		}}
	}
	s := &Service{
		sweeps:  []sweepFunc{sweep("result", true), sweep("enrollment", false), sweep("person", false)},
		timeout: 50 * time.Millisecond,
		log:     log,
	}

	// result times out; nothing after it runs this pass.
	assert.Zero(t, s.sweepBounded(t.Context()))
	assert.Equal(t, []string{"result"}, ran)

	// The later types get their turn first, then result times out again.
	ran = nil
	assert.Equal(t, 2, s.sweepBounded(t.Context()))
	assert.Equal(t, []string{"enrollment", "person", "result"}, ran)

	// Without the slow type a full pass resets to dependants first.
	s.sweeps[0] = sweep("result", false)
	ran = nil
	assert.Equal(t, 3, s.sweepBounded(t.Context()))
	assert.Equal(t, []string{"enrollment", "person", "result"}, ran)
	ran = nil
	assert.Equal(t, 3, s.sweepBounded(t.Context()))
	assert.Equal(t, []string{"result", "enrollment", "person"}, ran)
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
