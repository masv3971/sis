package command

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/SUNET/sis/internal/convert"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/readmodel"
)

// Metadata describes a document's format.
type Metadata struct {
	// InputFormat is the format of data. Supported: elm.
	InputFormat string `json:"input_format" binding:"required" example:"elm"`
	// OutputFormat is the format to return (parse only). Supported: eduapi, oeapi.
	OutputFormat string `json:"output_format,omitempty" example:"eduapi"`
}

// Check rejects unsupported formats. withOutput requires an output format
// (parse); without it, one must not be given (populate).
func (m Metadata) Check(converter *convert.Client, withOutput bool) error {
	if !converter.HasInput(m.InputFormat) {
		return &domain.ValidationError{Field: "metadata.input_format", Msg: "must be one of " + strings.Join(converter.InputFormats(), ", ")}
	}
	switch {
	case withOutput && !converter.HasOutput(m.OutputFormat):
		return &domain.ValidationError{Field: "metadata.output_format", Msg: "must be one of " + strings.Join(converter.OutputFormats(), ", ")}
	case !withOutput && m.OutputFormat != "":
		return &domain.ValidationError{Field: "metadata.output_format", Msg: "not used by populate; the records are served by /eduapi and /oeapi"}
	}
	return nil
}

// PopulateRequest is the populate command (POST /api/v1/populate).
type PopulateRequest struct {
	Metadata Metadata `json:"metadata"`
	// TTL is how long the records are served, as a Go duration ("24h",
	// "90m"). Omitted: they never expire.
	TTL string `json:"ttl,omitempty" example:"24h"`
	// Data is the document to load, e.g. an ELM v3 credential (JSON-LD),
	// plain or as EDCI issues it ({"credential": ...} or sealed
	// {"payload": ..., "signatures": ...}; the seal is not verified).
	Data json.RawMessage `json:"data" binding:"required" swaggertype:"object"`
}

// PopulateReply is the outcome of a populate command.
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

// Populate converts req.Data and upserts its records by sourcedId (see
// readmodel.Populate).
func (c *Client) Populate(ctx context.Context, req PopulateRequest) (*PopulateReply, error) {
	if err := req.Metadata.Check(c.converter, false); err != nil {
		return nil, err
	}
	var expiresAt *time.Time
	if req.TTL != "" {
		ttl, err := time.ParseDuration(req.TTL)
		if err != nil || ttl <= 0 {
			return nil, &domain.ValidationError{Field: "ttl", Msg: `must be a positive Go duration, e.g. "24h" or "90m"`}
		}
		expiry := c.now().UTC().Add(ttl).Truncate(time.Microsecond) // Postgres precision
		expiresAt = &expiry
	}
	parsed, err := c.converter.Parse(req.Metadata.InputFormat, req.Data, "data")
	if err != nil {
		return nil, err
	}
	items, err := c.catalog.Populate(ctx, parsed.Bundle, expiresAt)
	if err != nil {
		if len(items) > 0 {
			c.log.Error("populate stored part of a document", "stored", len(items), "err", err)
		}
		return nil, err
	}
	return &PopulateReply{Metadata: req.Metadata, ExpiresAt: expiresAt, Items: items, Warnings: parsed.Warnings}, nil
}
