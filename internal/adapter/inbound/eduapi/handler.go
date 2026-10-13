// Package eduapi is the inbound Edu-API v1.0 read provider: it publishes the
// canonical data in Edu-API shapes (read-only GET). Mounted behind the read scope.
package eduapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	dto "github.com/SUNET/sis/pkg/eduapi"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
)

const defaultLimit = 100

// Handler serves the Edu-API read endpoints.
type Handler struct {
	cat *readmodel.Catalog
}

// New returns a Handler reading from the catalog.
func New(cat *readmodel.Catalog) *Handler {
	return &Handler{cat: cat}
}

// Register attaches the Edu-API routes to the group.
func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/persons", h.listPersons)
	rg.GET("/persons/:id", h.getPerson)
	rg.GET("/orgs", listEntities(h.cat.Organizations, "orgs", dto.FromOrg))
	rg.GET("/orgs/:id", getEntity(h.cat.Organizations, "org", dto.FromOrg))
	rg.GET("/academicSessions", listEntities(h.cat.AcademicSessions, "academicSessions", dto.FromAcademicSession))
	rg.GET("/academicSessions/:id", getEntity(h.cat.AcademicSessions, "academicSession", dto.FromAcademicSession))
	rg.GET("/courses", listEntities(h.cat.Courses, "courses", dto.FromCourse))
	rg.GET("/courses/:id", getEntity(h.cat.Courses, "course", dto.FromCourse))
	rg.GET("/classes", listEntities(h.cat.Offerings, "classes", dto.FromClass))
	rg.GET("/classes/:id", getEntity(h.cat.Offerings, "class", dto.FromClass))
	rg.GET("/enrollments", listEntities(h.cat.Enrollments, "enrollments", dto.FromEnrollment))
	rg.GET("/enrollments/:id", getEntity(h.cat.Enrollments, "enrollment", dto.FromEnrollment))
}

func (h *Handler) listPersons(c *gin.Context) {
	page, err := h.cat.Persons.ListPersons(c.Request.Context(), limitOffset(c))
	if err != nil {
		serverError(c)
		return
	}
	out := make([]dto.Person, 0, len(page.Items))
	for _, p := range page.Items {
		out = append(out, dto.FromPerson(p))
	}
	c.Header("X-Total-Count", strconv.Itoa(page.Total))
	c.JSON(http.StatusOK, gin.H{"persons": out})
}

func (h *Handler) getPerson(c *gin.Context) {
	p, err := h.cat.Persons.GetPersonBySourcedID(c.Request.Context(), c.Param("id"))
	if err != nil {
		notFoundOrServer(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"person": dto.FromPerson(p)})
}

// listEntities builds a list handler for an entity type, mapping each item.
func listEntities[T any, PT domain.EntityPtr[T], D any](svc *service.Resource[T, PT], key string, mapFn func(PT) D) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, err := svc.List(c.Request.Context(), limitOffset(c))
		if err != nil {
			serverError(c)
			return
		}
		out := make([]D, 0, len(page.Items))
		for _, e := range page.Items {
			out = append(out, mapFn(e))
		}
		c.Header("X-Total-Count", strconv.Itoa(page.Total))
		c.JSON(http.StatusOK, gin.H{key: out})
	}
}

// getEntity builds a get-by-sourcedId handler for an entity type.
func getEntity[T any, PT domain.EntityPtr[T], D any](svc *service.Resource[T, PT], key string, mapFn func(PT) D) gin.HandlerFunc {
	return func(c *gin.Context) {
		e, err := svc.GetBySourcedID(c.Request.Context(), c.Param("id"))
		if err != nil {
			notFoundOrServer(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{key: mapFn(e)})
	}
}

// limitOffset parses OneRoster-style limit/offset query parameters.
func limitOffset(c *gin.Context) port.ListParams {
	params := port.ListParams{Limit: defaultLimit, Sort: c.Query("sort"), Filter: c.Query("filter")}
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			params.Limit = n
		}
	}
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			params.Offset = n
		}
	}
	return params
}

func serverError(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
}

func notFoundOrServer(c *gin.Context, err error) {
	if errors.Is(err, domain.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	serverError(c)
}
