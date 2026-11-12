package trace_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
	"github.com/SUNET/sis/pkg/trace"
)

func quietLog() *logger.Log { return logger.NewWithWriter("test", false, "error", io.Discard) }

func TestDisabledLeavesGlobalsAlone(t *testing.T) {
	before := otel.GetTracerProvider()
	tracer, err := trace.New(t.Context(), &model.Cfg{}, "sis", quietLog())
	require.NoError(t, err)
	assert.Same(t, before, otel.GetTracerProvider(), "global tracer provider")
	assert.NoError(t, tracer.Close(t.Context()))
}

// TestProductionExporterRefusesPlaintextRedirect: the collector's endpoint is
// checked for https at startup, but its redirects are not, and a 307 replays
// the spans at the new address. (Both servers are http here: the first request
// is the operator's choice, the redirect is what is refused.)
func TestProductionExporterRefusesPlaintextRedirect(t *testing.T) {
	for _, tc := range []struct {
		production bool
		wantLeaked int32
	}{{false, 1}, {true, 0}} {
		t.Run(fmt.Sprintf("production=%v", tc.production), func(t *testing.T) {
			var leaked atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked.Add(1) }))
			defer target.Close()
			collector := httptest.NewServer(http.RedirectHandler(target.URL+"/v1/traces", http.StatusTemporaryRedirect))
			defer collector.Close()

			previous := otel.GetTracerProvider()
			t.Cleanup(func() { otel.SetTracerProvider(previous) })
			otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {}))

			cfg := &model.Cfg{Common: model.Common{
				Production: tc.production,
				Tracing:    model.Tracing{Enabled: true, Endpoint: collector.URL, Timeout: 5 * time.Second},
			}}
			tracer, err := trace.New(t.Context(), cfg, "sis", quietLog())
			require.NoError(t, err)
			_, span := otel.Tracer("test").Start(t.Context(), "work")
			span.End()
			_ = tracer.Close(t.Context())

			assert.Equal(t, tc.wantLeaked, leaked.Load(), "requests that reached the redirect target")
		})
	}
}

func TestEnabledExportsSpansOnClose(t *testing.T) {
	var posts atomic.Int32
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/traces" {
			posts.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()

	previous := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	cfg := &model.Cfg{Common: model.Common{Tracing: model.Tracing{Enabled: true, Endpoint: collector.URL, Timeout: 5 * time.Second}}}
	tracer, err := trace.New(t.Context(), cfg, "sis", quietLog())
	require.NoError(t, err)
	_, span := otel.Tracer("test").Start(t.Context(), "work")
	span.End()
	require.NoError(t, tracer.Close(t.Context()))

	assert.Equal(t, int32(1), posts.Load(), "export requests to the collector")
}
