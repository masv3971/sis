// Package readmodel exposes the canonical services as a read-only catalog for
// the standards (OEAPI/Edu-API) provider adapters, which publish the same data
// in different shapes.
package readmodel

import (
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
)

// Client holds the per-entity services the provider adapters read from. It is
// a "Client" rather than a "Service" because it owns no runtime resources —
// it is a bundle of already-built per-entity services, assembled once at
// startup, with nothing to shut down.
type Client struct {
	Persons          *service.Service
	Organizations    *service.Resource[domain.Organization, *domain.Organization]
	AcademicSessions *service.Resource[domain.AcademicSession, *domain.AcademicSession]
	Courses          *service.Resource[domain.Course, *domain.Course]
	Offerings        *service.Resource[domain.Offering, *domain.Offering]
	Enrollments      *service.Resource[domain.Enrollment, *domain.Enrollment]
	Programs         *service.Resource[domain.Program, *domain.Program]
	Results          *service.Resource[domain.Result, *domain.Result]
}
