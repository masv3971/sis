package memory_test

import (
	"testing"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/adapter/outbound/repotest"
	"github.com/SUNET/sis/internal/core/domain"
)

func TestPersonSearch(t *testing.T) {
	repotest.PersonSearch(t, memory.NewPersonRepository(), "")
}

func TestListByRef(t *testing.T) {
	repotest.ListByRef(t, memory.NewDocRepo[domain.Enrollment](), "")
}
