package metric_test

import (
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
	"github.com/SUNET/sis/pkg/metric"
	"github.com/SUNET/sis/pkg/model"
)

func quietLog() *logger.Log { return logger.NewWithWriter("test", false, "error", io.Discard) }

func TestDisabledLeavesGlobalsAlone(t *testing.T) {
	before := otel.GetMeterProvider()
	meter, err := metric.New(t.Context(), &model.Cfg{}, "sis", quietLog())
	require.NoError(t, err)
	assert.Same(t, before, otel.GetMeterProvider(), "global meter provider")
	assert.NoError(t, meter.Close(t.Context()))
}

func TestEnabledExportsMetricsOnClose(t *testing.T) {
	var posts atomic.Int32
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/metrics" {
			posts.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()

	previous := otel.GetMeterProvider()
	t.Cleanup(func() { otel.SetMeterProvider(previous) })

	cfg := &model.Cfg{Common: model.Common{Metrics: model.Metrics{Enabled: true, Endpoint: collector.URL, Interval: time.Hour, Timeout: 5 * time.Second}}}
	meter, err := metric.New(t.Context(), cfg, "sis", quietLog())
	require.NoError(t, err)
	counter, err := otel.Meter("test").Int64Counter("test.count")
	require.NoError(t, err)
	counter.Add(t.Context(), 1)
	require.NoError(t, meter.Close(t.Context()))

	assert.Equal(t, int32(1), posts.Load(), "export requests to the collector")
}
