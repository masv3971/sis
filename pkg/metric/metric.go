// Package metric sets up OpenTelemetry metrics: an OTLP/HTTP exporter behind
// the global meter provider, so instrumentation (otelgin, otelpgx, amqpapi)
// finds it through otel.Meter. Disabled, the global stays the no-op provider.
// Inspired by SUNET/vc's pkg/metric.
package metric

import (
	"context"
	"fmt"
	"net/url"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// Meter owns the SDK meter provider. It is a Service: Close exports what is
// left.
type Meter struct {
	// provider is nil when metrics are disabled.
	provider *sdkmetric.MeterProvider
	log      *logger.Log
}

// New starts exporting to common.metrics.endpoint every common.metrics.interval
// and installs the provider as the OpenTelemetry global. The exporter connects
// lazily, so an unreachable collector does not fail New.
func New(ctx context.Context, cfg *model.Cfg, serviceName string, log *logger.Log) (*Meter, error) {
	m := &Meter{log: log.New("metric")}
	metrics := cfg.Common.Metrics
	if !metrics.Enabled {
		return m, nil
	}
	// WithEndpointURL takes the full URL; a bare base URL would post to "/".
	endpoint, err := url.JoinPath(metrics.Endpoint, "v1/metrics")
	if err != nil {
		return nil, fmt.Errorf("common.metrics.endpoint: %w", err)
	}
	exporter, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpointURL(endpoint),
		otlpmetrichttp.WithTimeout(metrics.Timeout),
	)
	if err != nil {
		return nil, err
	}
	m.provider = sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter,
			sdkmetric.WithInterval(metrics.Interval),
			sdkmetric.WithTimeout(metrics.Timeout),
		)),
		sdkmetric.WithResource(resource.NewSchemaless(attribute.String("service.name", serviceName))),
	)
	otel.SetMeterProvider(m.provider)
	m.log.Info("metrics enabled", "endpoint", metrics.Endpoint, "interval", metrics.Interval.String())
	return m, nil
}

// Close exports the remaining metrics and stops the exporter, within ctx.
func (m *Meter) Close(ctx context.Context) error {
	if m.provider == nil {
		return nil
	}
	m.log.Info("stopping meter")
	return m.provider.Shutdown(ctx)
}
