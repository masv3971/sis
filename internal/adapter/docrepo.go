package adapter

import (
	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/adapter/outbound/postgres"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
)

// docRepo builds a generic port.Repository for entity type T, backed by the
// Postgres doc store when pg is non-nil, else in-memory. It is a private
// free function because Go forbids type parameters on methods; keeping it
// here (in the composition root) is how we stop *postgres.Service leaking
// outside internal/adapter — only this file and selectStore see it.
func docRepo[T any, PT domain.EntityPtr[T]](pg *postgres.Service) port.Repository[T, PT] {
	if pg != nil {
		return postgres.NewDocRepo[T, PT](pg)
	}
	return memory.NewDocRepo[T, PT]()
}
