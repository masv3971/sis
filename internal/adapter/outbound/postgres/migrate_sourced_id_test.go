package postgres

import (
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
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
