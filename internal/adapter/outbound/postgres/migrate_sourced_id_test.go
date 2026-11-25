package postgres

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func foldTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("SIS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SIS_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	pool, err := pgxpool.New(t.Context(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// fold runs foldSourcedIDs in a transaction that is always rolled back, on a
// temp entities table that shadows the real one, so the fold sees rows the
// real migrations never produce.
func fold(t *testing.T, pool *pgxpool.Pool, rows [][3]string) (map[string]string, error) {
	t.Helper()
	conn, err := pool.Acquire(t.Context())
	require.NoError(t, err)
	t.Cleanup(conn.Release)
	tx, err := conn.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(t.Context()) })

	_, err = tx.Exec(t.Context(), `CREATE TEMP TABLE entities (
		type TEXT NOT NULL, id TEXT NOT NULL, sourced_id TEXT NOT NULL, data JSONB NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		expires_at TIMESTAMPTZ, PRIMARY KEY (type, id), UNIQUE (type, sourced_id)) ON COMMIT DROP`)
	require.NoError(t, err)
	for _, r := range rows {
		_, err = tx.Exec(t.Context(),
			`INSERT INTO entities (type, id, sourced_id, data) VALUES ($1, $2, $3, jsonb_build_object('sourcedId', $3::text))`,
			r[0], r[1], r[2])
		require.NoError(t, err)
	}
	if err := foldSourcedIDs(t.Context(), tx); err != nil {
		return nil, err
	}
	return readSourced(t, tx), nil
}

func readSourced(t *testing.T, tx pgx.Tx) map[string]string {
	t.Helper()
	out := map[string]string{}
	res, err := tx.Query(t.Context(), `SELECT id, sourced_id, data->>'sourcedId' FROM entities`)
	require.NoError(t, err)
	defer res.Close()
	for res.Next() {
		var id, col, doc string
		require.NoError(t, res.Scan(&id, &col, &doc))
		assert.Equal(t, col, doc, "column and document agree for %s", id)
		out[id] = col
	}
	require.NoError(t, res.Err())
	return out
}

func TestFoldSourcedIDsLowercasesColumnAndDocument(t *testing.T) {
	pool := foldTestPool(t)
	got, err := fold(t, pool, [][3]string{
		{"course", "c1", "BIO202601"},
		{"course", "c2", "already-lower"},
		{"person", "p1", "Liu:Anna.Andersson"},
		// Same folded id under another type is no collision.
		{"organization", "o1", "bio202601"},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"c1": "bio202601", "c2": "already-lower", "p1": "liu:anna.andersson", "o1": "bio202601",
	}, got)
}

func TestFoldSourcedIDsIsLocaleIndependent(t *testing.T) {
	pool := foldTestPool(t)
	// In a Turkish locale the database's lower() turns I into a dotless i;
	// Go's fold keeps i, whatever the database locale.
	got, err := fold(t, pool, [][3]string{{"course", "c1", "BIO"}})
	require.NoError(t, err)
	assert.Equal(t, "bio", got["c1"])
}

func TestFoldSourcedIDsRefusesCollisions(t *testing.T) {
	pool := foldTestPool(t)
	_, err := fold(t, pool, [][3]string{
		{"course", "c1", "BIO"},
		{"course", "c2", "bio"},
		{"course", "c3", "other"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collide ignoring case")
	assert.Contains(t, err.Error(), "course:BIO | bio")
	assert.NotContains(t, err.Error(), "other")
}

// lockTestPool returns a pool whose search_path is a scratch schema holding a
// real entities table, so separate connections contend for it. A temp table
// would be private to one session.
func lockTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("SIS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SIS_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	admin, err := pgxpool.New(t.Context(), url)
	require.NoError(t, err)
	t.Cleanup(admin.Close)

	schema := "fold_lock_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	_, err = admin.Exec(t.Context(), `CREATE SCHEMA `+schema)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = admin.Exec(t.Context(), `DROP SCHEMA `+schema+` CASCADE`) })
	_, err = admin.Exec(t.Context(), `CREATE TABLE `+schema+`.entities (
		type TEXT NOT NULL, id TEXT NOT NULL, sourced_id TEXT NOT NULL, data JSONB NOT NULL,
		PRIMARY KEY (type, id), UNIQUE (type, sourced_id))`)
	require.NoError(t, err)

	cfg, err := pgxpool.ParseConfig(url)
	require.NoError(t, err)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

const insertEntity = `INSERT INTO entities (type, id, sourced_id, data) VALUES ('course', $1, $2, jsonb_build_object('sourcedId', $2::text))`

// A write still open when the fold starts must finish first, or the fold
// would scan without it and leave its mixed-case id behind.
func TestFoldSourcedIDsWaitsForOpenWriter(t *testing.T) {
	pool := lockTestPool(t)
	ctx := t.Context()

	writer, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = writer.Rollback(ctx) })
	_, err = writer.Exec(ctx, insertEntity, "c1", "BIO")
	require.NoError(t, err)

	folder, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = folder.Rollback(ctx) })
	done := make(chan error, 1)
	go func() { done <- foldSourcedIDs(ctx, folder) }()

	select {
	case err := <-done:
		t.Fatalf("fold finished while a write was open: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	require.NoError(t, writer.Commit(ctx))
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("fold did not resume after the write committed")
	}
	assert.Equal(t, map[string]string{"c1": "bio"}, readSourced(t, folder), "the late write is folded too")
}

// Once the fold has scanned, a write must wait for its commit, so it cannot be
// overwritten by a queued update or slip in unfolded.
func TestFoldSourcedIDsBlocksWritesUntilCommit(t *testing.T) {
	pool := lockTestPool(t)
	ctx := t.Context()
	_, err := pool.Exec(ctx, insertEntity, "c1", "BIO")
	require.NoError(t, err)

	folder, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = folder.Rollback(ctx) })
	require.NoError(t, foldSourcedIDs(ctx, folder))

	writer, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = writer.Rollback(ctx) })
	_, err = writer.Exec(ctx, `SET LOCAL lock_timeout = '200ms'`)
	require.NoError(t, err)
	_, err = writer.Exec(ctx, insertEntity, "c2", "CHEM")
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr, "the insert must wait for the migration")
	assert.Equal(t, "55P03", pgErr.Code, "lock_not_available")

	require.NoError(t, folder.Commit(ctx))
	_, err = pool.Exec(ctx, insertEntity, "c3", "after")
	require.NoError(t, err, "writes resume once the migration commits")
}

func TestFoldSourcedIDsRefusesNonASCII(t *testing.T) {
	pool := foldTestPool(t)
	_, err := fold(t, pool, [][3]string{
		{"person", "p1", "Åsa"},
		{"person", "p2", "fine"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "printable ASCII")
	assert.Contains(t, err.Error(), "person:Åsa")
}
