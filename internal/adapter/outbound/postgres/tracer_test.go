package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// queryEndStatus ends a query span with err through tracer and returns the
// span's status. TraceQueryEnd ignores its connection and reads only the span
// in ctx, so no database is needed.
func queryEndStatus(t *testing.T, newTracer func() pgx.QueryTracer, err error) codes.Code {
	t.Helper()
	spans := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	tracer := newTracer()
	ctx, span := provider.Tracer("test").Start(t.Context(), "query")
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: err})
	if span.IsRecording() {
		span.End() // otelpgx only ends a span it considers its own
	}
	ended := spans.Ended()
	require.Len(t, ended, 1)
	return ended[0].Status().Code
}

func TestQueryTracerSpansOfClientErrors(t *testing.T) {
	uniqueErr := &pgconn.PgError{Code: uniqueViolation}
	cases := map[string]struct {
		err  error
		want codes.Code
	}{
		"success":          {nil, codes.Unset},
		"unique violation": {uniqueErr, codes.Unset},
		"wrapped unique":   {fmt.Errorf("insert: %w", uniqueErr), codes.Unset},
		"other pg error":   {&pgconn.PgError{Code: "40001"}, codes.Error},
		"plain error":      {errors.New("connection reset"), codes.Error},
		"timeout":          {context.DeadlineExceeded, codes.Error},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := queryEndStatus(t, func() pgx.QueryTracer { return newQueryTracer() }, tc.err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// The wrapper exists because otelpgx alone fails the span on a unique
// violation. If this stops holding, drop queryTracer.
func TestOtelpgxAloneFailsTheSpanOnUniqueViolation(t *testing.T) {
	got := queryEndStatus(t, func() pgx.QueryTracer { return otelpgx.NewTracer() }, &pgconn.PgError{Code: uniqueViolation})
	assert.Equal(t, codes.Error, got)
}
