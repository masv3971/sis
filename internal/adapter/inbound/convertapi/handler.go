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
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/convert"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/helpers"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// Client bundles the conversion handlers with the convert Client (formats)
// and the read-model Client (storage for populate). It is a "Client" rather
// than a "Service" because it owns no runtime resources and has nothing to
// shut down.
//
// The receiver is named c to match the vc convention; the gin.Context
// parameter is therefore named gc to avoid shadowing.
type Client struct {
	cfg     *model.Cfg
	catalog *readmodel.Client
	convert *convert.Client
	now     func() time.Time
	log     *logger.Log
}

// New builds the convertapi Client, with its own convert Client (pure, so
// nothing is shared or shut down).
func New(ctx context.Context, cfg *model.Cfg, catalog *readmodel.Client, log *logger.Log) (*Client, error) {
	conv, err := convert.New(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	return &Client{cfg: cfg, catalog: catalog, convert: conv, now: time.Now, log: log.New("convertapi")}, nil
}

// Metadata describes the request's document.
type Metadata struct {
	// InputFormat is the format of data. Supported: elm.
	InputFormat string `json:"input_format" binding:"required" example:"elm"`
	// OutputFormat is the format to return (parse only). Supported: eduapi, oeapi.
	OutputFormat string `json:"output_format,omitempty" example:"eduapi"`
}

// ParseRequest is the body of POST /api/v1/parse.
type ParseRequest struct {
	Metadata Metadata `json:"metadata"`
	// Data is the document to convert, e.g. an ELM v3 credential (JSON-LD).
	Data json.RawMessage `json:"data" binding:"required" swaggertype:"object"`
}

// ParseReply is the response of POST /api/v1/parse.
type ParseReply struct {
	Metadata Metadata `json:"metadata"`
	// Data holds the converted records, one array per resource collection of
	// the output format (Edu-API: persons, orgs, academicSessions, courses,
	// programs, classes, enrollments, results; OEAPI: persons, organizations,
	// academicSessions, courses, programs, offerings, associations, results).
	// References between them are sourcedIds.
	Data any `json:"data" swaggertype:"object"`
	// Warnings name the parts of the document that were not converted.
	Warnings []string `json:"warnings"`
}

// PopulateRequest is the body of POST /api/v1/populate.
type PopulateRequest struct {
	Metadata Metadata `json:"metadata"`
	// TTL is how long the records are served, as a Go duration ("24h",
	// "90m"). Omitted: they never expire.
	TTL string `json:"ttl,omitempty" example:"24h"`
	// Data is the document to load, e.g. an ELM v3 credential (JSON-LD).
	Data json.RawMessage `json:"data" binding:"required" swaggertype:"object"`
}

// PopulateReply is the response of POST /api/v1/populate.
type PopulateReply struct {
	Metadata Metadata `json:"metadata"`
	// ExpiresAt is when the loaded records stop being served. Absent when no
	// ttl was given. A record that already existed without expiry keeps
	// none.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	// Items lists every stored record, in the order written.
	Items    []readmodel.PopulateItem `json:"items"`
	Warnings []string                 `json:"warnings"`
}

// Parse converts a document and returns it in the requested format; nothing
// is stored.
//
//	@Summary		Convert a document
//	@ID				parse
//	@Description	Converts data from metadata.input_format (elm: an ELM v3 / European Digital Credential JSON-LD document) to metadata.output_format (eduapi or oeapi) and returns it. Nothing is stored. Records keep the document's ids as sourcedIds; references between them are sourcedIds. Requires scope sis.read or role admin, registrar or readonly.
//	@Tags			convert
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		ParseRequest			true	"Document to convert"
//	@Success		200		{object}	ParseReply				"Converted document"
//	@Failure		400		{object}	helpers.ErrorResponse	"Malformed body, unsupported format, or a document that cannot be converted"
//	@Failure		401		{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		413		{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500		{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/parse [post]
func (c *Client) Parse(gc *gin.Context) {
	var req ParseRequest
	if err := helpers.BindJSON(gc, &req); err != nil {
		helpers.WriteError(gc, err)
		return
	}
	if err := c.checkFormats(req.Metadata, true); err != nil {
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
//	@Description	Converts data from metadata.input_format (elm) and upserts every record by sourcedId, so /eduapi and /oeapi (and /api/v1) serve them. With ttl the records stop being served after that duration and are then deleted; re-populating refreshes the expiry, but a record that already existed without expiry keeps none. References in the document must resolve within it or to stored records. The document is checked completely before anything is stored; a storage failure part-way leaves earlier records stored, and retrying the same request converges. Requires scope sis.write or role admin or registrar.
//	@Tags			convert
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		PopulateRequest			true	"Document to load"
//	@Success		200		{object}	PopulateReply			"Stored records"
//	@Failure		400		{object}	helpers.ErrorResponse	"Malformed body, unsupported format, invalid ttl, or a document that cannot be converted"
//	@Failure		401		{object}	auth.ErrorResponse		"Missing or invalid token"
//	@Failure		403		{object}	auth.ErrorResponse		"Insufficient scope or role"
//	@Failure		409		{object}	helpers.ErrorResponse	"A record id collides with an existing record"
//	@Failure		413		{object}	helpers.ErrorResponse	"Request body too large"
//	@Failure		500		{object}	helpers.ErrorResponse	"Internal Server Error"
//	@Router			/populate [post]
func (c *Client) Populate(gc *gin.Context) {
	var req PopulateRequest
	if err := helpers.BindJSON(gc, &req); err != nil {
		helpers.WriteError(gc, err)
		return
	}
	if err := c.checkFormats(req.Metadata, false); err != nil {
		helpers.WriteError(gc, err)
		return
	}
	var expiresAt *time.Time
	if req.TTL != "" {
		ttl, err := time.ParseDuration(req.TTL)
		if err != nil || ttl <= 0 {
			helpers.WriteError(gc, &domain.ValidationError{Field: "ttl", Msg: `must be a positive Go duration, e.g. "24h" or "90m"`})
			return
		}
		t := c.now().UTC().Add(ttl).Truncate(time.Microsecond) // Postgres precision
		expiresAt = &t
	}
	parsed, err := c.convert.Parse(req.Metadata.InputFormat, req.Data, "data")
	if err != nil {
		helpers.WriteError(gc, err)
		return
	}
	items, err := c.catalog.Populate(gc.Request.Context(), parsed.Bundle, expiresAt)
	if err != nil {
		if len(items) > 0 {
			c.log.Error("populate stored part of a document", "stored", len(items), "err", err)
		}
		helpers.WriteError(gc, err)
		return
	}
	gc.JSON(http.StatusOK, PopulateReply{Metadata: req.Metadata, ExpiresAt: expiresAt, Items: items, Warnings: parsed.Warnings})
}

// checkFormats rejects unsupported formats. withOutput requires an output
// format (parse); without it, one must not be given (populate).
func (c *Client) checkFormats(m Metadata, withOutput bool) error {
	if !c.convert.HasInput(m.InputFormat) {
		return &domain.ValidationError{Field: "metadata.input_format", Msg: "must be one of " + strings.Join(c.convert.InputFormats(), ", ")}
	}
	switch {
	case withOutput && !c.convert.HasOutput(m.OutputFormat):
		return &domain.ValidationError{Field: "metadata.output_format", Msg: "must be one of " + strings.Join(c.convert.OutputFormats(), ", ")}
	case !withOutput && m.OutputFormat != "":
		return &domain.ValidationError{Field: "metadata.output_format", Msg: "not used by populate; the records are served by /eduapi and /oeapi"}
	}
	return nil
}
