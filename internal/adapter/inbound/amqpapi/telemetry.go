package amqpapi

import (
	"context"
	"time"

	"github.com/Azure/go-amqp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/SUNET/sis/pkg/logger"
)

const telemetryName = "github.com/SUNET/sis/internal/adapter/inbound/amqpapi"

// telemetry records one span and two metrics per command. The globals are
// no-ops unless common.tracing / common.metrics are enabled.
type telemetry struct {
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	commands   metric.Int64Counter
	duration   metric.Float64Histogram
}

// newTelemetry falls back to no-op instruments if the SDK refuses one, so
// telemetry never stops commands from running.
func newTelemetry(log *logger.Log) *telemetry {
	tel, err := buildTelemetry(otel.Meter(telemetryName))
	if err != nil {
		log.Warn("amqp metrics disabled", "err", err)
		tel, _ = buildTelemetry(noop.Meter{})
	}
	return tel
}

func buildTelemetry(meter metric.Meter) (*telemetry, error) {
	commands, err := meter.Int64Counter("sis.amqp.commands",
		metric.WithDescription("AMQP commands processed, by subject and outcome."))
	if err != nil {
		return nil, err
	}
	duration, err := meter.Float64Histogram("sis.amqp.command.duration",
		metric.WithUnit("s"), metric.WithDescription("Time to run and settle one AMQP command."))
	if err != nil {
		return nil, err
	}
	return &telemetry{tracer: otel.Tracer(telemetryName), propagator: otel.GetTextMapPropagator(), commands: commands, duration: duration}, nil
}

// traceCarrier reads the W3C trace-context properties (traceparent,
// tracestate) a publisher put in the message's application properties.
// Other properties, and values that are not text, are ignored.
func traceCarrier(msg *amqp.Message) propagation.MapCarrier {
	carrier := propagation.MapCarrier{}
	for _, key := range []string{"traceparent", "tracestate"} {
		if value := headerString(msg.ApplicationProperties[key]); value != "" {
			carrier[key] = value
		}
	}
	return carrier
}

// begin starts the command's span, a child of the publisher's span when the
// message carries a valid traceparent. The caller must call the returned func
// once, with the outcome, after the message is settled.
func (t *telemetry) begin(ctx context.Context, subject string, msg *amqp.Message) (context.Context, func(outcome string, err error)) {
	began := time.Now()
	ctx = t.propagator.Extract(ctx, traceCarrier(msg))
	ctx, span := t.tracer.Start(ctx, "amqp "+subject, trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attribute.String("messaging.system", "amqp"), attribute.String("messaging.operation.name", subject)))
	return ctx, func(outcome string, err error) {
		attrs := attribute.NewSet(attribute.String("subject", subject), attribute.String("outcome", outcome))
		t.commands.Add(ctx, 1, metric.WithAttributeSet(attrs))
		t.duration.Record(ctx, time.Since(began).Seconds(), metric.WithAttributeSet(attrs))
		span.SetAttributes(attribute.String("sis.outcome", outcome))
		if err != nil && outcome != "rejected" {
			span.RecordError(err)
			span.SetStatus(codes.Error, outcome)
		}
		span.End()
	}
}
