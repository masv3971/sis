package postgres

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// TestPoolConfigAppliesTimeouts: the configured timeouts override the
// connection string; zero keeps what it says.
func TestPoolConfigAppliesTimeouts(t *testing.T) {
	const url = "postgres://u:p@db:5432/sis?connect_timeout=3"

	poolCfg, err := poolConfig(model.Postgres{ConnectionString: url, ConnectTimeout: 10 * time.Second, StatementTimeout: 1500 * time.Millisecond})
	require.NoError(t, err)
	assert.Equal(t, 10*time.Second, poolCfg.ConnConfig.ConnectTimeout)
	assert.Equal(t, "1500", poolCfg.ConnConfig.RuntimeParams["statement_timeout"])

	poolCfg, err = poolConfig(model.Postgres{ConnectionString: url})
	require.NoError(t, err)
	assert.Equal(t, 3*time.Second, poolCfg.ConnConfig.ConnectTimeout, "the URL's connect_timeout")
	assert.NotContains(t, poolCfg.ConnConfig.RuntimeParams, "statement_timeout")
}

// TestStatementTimeoutReachesPool: after migrations (which lift the limit on
// their connection) every pooled session has the configured timeout.
func TestStatementTimeoutReachesPool(t *testing.T) {
	url := os.Getenv("SIS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SIS_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	cfg := &model.Cfg{}
	cfg.SIS.Adapter.Outbound.Postgres = model.Postgres{ConnectionString: url, StatementTimeout: 1234 * time.Millisecond}
	svc, err := New(t.Context(), cfg, logger.NewWithWriter("test", false, "error", io.Discard))
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })

	// More than one connection, so the migration's one is among them.
	conns := make([]*pgxpool.Conn, 0, 3)
	for range 3 {
		conn, err := svc.pool.Acquire(t.Context())
		require.NoError(t, err)
		conns = append(conns, conn)
	}
	for _, conn := range conns {
		var setting string
		require.NoError(t, conn.QueryRow(t.Context(), `SHOW statement_timeout`).Scan(&setting))
		assert.Equal(t, "1234ms", setting)
		conn.Release()
	}
}

func TestPoolConfigRejectsBadURL(t *testing.T) {
	_, err := poolConfig(model.Postgres{ConnectionString: "postgres://u:secret@db:notaport/sis"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret", "the password must not reach the log")
}
