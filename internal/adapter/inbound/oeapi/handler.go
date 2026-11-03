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

// Client bundles the OEAPI handlers with the read-model Client and a logger.
// It is a "Client" rather than a "Service" because it owns no runtime
// resources and has nothing to shut down.
//
// The receiver is named c to match the vc convention; the gin.Context
// parameter is therefore named gc to avoid shadowing.
type Client struct {
	cfg     *model.Cfg
	catalog *readmodel.Client
	log     *logger.Log
}

// New builds the oeapi Client.
func New(_ context.Context, cfg *model.Cfg, catalog *readmodel.Client, log *logger.Log) (*Client, error) {
	return &Client{cfg: cfg, catalog: catalog, log: log.New("oeapi")}, nil
}

// ListPersons publishes persons in the OEAPI page envelope.
func (c *Client) ListPersons(gc *gin.Context) {
	pageNumber, pageSize, params, err := c.page(gc)
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	page, err := c.catalog.Persons.ListPersons(gc.Request.Context(), params)
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	out := make([]dto.Person, 0, len(page.Items))
	for _, person := range page.Items {
		out = append(out, dto.FromPerson(person))
	}
	gc.JSON(http.StatusOK, dto.NewPage(out, pageNumber, pageSize, page.Total))
}

// GetPerson publishes a single person by sourcedId in OEAPI shape.
func (c *Client) GetPerson(gc *gin.Context) {
	person, err := c.catalog.Persons.GetPersonBySourcedID(gc.Request.Context(), gc.Param("id"))
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	gc.JSON(http.StatusOK, dto.FromPerson(person))
}

// The generic handlers below could not be methods on *Client: Go does not
// allow type parameters on methods, so they are free functions that take
// *Client as the first argument. Each returns a gin.HandlerFunc the
// httpserver can mount directly.

// List builds an OEAPI paged list handler for an entity type.
func List[T any, PT domain.EntityPtr[T], D any](c *Client, svc *service.Resource[T, PT], toDTO func(PT) D) gin.HandlerFunc {
	return func(gc *gin.Context) {
		pageNumber, pageSize, params, err := c.page(gc)
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		page, err := svc.List(gc.Request.Context(), params)
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		items, err := publishRefs(c, gc, page.Items...)
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		out := make([]D, 0, len(items))
		for _, e := range items {
			out = append(out, toDTO(e))
		}
		gc.JSON(http.StatusOK, dto.NewPage(out, pageNumber, pageSize, page.Total))
	}
}

// Get builds a get-by-sourcedId handler for an entity type.
func Get[T any, PT domain.EntityPtr[T], D any](c *Client, svc *service.Resource[T, PT], toDTO func(PT) D) gin.HandlerFunc {
	return func(gc *gin.Context) {
		e, err := svc.GetBySourcedID(gc.Request.Context(), gc.Param("id"))
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		items, err := publishRefs(c, gc, e)
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		if len(items) == 0 {
			helpers.WriteError(gc, domain.ErrNotFound)
			return
		}
		gc.JSON(http.StatusOK, toDTO(e))
	}
}

// publishRefs rewrites the items' references to other records into those
// records' sourcedIds (see readmodel.Client.PublishRefs), so every
// relationship in the response can be followed through this provider, and
// returns the items that are publishable: orphans (a required reference no
// longer resolves) are dropped, so a page may hold fewer than pageSize
// items while totals still count stored records. A free function for the
// same reason as List/Get: it is generic over PT.
func publishRefs[PT domain.Entity](c *Client, gc *gin.Context, items ...PT) ([]PT, error) {
	entities := make([]domain.Entity, len(items))
	for i, e := range items {
		entities[i] = e
	}
	publishable, err := c.catalog.PublishRefs(gc.Request.Context(), entities...)
	if err != nil {
		return nil, err
	}
	out := make([]PT, 0, len(items))
	for i, e := range items {
		if publishable[i] {
			out = append(out, e)
		}
	}
	return out, nil
}

// page parses OEAPI pageNumber/pageSize (1-based) query parameters. pageSize
// is clamped to sis.adapter.inbound.oeapi.max_page_size and a pageNumber
// whose offset would overflow is rejected, so neither input can produce an
// unbounded or negative query. An omitted pageSize takes .default_page_size.
func (c *Client) page(gc *gin.Context) (pageNumber, pageSize int, params port.ListParams, err error) {
	oeapiCfg := c.cfg.SIS.Adapter.Inbound.OEAPI
	return helpers.ParsePageNumberSize(gc, oeapiCfg.DefaultPageSize, oeapiCfg.MaxPageSize)
}
