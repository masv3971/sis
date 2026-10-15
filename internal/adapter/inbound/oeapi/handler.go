// Package oeapi is the inbound Open Education API v6.0 read provider: it
// publishes the canonical data in OEAPI shapes with the OEAPI page envelope
// (read-only GET). Entry points are methods on Client; route declarations
// live in the httpserver package.
package oeapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	dto "github.com/SUNET/sis/pkg/oeapi"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/helpers"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

const defaultPageSize = 10

// Client bundles the OEAPI handlers with the read-model Client and a logger.
// It is a "Client" rather than a "Service" because it owns no runtime
// resources and has nothing to shut down.
//
// The receiver is named c to match the vc convention; the gin.Context
// parameter is therefore named gc to avoid shadowing.
type Client struct {
	catalog  *readmodel.Client
	maxLimit int
	log      *logger.Log
}

// New builds the oeapi Client.
func New(_ context.Context, cfg *model.Cfg, catalog *readmodel.Client, log *logger.Log) (*Client, error) {
	return &Client{catalog: catalog, maxLimit: cfg.SIS.APIServer.MaxLimit, log: log.New("oeapi")}, nil
}

// ListPersons publishes persons in the OEAPI page envelope.
func (c *Client) ListPersons(gc *gin.Context) {
	pageNumber, pageSize, params, err := c.page(gc)
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	p, err := c.catalog.Persons.ListPersons(gc.Request.Context(), params)
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	out := make([]dto.Person, 0, len(p.Items))
	for _, person := range p.Items {
		out = append(out, dto.FromPerson(person))
	}
	gc.JSON(http.StatusOK, dto.NewPage(out, pageNumber, pageSize, p.Total))
}

// GetPerson publishes a single person by sourcedId in OEAPI shape.
func (c *Client) GetPerson(gc *gin.Context) {
	p, err := c.catalog.Persons.GetPersonBySourcedID(gc.Request.Context(), gc.Param("id"))
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	gc.JSON(http.StatusOK, dto.FromPerson(p))
}

// The generic handlers below could not be methods on *Client: Go does not
// allow type parameters on methods, so they are free functions that take
// *Client as the first argument. Each returns a gin.HandlerFunc the
// httpserver can mount directly.

// List builds an OEAPI paged list handler for an entity type.
func List[T any, PT domain.EntityPtr[T], D any](c *Client, svc *service.Resource[T, PT], mapFn func(PT) D) gin.HandlerFunc {
	return func(gc *gin.Context) {
		pageNumber, pageSize, params, err := c.page(gc)
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		res, err := svc.List(gc.Request.Context(), params)
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		out := make([]D, 0, len(res.Items))
		for _, e := range res.Items {
			out = append(out, mapFn(e))
		}
		gc.JSON(http.StatusOK, dto.NewPage(out, pageNumber, pageSize, res.Total))
	}
}

// Get builds a get-by-sourcedId handler for an entity type.
func Get[T any, PT domain.EntityPtr[T], D any](_ *Client, svc *service.Resource[T, PT], mapFn func(PT) D) gin.HandlerFunc {
	return func(gc *gin.Context) {
		e, err := svc.GetBySourcedID(gc.Request.Context(), gc.Param("id"))
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		gc.JSON(http.StatusOK, mapFn(e))
	}
}

// page parses OEAPI pageNumber/pageSize (1-based) query parameters. pageSize
// is clamped to the configured max_limit and a pageNumber whose offset would
// overflow is rejected, so neither input can produce an unbounded or negative
// query.
func (c *Client) page(gc *gin.Context) (pageNumber, pageSize int, params port.ListParams, err error) {
	return helpers.ParsePageNumberSize(gc, defaultPageSize, c.maxLimit)
}
