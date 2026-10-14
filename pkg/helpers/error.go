// error.go is the central place for API errors: the structured Error type, the
// single classifier that maps any error onto a title/status/detail, and the one
// renderer that writes it as HTTP. Handlers call WriteError instead of choosing
// status codes and JSON shapes themselves, so error behaviour lives in one file.
// Structure mirrors SUNET/vc's pkg/helpers/error.go, reimplemented for sis.
package helpers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/SUNET/sis/internal/core/domain"
)

// Titles are the stable, machine-readable error codes clients can switch on.
// They stay constant even if the human-facing wording around them changes.
const (
	ErrValidation   = "validation_error"
	ErrBadRequest   = "invalid_request"
	ErrUnauthorized = "unauthorized"
	ErrNotFound     = "not_found"
	ErrConflict     = "conflict"
	ErrInternal     = "internal_server_error"
)

// Error is a structured, renderable API error. Title is the stable code; Err is
// optional detail that is safe to return to the client; HTTPStatus is the status
// to send, with 0 meaning "derive it from Title".
type Error struct {
	Title      string `json:"title"`
	Err        any    `json:"details,omitempty"`
	HTTPStatus int    `json:"-"`

	// unclassified marks an Error the classifier built from a Go error it
	// recognised nothing about: its detail is just the raw error string, so the
	// renderer redacts it rather than leak internals. Unexported so only this
	// package sets it, and invisible to JSON.
	unclassified bool
}

// ErrorResponse is the JSON envelope for an error: {"error": {...}}.
type ErrorResponse struct {
	Error *Error `json:"error"`
}

// NewError creates an Error with only a title.
func NewError(title string) *Error { return &Error{Title: title} }

// NewErrorDetails creates an Error with a title and detail.
func NewErrorDetails(title string, err any) *Error { return &Error{Title: title, Err: err} }

// NewErrorWithStatus creates an Error with a title and explicit HTTP status.
func NewErrorWithStatus(title string, status int) *Error {
	return &Error{Title: title, HTTPStatus: status}
}

// NewErrorDetailsWithStatus creates an Error with a title, detail and explicit
// HTTP status.
func NewErrorDetailsWithStatus(title string, err any, status int) *Error {
	return &Error{Title: title, Err: err, HTTPStatus: status}
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("[%s] %+v", e.Title, e.Err)
	}
	return fmt.Sprintf("[%s]", e.Title)
}

// IsUnclassified reports whether this Error was built from an unrecognised Go
// error, meaning its detail is raw and must not be returned to the client.
func (e *Error) IsUnclassified() bool { return e != nil && e.unclassified }

// Status returns the HTTP status for the error: the explicit HTTPStatus when
// set, otherwise one derived from Title, defaulting to 500.
func (e *Error) Status() int {
	if e == nil {
		return http.StatusInternalServerError
	}
	if e.HTTPStatus != 0 {
		return e.HTTPStatus
	}
	switch e.Title {
	case ErrValidation, ErrBadRequest:
		return http.StatusBadRequest
	case ErrUnauthorized:
		return http.StatusUnauthorized
	case ErrNotFound:
		return http.StatusNotFound
	case ErrConflict:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// NewErrorFromError classifies err into a renderable Error. An error that is
// already an *Error passes through; request-binding failures (validation and
// malformed JSON) and the domain sentinels get a stable title, status and safe
// detail. Anything unrecognised becomes an unclassified internal error whose
// detail the renderer redacts.
func NewErrorFromError(err error) *Error {
	if err == nil {
		return nil
	}

	// Already an *Error (possibly wrapped with %w): keep it as chosen.
	if apiErr, ok := errors.AsType[*Error](err); ok {
		return apiErr
	}

	// Request binding: validation and malformed JSON are the client's fault.
	if fieldErrors, ok := errors.AsType[validator.ValidationErrors](err); ok {
		return &Error{Title: ErrValidation, Err: validationDetails(fieldErrors), HTTPStatus: http.StatusBadRequest}
	}
	if typeErr, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
		return &Error{
			Title:      ErrBadRequest,
			Err:        map[string]any{"field": typeErr.Field, "expected": typeErr.Type.String()},
			HTTPStatus: http.StatusBadRequest,
		}
	}
	if syntaxErr, ok := errors.AsType[*json.SyntaxError](err); ok {
		return &Error{
			Title:      ErrBadRequest,
			Err:        map[string]any{"offset": syntaxErr.Offset, "error": syntaxErr.Error()},
			HTTPStatus: http.StatusBadRequest,
		}
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return &Error{Title: ErrBadRequest, Err: "empty or truncated request body", HTTPStatus: http.StatusBadRequest}
	}

	// Domain validation and sentinels.
	if domErr, ok := errors.AsType[*domain.ValidationError](err); ok {
		return &Error{
			Title:      ErrValidation,
			Err:        map[string]any{"field": domErr.Field, "error": domErr.Msg},
			HTTPStatus: http.StatusBadRequest,
		}
	}
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return &Error{Title: ErrNotFound, HTTPStatus: http.StatusNotFound}
	case errors.Is(err, domain.ErrConflict):
		return &Error{Title: ErrConflict, HTTPStatus: http.StatusConflict}
	}

	return &Error{Title: ErrInternal, Err: err.Error(), HTTPStatus: http.StatusInternalServerError, unclassified: true}
}

// WriteError classifies err and writes it as the HTTP error response. The detail
// of an unclassified error is dropped so internal failures leak nothing beyond a
// generic title.
func WriteError(c *gin.Context, err error) {
	e := NewErrorFromError(err)
	if e == nil {
		e = NewErrorWithStatus(ErrInternal, http.StatusInternalServerError)
	}
	if e.IsUnclassified() {
		e = NewErrorWithStatus(e.Title, e.Status())
	}
	c.JSON(e.Status(), ErrorResponse{Error: e})
}

// validationDetails renders validator field failures as structured, safe detail.
func validationDetails(fieldErrors validator.ValidationErrors) []map[string]any {
	out := make([]map[string]any, 0, len(fieldErrors))
	for _, e := range fieldErrors {
		out = append(out, map[string]any{
			"field":      e.Field(),
			"namespace":  e.Namespace(),
			"validation": e.Tag(),
			"param":      e.Param(),
		})
	}
	return out
}
