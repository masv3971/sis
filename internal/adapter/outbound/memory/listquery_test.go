package memory_test

import (
	"testing"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/adapter/outbound/repotest"
	"github.com/SUNET/sis/internal/core/domain"
)

func TestListQueryDocs(t *testing.T) {
	repotest.ListQueryDocs(t, memory.NewDocRepo[domain.Course]())
}

func TestListQueryPersons(t *testing.T) {
	repotest.ListQueryPersons(t, memory.NewDocRepo[domain.Person]())
}
