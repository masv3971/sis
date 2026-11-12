// Package trace sets up OpenTelemetry tracing: an OTLP/HTTP exporter behind
// the global tracer provider, so instrumentation (otelgin, otelpgx, amqpapi)
// finds it through otel.Tracer. Disabled, the global stays the no-op provider.
// Inspired by SUNET/vc's pkg/trace.
package trace

import (
	"context"
	"fmt"
	"net/url"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/SUNET/sis/pkg/helpers"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// Tracer owns the SDK tracer provider. It is a Service: Close flushes the
// spans still buffered.
type Tracer struct {
	// provider is nil when tracing is disabled.
	provider *sdktrace.TracerProvider
	log      *logger.Log
}

// New starts exporting to common.tracing.endpoint and installs the provider
// and the W3C trace-context propagator as the OpenTelemetry globals. The
// exporter connects lazily, so an unreachable collector does not fail New.
func New(ctx context.Context, cfg *model.Cfg, serviceName string, log *logger.Log) (*Tracer, error) {
	t := &Tracer{log: log.New("trace")}
	tracing := cfg.Common.Tracing
	if !tracing.Enabled {
		return t, nil
	}
	// WithEndpointURL takes the full URL; a bare base URL would post to "/".
	endpoint, err := url.JoinPath(tracing.Endpoint, "v1/traces")
	if err != nil {
		return nil, fmt.Errorf("common.tracing.endpoint: %w", err)
	}
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpointURL(endpoint),
		otlptracehttp.WithTimeout(tracing.Timeout),
	}
	if cfg.Common.Production {
		// The default client follows a 307 or 308 to http and replays the spans there.
		opts = append(opts, otlptracehttp.WithHTTPClient(helpers.HTTPSOnlyClient(tracing.Timeout)))
	}
	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	t.provider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", serviceName))),
	)
	otel.SetTracerProvider(t.provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.log.Info("tracing enabled", "endpoint", tracing.Endpoint)
	return t, nil
}

// Close flushes buffered spans and stops the exporter, within ctx.
func (t *Tracer) Close(ctx context.Context) error {
	if t.provider == nil {
		return nil
	}
	t.log.Info("stopping tracer")
	return t.provider.Shutdown(ctx)
}
