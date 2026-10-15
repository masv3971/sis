// Package restadmin is the inbound native /api/v1 REST adapter: it translates
// HTTP into service-layer calls for persons and the generic entity resources.
// Entry points are methods on Client; route declarations live in the
// httpserver package.
package restadmin

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/helpers"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// Client bundles the /api/v1 handlers with the read-model Client (through
// which it reaches both the person service and the generic entity resources)
// and a logger. It is a "Client" rather than a "Service" because it owns no
// runtime resources and has nothing to shut down.
//
// The receiver is named c to match the vc convention; the gin.Context
// parameter is therefore named gc to avoid shadowing.
type Client struct {
	cfg     *model.Cfg
	catalog *readmodel.Client
	log     *logger.Log
}

// New builds the restadmin Client.
func New(_ context.Context, cfg *model.Cfg, catalog *readmodel.Client, log *logger.Log) (*Client, error) {
	return &Client{cfg: cfg, catalog: catalog, log: log.New("restadmin")}, nil
}

// ListPersons lists persons with limit/offset paging.
func (c *Client) ListPersons(gc *gin.Context) {
	params, err := c.listParams(gc)
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	page, err := c.catalog.Persons.ListPersons(gc.Request.Context(), params)
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	gc.JSON(http.StatusOK, gin.H{
		"persons": page.Items,
		"total":   page.Total,
		"limit":   params.Limit,
		"offset":  params.Offset,
	})
}

// CreatePerson creates a person from the JSON body.
func (c *Client) CreatePerson(gc *gin.Context) {
	var p domain.Person
	if err := gc.ShouldBindJSON(&p); err != nil {
		helpers.WriteError(gc, err)
		return
	}
	created, err := c.catalog.Persons.CreatePerson(gc.Request.Context(), p)
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	gc.JSON(http.StatusCreated, created)
}

// GetPerson returns a single person by id.
func (c *Client) GetPerson(gc *gin.Context) {
	p, err := c.catalog.Persons.GetPerson(gc.Request.Context(), gc.Param("id"))
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	gc.JSON(http.StatusOK, p)
}

// UpdatePerson replaces a person; the path id is authoritative over the body.
func (c *Client) UpdatePerson(gc *gin.Context) {
	var p domain.Person
	if err := gc.ShouldBindJSON(&p); err != nil {
		helpers.WriteError(gc, err)
		return
	}
	p.ID = gc.Param("id")
	updated, err := c.catalog.Persons.UpdatePerson(gc.Request.Context(), p)
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	gc.JSON(http.StatusOK, updated)
}

// DeletePerson removes a person by id.
func (c *Client) DeletePerson(gc *gin.Context) {
	if err := c.catalog.Persons.DeletePerson(gc.Request.Context(), gc.Param("id")); err != nil {
		helpers.WriteError(gc, err)
		return
	}
	gc.Status(http.StatusNoContent)
}

// listParams parses limit/offset/sort/filter query parameters. An omitted
// limit takes sis.adapter.inbound.restadmin.default_limit; a given one must
// be positive and is clamped to .max_limit, so a caller can never request an
// unbounded page.
func (c *Client) listParams(gc *gin.Context) (port.ListParams, error) {
	rc := c.cfg.SIS.Adapter.Inbound.RestAdmin
	return helpers.ParseLimitOffset(gc, rc.DefaultLimit, rc.MaxLimit)
}
