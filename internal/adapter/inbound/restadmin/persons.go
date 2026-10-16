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

// PersonListReply is the response envelope of GET /api/v1/persons.
type PersonListReply struct {
	Persons []domain.Person `json:"persons"`
	// Total is the number of persons across all pages.
	Total int `json:"total"`
	// Limit is the effective page size after defaulting and clamping.
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// ListPersons lists persons with limit/offset paging.
//
//	@Summary		List persons
//	@ID				list-persons
//	@Description	Lists persons with limit/offset paging. Requires scope sis.read or role admin, registrar or readonly.
//	@Tags			persons
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit	query		int						false	"Page size; defaults to default_limit and is clamped to max_limit"	minimum(1)
//	@Param			offset	query		int						false	"Number of items to skip"											minimum(0)
//	@Success		200		{object}	PersonListReply			"Success"
//	@Failure		400		{object}	helpers.ErrorResponse	"Invalid paging parameters"
//	@Failure		401		{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		500		{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/persons [get]
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
	gc.JSON(http.StatusOK, PersonListReply{
		Persons: page.Items,
		Total:   page.Total,
		Limit:   params.Limit,
		Offset:  params.Offset,
	})
}

// CreatePerson creates a person from the JSON body.
//
//	@Summary		Create person
//	@ID				create-person
//	@Description	Creates a person. givenName, familyName and status are required. id is generated when empty, sourcedId defaults to id, and createdAt/updatedAt are set by the server. Requires scope sis.write or role admin or registrar.
//	@Tags			persons
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			person	body		domain.Person			true	"Person to create"
//	@Success		201		{object}	domain.Person			"Created"
//	@Failure		400		{object}	helpers.ErrorResponse	"Malformed body or validation error"
//	@Failure		401		{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		409		{object}	helpers.ErrorResponse	"id or sourcedId already in use"
//	@Failure		413		{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500		{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/persons [post]
func (c *Client) CreatePerson(gc *gin.Context) {
	var p domain.Person
	if err := helpers.BindJSON(gc, &p); err != nil {
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
//
//	@Summary		Get person
//	@ID				get-person
//	@Description	Returns one person by id. Requires scope sis.read or role admin, registrar or readonly.
//	@Tags			persons
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string					true	"Person id"
//	@Success		200	{object}	domain.Person			"Success"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403	{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/persons/{id} [get]
func (c *Client) GetPerson(gc *gin.Context) {
	p, err := c.catalog.Persons.GetPerson(gc.Request.Context(), gc.Param("id"))
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	gc.JSON(http.StatusOK, p)
}

// UpdatePerson replaces a person; the path id is authoritative over the body.
//
//	@Summary		Replace person
//	@ID				update-person
//	@Description	Replaces a person. The path id wins over any id in the body; createdAt is preserved and an empty sourcedId keeps the stored one. Requires scope sis.write or role admin or registrar.
//	@Tags			persons
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"Person id"
//	@Param			person	body		domain.Person			true	"Replacement person"
//	@Success		200		{object}	domain.Person			"Success"
//	@Failure		400		{object}	helpers.ErrorResponse	"Malformed body or validation error"
//	@Failure		401		{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404		{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		409		{object}	helpers.ErrorResponse	"sourcedId already in use"
//	@Failure		413		{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500		{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/persons/{id} [put]
func (c *Client) UpdatePerson(gc *gin.Context) {
	var p domain.Person
	if err := helpers.BindJSON(gc, &p); err != nil {
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
//
//	@Summary		Delete person
//	@ID				delete-person
//	@Description	Deletes one person by id. Requires scope sis.write or role admin or registrar.
//	@Tags			persons
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Person id"
//	@Success		204	"No Content"
//	@Failure		401	{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403	{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		404	{object}	helpers.ErrorResponse	"Not Found"
//	@Failure		500	{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/persons/{id} [delete]
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
