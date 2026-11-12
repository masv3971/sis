package trace_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/pkg/trace"
)

func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	spans := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	return spans
}

func TestStartMarksOnlyServerFailuresAsErrors(t *testing.T) {
	cases := map[string]struct {
		err  error
		want codes.Code
	}{
		"success":       {nil, codes.Unset},
		"not found":     {domain.ErrNotFound, codes.Unset},
		"conflict":      {fmt.Errorf("create: %w", domain.ErrConflict), codes.Unset},
		"validation":    {&domain.ValidationError{Field: "x", Msg: "bad"}, codes.Unset},
		"internal":      {errors.New("connection reset"), codes.Error},
		"timeout":       {context.DeadlineExceeded, codes.Error},
		"wrapped crash": {fmt.Errorf("store: %w", errors.New("disk full")), codes.Error},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			spans := recordSpans(t)
			_, end := trace.Start(t.Context(), "work", attribute.String("k", "v"))
			end(tc.err)
			ended := spans.Ended()
			require.Len(t, ended, 1)
			assert.Equal(t, tc.want, ended[0].Status().Code)
		})
	}
}

func TestStartNestsUnderParent(t *testing.T) {
	spans := recordSpans(t)
	ctx, endOuter := trace.Start(t.Context(), "outer")
	_, endInner := trace.Start(ctx, "inner")
	endInner(nil)
	endOuter(nil)
	ended := spans.Ended()
	require.Len(t, ended, 2)
	assert.Equal(t, ended[1].SpanContext().SpanID(), ended[0].Parent().SpanID(), "inner's parent is outer")
}
