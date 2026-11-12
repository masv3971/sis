package amqpapi

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/Azure/go-amqp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

func TestCommandSpanJoinsPublishersTrace(t *testing.T) {
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	const parentID = "00f067aa0ba902b7"
	spans := tracetest.NewSpanRecorder()
	tel, err := buildTelemetry(metric.NewMeterProvider().Meter("test"))
	require.NoError(t, err)
	tel.tracer = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("test")
	tel.propagator = propagation.TraceContext{}

	cases := map[string]struct {
		props      map[string]any
		wantParent bool
	}{
		"string traceparent": {map[string]any{"traceparent": "00-" + traceID + "-" + parentID + "-01"}, true},
		"binary traceparent": {map[string]any{"traceparent": []byte("00-" + traceID + "-" + parentID + "-01")}, true},
		"no traceparent":     {nil, false},
		"malformed":          {map[string]any{"traceparent": "not-a-trace"}, false},
		"not text":           {map[string]any{"traceparent": 42}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			before := len(spans.Ended())
			_, end := tel.begin(t.Context(), "person.create", &amqp.Message{ApplicationProperties: tc.props})
			end("accepted", nil)
			ended := spans.Ended()
			require.Len(t, ended, before+1)
			span := ended[before]
			if tc.wantParent {
				assert.Equal(t, traceID, span.SpanContext().TraceID().String(), "trace id")
				assert.Equal(t, parentID, span.Parent().SpanID().String(), "parent span id")
				return
			}
			assert.False(t, span.Parent().IsValid(), "a root span, not a child")
		})
	}
}

func TestFailedSettlementIsReportedAsSuch(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	reader := metric.NewManualReader()
	tel, err := buildTelemetry(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	tel.tracer = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("test")

	fake := newFakeLink()
	fake.acceptErr <- context.DeadlineExceeded
	svc := &Service{
		commands: map[string]handlerFunc{"person.create": func(context.Context, []byte) error { return nil }},
		cfg:      model.AMQPAPI{MaxMessageBytes: 1 << 20, Scopes: []string{auth.ScopeWrite}},
		tel:      tel,
		log:      logger.NewWithWriter("test", false, "error", io.Discard),
	}
	err = svc.process(t.Context(), fake, message("person.create", "", `{}`))
	require.ErrorIs(t, err, context.DeadlineExceeded, "process reports the failed settlement")

	ended := spans.Ended()
	require.Len(t, ended, 1, "spans")
	assert.Equal(t, codes.Error, ended[0].Status().Code, "a failed settlement fails the span")

	var got metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &got))
	var outcomes []string
	for _, scope := range got.ScopeMetrics {
		for _, m := range scope.Metrics {
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok && m.Name == "sis.amqp.commands" {
				for _, point := range sum.DataPoints {
					outcome, _ := point.Attributes.Value("outcome")
					outcomes = append(outcomes, outcome.AsString())
				}
			}
		}
	}
	assert.Equal(t, []string{"settle_failed"}, outcomes, "sis.amqp.commands outcomes")
}

func TestTelemetryRecordsSpanAndMetrics(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	reader := metric.NewManualReader()
	tel, err := buildTelemetry(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	tel.tracer = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("test")

	for _, run := range []struct {
		outcome string
		err     error
	}{{"accepted", nil}, {"rejected", errors.New("bad body")}, {"retry", errors.New("db down")}} {
		_, end := tel.begin(t.Context(), "person.create", &amqp.Message{})
		end(run.outcome, run.err)
	}

	ended := spans.Ended()
	require.Len(t, ended, 3, "spans")
	assert.Equal(t, "amqp person.create", ended[0].Name())
	assert.Equal(t, codes.Unset, ended[0].Status().Code, "accepted")
	assert.Equal(t, codes.Unset, ended[1].Status().Code, "a rejection is the sender's error, not ours")
	assert.Equal(t, codes.Error, ended[2].Status().Code, "retry")

	var got metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &got))
	counts := map[string]int64{}
	for _, scope := range got.ScopeMetrics {
		for _, m := range scope.Metrics {
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok && m.Name == "sis.amqp.commands" {
				for _, point := range sum.DataPoints {
					outcome, _ := point.Attributes.Value("outcome")
					counts[outcome.AsString()] = point.Value
				}
			}
		}
	}
	assert.Equal(t, map[string]int64{"accepted": 1, "rejected": 1, "retry": 1}, counts, "sis.amqp.commands by outcome")
}
