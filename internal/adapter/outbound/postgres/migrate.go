package postgres

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.up.sql
var migrationsFS embed.FS

// migrationLockKey is the pg_advisory_lock key that serialises migration runs
// across replicas. Arbitrary but fixed; ASCII "sis_migr".
const migrationLockKey int64 = 0x7369735f6d696772

// migrate applies any embedded *.up.sql files not yet recorded in
// schema_migrations, each in its own transaction, in filename order. It is a
// minimal runner kept dependency-free; the file naming matches golang-migrate
// so a swap later is mechanical.
//
// The whole sequence runs on one dedicated connection holding a session-level
// advisory lock, so replicas starting together take turns: the first applies
// the pending versions and the rest then observe them as already recorded.
func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}

	// Waiting for another replica's migrations, and the migrations
	// themselves, may take longer than sis.adapter.outbound.postgres.
	// statement_timeout; the startup timeout (ctx) bounds them instead.
	// releaseMigrationLock restores the session default.
	if _, err := conn.Exec(ctx, `SET statement_timeout = 0`); err != nil {
		discardConn(conn)
		return fmt.Errorf("lift statement_timeout for migrations: %w", err)
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		// The lock state is unknown (the query may have been sent), so
		// never hand this session back to the pool.
		discardConn(conn)
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer releaseMigrationLock(conn)

	return migrateLocked(ctx, conn)
}

// migrationUnlockTimeout bounds the advisory-unlock round trip during
// cleanup. It is independent of the caller's ctx: the unlock must still run
// after ctx is cancelled, but must not hang startup if PostgreSQL stops
// responding.
const migrationUnlockTimeout = 5 * time.Second

// releaseMigrationLock unlocks the migration advisory lock on its own bounded
// context and returns conn to the pool only if the unlock was confirmed.
// Otherwise the connection is closed, which ends the session and so releases
// the lock server-side, instead of pooling a possibly lock-holding session.
func releaseMigrationLock(conn *pgxpool.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), migrationUnlockTimeout)
	defer cancel()
	var unlocked bool
	if err := conn.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, migrationLockKey).Scan(&unlocked); err != nil || !unlocked {
		discardConn(conn)
		return
	}
	// Back to the pool's statement_timeout before the pool reuses it.
	if _, err := conn.Exec(ctx, `RESET statement_timeout`); err != nil {
		discardConn(conn)
		return
	}
	conn.Release()
}

// discardConn removes conn from the pool and closes it, bounded by
// migrationUnlockTimeout.
func discardConn(conn *pgxpool.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), migrationUnlockTimeout)
	defer cancel()
	_ = conn.Hijack().Close(ctx)
}

// migrateLocked runs the migration sequence on conn; the caller must hold
// the migration advisory lock on that same connection.
func migrateLocked(ctx context.Context, conn *pgxpool.Conn) error {
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		version := strings.TrimSuffix(name, ".up.sql")
		var exists bool
		if err := conn.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, version,
		).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if exists {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if err := applyMigration(ctx, conn, version, string(sqlBytes)); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, conn *pgxpool.Conn, version, sqlText string) error {
	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, sqlText); err != nil {
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`, version,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", version, err)
		}
		return nil
	})
}
