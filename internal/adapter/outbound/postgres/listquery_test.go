package postgres_test

import (
	"testing"

	"github.com/SUNET/sis/internal/adapter/outbound/postgres"
	"github.com/SUNET/sis/internal/adapter/outbound/repotest"
	"github.com/SUNET/sis/internal/core/domain"
)

func TestListQueryDocs(t *testing.T) {
	repotest.ListQueryDocs(t, postgres.NewDocRepo[domain.Course](newStore(t)))
}

func TestListQueryPersons(t *testing.T) {
	repotest.ListQueryPersons(t, postgres.NewDocRepo[domain.Person](newStore(t)))
}
