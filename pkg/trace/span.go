package trace

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/SUNET/sis/pkg/helpers"
)

const instrumentationName = "github.com/SUNET/sis"

// Start begins a span under the global tracer provider, a no-op unless
// common.tracing is enabled. Call the returned func once with the operation's
// error. Only a server-side failure (HTTP status 500 or above, as
// helpers.NewErrorFromError classifies it) marks the span failed: a missing
// record or a bad request is the caller's doing, not the service's.
func Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, func(err error)) {
	ctx, span := otel.Tracer(instrumentationName).Start(ctx, name, oteltrace.WithAttributes(attrs...))
	return ctx, func(err error) {
		if err != nil && helpers.NewErrorFromError(err).Status() >= http.StatusInternalServerError {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed")
		}
		span.End()
	}
}
