package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SUNET/sis/internal/adapter/outbound/postgres"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

// TestListByRef checks the filter, the total, live-only reads and that a
// field name that could alter the SQL is refused.
func TestListByRef(t *testing.T) {
	ctx := t.Context()
	svc := newStore(t)
	suffix := time.Now().Format("150405.000000")
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	results := postgres.NewDocRepo[domain.Result](svc)

	offering := "off-" + suffix
	records := []*domain.Result{
		{Base: domain.Base{ID: "lr1-" + suffix, SourcedID: "lr1-" + suffix, CreatedAt: now, UpdatedAt: now}, PersonID: "p", CourseID: "c", OfferingID: offering},
		{Base: domain.Base{ID: "lr2-" + suffix, SourcedID: "lr2-" + suffix, CreatedAt: now, UpdatedAt: now}, PersonID: "q", CourseID: "c", OfferingID: offering},
		{Base: domain.Base{ID: "lr3-" + suffix, SourcedID: "lr3-" + suffix, CreatedAt: now, UpdatedAt: now}, PersonID: "p", CourseID: "c", OfferingID: "other-" + suffix},
		{Base: domain.Base{ID: "lr4-" + suffix, SourcedID: "lr4-" + suffix, CreatedAt: now, UpdatedAt: now, ExpiresAt: &past}, PersonID: "p", CourseID: "c", OfferingID: offering},
	}
	t.Cleanup(func() {
		for _, r := range records {
			_ = results.Delete(context.Background(), r.ID)
		}
		_, _ = results.DeleteExpired(context.Background(), time.Now().Add(time.Hour))
	})
	for _, r := range records {
		require.NoError(t, results.Create(ctx, r))
	}

	page, err := results.ListByRef(ctx, "offeringId", offering, port.ListParams{Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, 2, page.Total, "live matches only")
	require.Len(t, page.Items, 1)
	assert.Equal(t, records[0].ID, page.Items[0].ID, "ordered by sourcedId")

	_, err = results.ListByRef(ctx, "offeringId' OR '1'='1", offering, port.ListParams{})
	assert.Error(t, err, "field name must be checked")
}
