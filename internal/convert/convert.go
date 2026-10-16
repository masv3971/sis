// Package convert translates documents between external formats and the
// canonical domain. Parsers turn an input format (ELM today) into a
// domain.Bundle; renderers turn a bundle into an output format (Edu-API,
// OEAPI). Adding a format is one registry entry, so the HTTP surface
// (/api/v1/parse, /api/v1/populate) does not change.
package convert

import (
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"

	edudto "github.com/SUNET/sis/pkg/eduapi"
	oedto "github.com/SUNET/sis/pkg/oeapi"
)

// Format names, as used in the request metadata.
const (
	FormatELM    = "elm"
	FormatEduAPI = "eduapi"
	FormatOEAPI  = "oeapi"
)

// Parsed is the result of parsing a document: the converted records and
// notes about parts of the document that were skipped.
type Parsed struct {
	Bundle   *domain.Bundle
	Warnings []string
}

// parser converts data to canonical records. path is the request location of
// data (e.g. "data"), used to name the offending part in errors.
type parser func(data []byte, path string) (*Parsed, error)

// renderer converts canonical records to an output format.
type renderer func(*domain.Bundle) any

// Client holds the format registries. It is a "Client" rather than a
// "Service" because it owns no runtime resources: conversion is pure.
type Client struct {
	parsers   map[string]parser
	renderers map[string]renderer
	log       *logger.Log
}

// New builds the convert Client with every supported format registered.
func New(_ context.Context, _ *model.Cfg, log *logger.Log) (*Client, error) {
	return &Client{
		parsers: map[string]parser{FormatELM: parseELM},
		renderers: map[string]renderer{
			FormatEduAPI: func(b *domain.Bundle) any { return edudto.FromBundle(b) },
			FormatOEAPI:  func(b *domain.Bundle) any { return oedto.FromBundle(b) },
		},
		log: log.New("convert"),
	}, nil
}

// InputFormats returns the supported input formats, sorted.
func (c *Client) InputFormats() []string { return slices.Sorted(maps.Keys(c.parsers)) }

// OutputFormats returns the supported output formats, sorted.
func (c *Client) OutputFormats() []string { return slices.Sorted(maps.Keys(c.renderers)) }

// HasInput reports whether format can be parsed.
func (c *Client) HasInput(format string) bool { _, ok := c.parsers[format]; return ok }

// HasOutput reports whether format can be rendered.
func (c *Client) HasOutput(format string) bool { _, ok := c.renderers[format]; return ok }

// Parse converts data in format to canonical records. format must satisfy
// HasInput. Errors are the client's fault: malformed JSON (json.SyntaxError
// and friends) or a *domain.ValidationError naming the document path.
func (c *Client) Parse(format string, data []byte, path string) (*Parsed, error) {
	p, ok := c.parsers[format]
	if !ok {
		return nil, &domain.ValidationError{Field: "input_format", Msg: "unsupported"}
	}
	parsed, err := p(data, path)
	if err != nil {
		// Every parse failure is the document's fault: report it as a
		// validation error on the document rather than an internal one.
		if _, ok := errors.AsType[*domain.ValidationError](err); ok {
			return nil, err
		}
		return nil, &domain.ValidationError{Field: path, Msg: err.Error()}
	}
	if parsed.Warnings == nil {
		parsed.Warnings = []string{}
	}
	return parsed, nil
}

// Render converts b to format, which must satisfy HasOutput.
func (c *Client) Render(format string, b *domain.Bundle) (any, error) {
	r, ok := c.renderers[format]
	if !ok {
		return nil, &domain.ValidationError{Field: "output_format", Msg: "unsupported"}
	}
	return r(b), nil
}
