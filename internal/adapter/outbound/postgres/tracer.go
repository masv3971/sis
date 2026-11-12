package postgres

import (
	"context"
	"errors"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// queryTracer is otelpgx's tracer minus the failure mark on a unique-key
// violation: the repositories turn it into domain.ErrConflict (HTTP 409),
// a client error, so it must not fail the span or count as a database error.
type queryTracer struct {
	*otelpgx.Tracer
}

func newQueryTracer() *queryTracer {
	return &queryTracer{Tracer: otelpgx.NewTracer()}
}

// TraceQueryEnd drops a unique-key violation from the event before otelpgx
// sees it; the caller still gets the original error.
func (t *queryTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	if pgErr, ok := errors.AsType[*pgconn.PgError](data.Err); ok && pgErr.Code == uniqueViolation {
		data.Err = nil
	}
	t.Tracer.TraceQueryEnd(ctx, conn, data)
}
