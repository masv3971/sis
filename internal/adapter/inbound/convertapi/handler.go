// Package convertapi is the inbound adapter for document conversion: POST
// /api/v1/parse converts a document (e.g. an ELM credential) to Edu-API or
// OEAPI and returns it; POST /api/v1/populate converts it and stores the
// records, optionally time-limited, so the /eduapi and /oeapi providers
// serve them. Entry points are methods on Client; route declarations live in
// the httpserver package.
package convertapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/command"
	"github.com/SUNET/sis/internal/convert"
	"github.com/SUNET/sis/pkg/helpers"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// Client bundles the conversion handlers with the convert Client (formats)
// and the command Client (populate). It is a "Client" rather than a
// "Service" because it owns no runtime resources and has nothing to shut
// down.
//
// The receiver is named c to match the vc convention; the gin.Context
// parameter is therefore named gc to avoid shadowing.
type Client struct {
	cfg      *model.Cfg
	commands *command.Client
	convert  *convert.Client
	log      *logger.Log
}

// New builds the convertapi Client, with its own convert Client (pure, so
// nothing is shared or shut down).
func New(ctx context.Context, cfg *model.Cfg, commands *command.Client, log *logger.Log) (*Client, error) {
	conv, err := convert.New(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	return &Client{cfg: cfg, commands: commands, convert: conv, log: log.New("convertapi")}, nil
}

// ParseRequest is the body of POST /api/v1/parse.
type ParseRequest struct {
	Metadata command.Metadata `json:"metadata"`
	// Data is the document to convert, e.g. an ELM v3 credential (JSON-LD).
	Data json.RawMessage `json:"data" binding:"required" swaggertype:"object"`
}

// ParseReply is the response of POST /api/v1/parse.
type ParseReply struct {
	Metadata command.Metadata `json:"metadata"`
	// Data holds the converted records, one array per resource collection of
	// the output format (Edu-API: persons, orgs, academicSessions, courses,
	// programs, classes, enrollments, results; OEAPI: persons, organizations,
	// academicSessions, courses, programs, offerings, associations, results).
	// References between them are sourcedIds.
	Data any `json:"data" swaggertype:"object"`
	// Warnings name the parts of the document that were not converted.
	Warnings []string `json:"warnings"`
}

// Parse converts a document and returns it in the requested format; nothing
// is stored.
//
//	@Summary		Convert a document
//	@ID				parse
//	@Description	Converts data from metadata.input_format (elm: an ELM v3 / European Digital Credential JSON-LD document) to metadata.output_format (eduapi or oeapi) and returns it. Nothing is stored. Records keep the document's ids as sourcedIds; references between them are sourcedIds. Requires scope sis.read.
//	@Tags			convert
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		ParseRequest			true	"Document to convert"
//	@Success		200		{object}	ParseReply				"Converted document"
//	@Failure		400		{object}	helpers.ErrorResponse	"Malformed body, unsupported format, or a document that cannot be converted"
//	@Failure		401		{object}	auth.ErrorResponse		"Missing, invalid or untrusted token"
//	@Failure		403		{object}	auth.ErrorResponse		"Token lacks the required scope"
//	@Failure		413		{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500		{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/parse [post]
func (c *Client) Parse(gc *gin.Context) {
	var req ParseRequest
	if err := helpers.BindJSON(gc, &req); err != nil {
		helpers.WriteError(gc, err)
		return
	}
	if err := req.Metadata.Check(c.convert, true); err != nil {
		helpers.WriteError(gc, err)
		return
	}
	parsed, err := c.convert.Parse(req.Metadata.InputFormat, req.Data, "data")
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	out, err := c.convert.Render(req.Metadata.OutputFormat, parsed.Bundle)
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	gc.JSON(http.StatusOK, ParseReply{Metadata: req.Metadata, Data: out, Warnings: parsed.Warnings})
}

// Populate converts a document and stores its records.
//
//	@Summary		Load a document
//	@ID				populate
//	@Description	Converts data from metadata.input_format (elm) and upserts every record by sourcedId, so /eduapi and /oeapi (and /api/v1) serve them. With ttl the records stop being served after that duration and are then deleted; re-populating refreshes the expiry, but a record that already existed without expiry keeps none. References in the document must resolve within it or to stored records. The document is checked completely before anything is stored; a storage failure part-way leaves earlier records stored, and retrying the same request converges. Requires scope sis.write.
//	@Tags			convert
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		command.PopulateRequest	true	"Document to load"
//	@Success		200		{object}	command.PopulateReply	"Stored records"
//	@Failure		400		{object}	helpers.ErrorResponse	"Malformed body, unsupported format, invalid ttl, or a document that cannot be converted"
//	@Failure		401		{object}	auth.ErrorResponse		"Missing, invalid or untrusted token"
//	@Failure		403		{object}	auth.ErrorResponse		"Token lacks the required scope"
//	@Failure		409		{object}	helpers.ErrorResponse	"A record id collides with an existing record"
//	@Failure		413		{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500		{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/populate [post]
func (c *Client) Populate(gc *gin.Context) {
	var req command.PopulateRequest
	if err := helpers.BindJSON(gc, &req); err != nil {
		helpers.WriteError(gc, err)
		return
	}
	reply, err := c.commands.Populate(gc.Request.Context(), req)
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	gc.JSON(http.StatusOK, reply)
}
