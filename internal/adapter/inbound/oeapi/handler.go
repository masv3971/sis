// Package oeapi is the inbound Open Education API v6.0 read provider: it
// publishes the canonical data in OEAPI shapes with the OEAPI page envelope
// (read-only GET). Mounted behind the read scope.
package oeapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	dto "github.com/SUNET/sis/pkg/oeapi"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
)

const defaultPageSize = 10

// Handler serves the OEAPI read endpoints.
type Handler struct {
	cat *readmodel.Catalog
}

// New returns a Handler reading from the catalog.
func New(cat *readmodel.Catalog) *Handler {
	return &Handler{cat: cat}
}

// Register attaches the OEAPI routes to the group.
func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/persons", h.listPersons)
	rg.GET("/persons/:id", h.getPerson)
	rg.GET("/organizations", listEntities(h.cat.Organizations, dto.FromOrganization))
	rg.GET("/organizations/:id", getEntity(h.cat.Organizations, dto.FromOrganization))
	rg.GET("/academic-sessions", listEntities(h.cat.AcademicSessions, dto.FromAcademicSession))
	rg.GET("/academic-sessions/:id", getEntity(h.cat.AcademicSessions, dto.FromAcademicSession))
	rg.GET("/courses", listEntities(h.cat.Courses, dto.FromCourse))
	rg.GET("/courses/:id", getEntity(h.cat.Courses, dto.FromCourse))
	rg.GET("/offerings", listEntities(h.cat.Offerings, dto.FromOffering))
	rg.GET("/offerings/:id", getEntity(h.cat.Offerings, dto.FromOffering))
	rg.GET("/associations", listEntities(h.cat.Enrollments, dto.FromAssociation))
	rg.GET("/associations/:id", getEntity(h.cat.Enrollments, dto.FromAssociation))
}

func (h *Handler) listPersons(c *gin.Context) {
	pageNumber, pageSize := page(c)
	p, err := h.cat.Persons.ListPersons(c.Request.Context(), pageParams(pageNumber, pageSize))
	if err != nil {
		serverError(c)
		return
	}
	out := make([]dto.Person, 0, len(p.Items))
	for _, person := range p.Items {
		out = append(out, dto.FromPerson(person))
	}
	c.JSON(http.StatusOK, dto.NewPage(out, pageNumber, pageSize, p.Total))
}

func (h *Handler) getPerson(c *gin.Context) {
	p, err := h.cat.Persons.GetPersonBySourcedID(c.Request.Context(), c.Param("id"))
	if err != nil {
		notFoundOrServer(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.FromPerson(p))
}

// listEntities builds an OEAPI paged list handler for an entity type.
func listEntities[T any, PT domain.EntityPtr[T], D any](svc *service.Resource[T, PT], mapFn func(PT) D) gin.HandlerFunc {
	return func(c *gin.Context) {
		pageNumber, pageSize := page(c)
		res, err := svc.List(c.Request.Context(), pageParams(pageNumber, pageSize))
		if err != nil {
			serverError(c)
			return
		}
		out := make([]D, 0, len(res.Items))
		for _, e := range res.Items {
			out = append(out, mapFn(e))
		}
		c.JSON(http.StatusOK, dto.NewPage(out, pageNumber, pageSize, res.Total))
	}
}

// getEntity builds a get-by-sourcedId handler for an entity type.
func getEntity[T any, PT domain.EntityPtr[T], D any](svc *service.Resource[T, PT], mapFn func(PT) D) gin.HandlerFunc {
	return func(c *gin.Context) {
		e, err := svc.GetBySourcedID(c.Request.Context(), c.Param("id"))
		if err != nil {
			notFoundOrServer(c, err)
			return
		}
		c.JSON(http.StatusOK, mapFn(e))
	}
}

// page parses OEAPI pageNumber/pageSize (1-based) query parameters.
func page(c *gin.Context) (pageNumber, pageSize int) {
	pageNumber, pageSize = 1, defaultPageSize
	if v := c.Query("pageNumber"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			pageNumber = n
		}
	}
	if v := c.Query("pageSize"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			pageSize = n
		}
	}
	return pageNumber, pageSize
}

func pageParams(pageNumber, pageSize int) port.ListParams {
	return port.ListParams{Limit: pageSize, Offset: (pageNumber - 1) * pageSize}
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
