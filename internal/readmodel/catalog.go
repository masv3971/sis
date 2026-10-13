// Package readmodel exposes the canonical services as a read-only catalog for
// the standards (OEAPI/Edu-API) provider adapters, which publish the same data
// in different shapes.
package readmodel

import (
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
)

// Catalog holds the per-entity services the provider adapters read from.
type Catalog struct {
	Persons          *service.Service
	Organizations    *service.Resource[domain.Organization, *domain.Organization]
	AcademicSessions *service.Resource[domain.AcademicSession, *domain.AcademicSession]
	Courses          *service.Resource[domain.Course, *domain.Course]
	Offerings        *service.Resource[domain.Offering, *domain.Offering]
	Enrollments      *service.Resource[domain.Enrollment, *domain.Enrollment]
}
