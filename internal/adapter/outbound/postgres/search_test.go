package postgres_test

import (
	"strings"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/adapter/outbound/postgres"
	"github.com/SUNET/sis/internal/adapter/outbound/repotest"
	"github.com/SUNET/sis/internal/core/domain"
)

// runPrefix keeps this run's rows apart from other data in the database.
func runPrefix() string {
	return strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "") + "-"
}

func TestPersonSearch(t *testing.T) {
	repotest.PersonSearch(t, newStore(t).Persons(), runPrefix())
}

func TestListByRef(t *testing.T) {
	repotest.ListByRef(t, postgres.NewDocRepo[domain.Enrollment](newStore(t)), runPrefix())
}
