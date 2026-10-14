// Package eduapi is the inbound Edu-API v1.0 read provider: it publishes the
// canonical data in Edu-API shapes (read-only GET). Entry points are methods
// on Client; route declarations live in the httpserver package.
package eduapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	dto "github.com/SUNET/sis/pkg/eduapi"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/helpers"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

const defaultLimit = 100

// Client bundles the Edu-API handlers with the read-model Client and a
// logger. It is a "Client" rather than a "Service" because it owns no
// runtime resources and has nothing to shut down.
//
// The receiver is named c to match the vc convention; the gin.Context
// parameter is therefore named gc to avoid shadowing.
type Client struct {
	catalog *readmodel.Client
	log     *logger.Log
}

// New builds the eduapi Client.
func New(_ context.Context, _ *model.Cfg, catalog *readmodel.Client, log *logger.Log) (*Client, error) {
	return &Client{catalog: catalog, log: log.New("eduapi")}, nil
}

// ListPersons publishes persons in Edu-API shape with an X-Total-Count header.
func (c *Client) ListPersons(gc *gin.Context) {
	page, err := c.catalog.Persons.ListPersons(gc.Request.Context(), limitOffset(gc))
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	out := make([]dto.Person, 0, len(page.Items))
	for _, p := range page.Items {
		out = append(out, dto.FromPerson(p))
	}
	gc.Header("X-Total-Count", strconv.Itoa(page.Total))
	gc.JSON(http.StatusOK, gin.H{"persons": out})
}

// GetPerson publishes a single person by sourcedId in Edu-API shape.
func (c *Client) GetPerson(gc *gin.Context) {
	p, err := c.catalog.Persons.GetPersonBySourcedID(gc.Request.Context(), gc.Param("id"))
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	gc.JSON(http.StatusOK, gin.H{"person": dto.FromPerson(p)})
}

// The generic handlers below could not be methods on *Client: Go does not
// allow type parameters on methods, so they are free functions that take
// *Client as the first argument. Each returns a gin.HandlerFunc the
// httpserver can mount directly.

// List builds a list handler for an entity type, mapping each item under key.
func List[T any, PT domain.EntityPtr[T], D any](_ *Client, svc *service.Resource[T, PT], key string, mapFn func(PT) D) gin.HandlerFunc {
	return func(gc *gin.Context) {
		page, err := svc.List(gc.Request.Context(), limitOffset(gc))
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		out := make([]D, 0, len(page.Items))
		for _, e := range page.Items {
			out = append(out, mapFn(e))
		}
		gc.Header("X-Total-Count", strconv.Itoa(page.Total))
		gc.JSON(http.StatusOK, gin.H{key: out})
	}
}

// Get builds a get-by-sourcedId handler for an entity type.
func Get[T any, PT domain.EntityPtr[T], D any](_ *Client, svc *service.Resource[T, PT], key string, mapFn func(PT) D) gin.HandlerFunc {
	return func(gc *gin.Context) {
		e, err := svc.GetBySourcedID(gc.Request.Context(), gc.Param("id"))
		if err != nil {
			helpers.WriteError(gc, err)
			return
		}
		gc.JSON(http.StatusOK, gin.H{key: mapFn(e)})
	}
}

// limitOffset parses OneRoster-style limit/offset query parameters.
func limitOffset(gc *gin.Context) port.ListParams {
	params := port.ListParams{Limit: defaultLimit, Sort: gc.Query("sort"), Filter: gc.Query("filter")}
	if v := gc.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			params.Limit = n
		}
	}
	if v := gc.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			params.Offset = n
		}
	}
	return params
}
