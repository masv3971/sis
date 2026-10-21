// error.go is the central place for API errors: the structured Error type, the
// single classifier that maps any error onto a title/status/detail, and the one
// renderer that writes it as HTTP. Handlers call WriteError instead of choosing
// status codes and JSON shapes themselves, so error behaviour lives in one file.
// Structure mirrors SUNET/vc's pkg/helpers/error.go, reimplemented for sis.
package helpers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	"github.com/SUNET/sis/internal/core/domain"
)

// Titles are the stable, machine-readable error codes clients can switch on.
// They stay constant even if the human-facing wording around them changes.
const (
	ErrValidation   = "validation_error"
	ErrBadRequest   = "invalid_request"
	ErrUnauthorized = "unauthorized"
	ErrForbidden    = "forbidden"
	ErrNotFound     = "not_found"
	ErrConflict     = "conflict"
	ErrTooLarge     = "request_too_large"
	ErrUnavailable  = "unavailable"
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
	case ErrForbidden:
		return http.StatusForbidden
	case ErrNotFound:
		return http.StatusNotFound
	case ErrConflict:
		return http.StatusConflict
	case ErrTooLarge:
		return http.StatusRequestEntityTooLarge
	case ErrUnavailable:
		return http.StatusServiceUnavailable
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
	case errors.Is(err, context.DeadlineExceeded):
		// The operation's deadline (request, command) passed; retrying may
		// succeed, so 503 rather than 500.
		return &Error{Title: ErrUnavailable, Err: MsgTimedOut, HTTPStatus: http.StatusServiceUnavailable}
	}

	return &Error{Title: ErrInternal, Err: err.Error(), HTTPStatus: http.StatusInternalServerError, unclassified: true}
}

// BindJSON decodes the request body into v and returns a classified *Error on
// failure. Every decode failure is the client's fault, so anything
// NewErrorFromError does not recognise (strict-mode unknown fields, a
// time.Time that fails to parse, ...) becomes ErrBadRequest (400) with the
// decoder message as detail instead of an unclassified 500. An oversized body
// (http.MaxBytesReader) is 413. Call it only on the request body: service
// errors must keep going through WriteError's own classification.
//
// The whole body is read before decoding, and it must hold exactly one JSON
// value (surrounding whitespace allowed). gin's own binding decodes only the
// first value and never reads the rest, so without this a valid object
// followed by garbage or a second object would be accepted, and trailing
// bytes past the body limit would never trip the 413.
//
// The read is bound by the request context too: when it ends (the request's
// work deadline, see httpserver.requestTimeout), the connection's read
// deadline is pulled in to now, which interrupts a body still arriving. It
// is never pushed later, so sis.httpserver.read_timeout still applies. A
// request whose context has ended gets its ctx error (deadline: 503), never
// decoded input, so a command is not dispatched after the deadline.
func BindJSON(gc *gin.Context, v any) error {
	if gc.Request == nil || gc.Request.Body == nil {
		return DecodeJSON(nil, v)
	}
	ctx := gc.Request.Context()
	stop := context.AfterFunc(ctx, func() {
		// ErrNotSupported outside a real server (httptest recorders);
		// the ctx check below still applies there.
		_ = http.NewResponseController(gc.Writer).SetReadDeadline(time.Now())
	})
	body, err := io.ReadAll(gc.Request.Body)
	stop()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("read request body: %w", ctxErr)
	}
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return &Error{Title: ErrTooLarge, HTTPStatus: http.StatusRequestEntityTooLarge}
		}
		return decodeError(err)
	}
	return DecodeJSON(body, v)
}

// DecodeJSON decodes body, which must hold exactly one JSON value, into v,
// rejecting unknown fields and checking `binding` tags, and returns a
// classified *Error (400) on failure. It is BindJSON without the HTTP
// request, for transports that hand over a whole message (e.g. AMQP).
func DecodeJSON(body []byte, v any) error {
	if err := decodeSingleJSON(body, v); err != nil {
		return decodeError(err)
	}
	return nil
}

// decodeError classifies a body read or decode failure, which is always the
// client's fault (400).
func decodeError(err error) error {
	if apiErr := NewErrorFromError(err); !apiErr.IsUnclassified() {
		return apiErr
	}
	// The decoder's own messages name the offending field or value and carry
	// no server internals, so they are safe to return.
	return &Error{Title: ErrBadRequest, Err: err.Error(), HTTPStatus: http.StatusBadRequest}
}

// errTrailingJSON is returned when a body holds more than one JSON value. It
// is an *Error so DecodeJSON passes it through as a 400 unchanged.
var errTrailingJSON = &Error{
	Title:      ErrBadRequest,
	Err:        "request body must contain a single JSON value",
	HTTPStatus: http.StatusBadRequest,
}

// decodeSingleJSON rejects anything but a single JSON value, then decodes
// strictly and validates with gin's validator, so `binding` tags behave as
// with ShouldBindJSON under EnableJsonDecoderDisallowUnknownFields.
func decodeSingleJSON(body []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	var first json.RawMessage
	if err := dec.Decode(&first); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errTrailingJSON
	}
	strict := json.NewDecoder(bytes.NewReader(first))
	strict.DisallowUnknownFields()
	if err := strict.Decode(v); err != nil {
		return err
	}
	if binding.Validator == nil {
		return nil
	}
	return binding.Validator.ValidateStruct(v)
}

// WriteError classifies err and writes it as the HTTP error response. The detail
// of an unclassified error is dropped so internal failures leak nothing beyond a
// generic title. Before redacting, the original error is attached to the gin
// context (gc.Errors) so the request logger records it server-side; otherwise
// a database failure would surface only as an unexplained 500.
func WriteError(gc *gin.Context, err error) {
	apiErr := NewErrorFromError(err)
	if apiErr == nil {
		apiErr = NewErrorWithStatus(ErrInternal, http.StatusInternalServerError)
	}
	if apiErr.IsUnclassified() {
		_ = gc.Error(err)
		apiErr = NewErrorWithStatus(apiErr.Title, apiErr.Status())
	}
	gc.JSON(apiErr.Status(), ErrorResponse{Error: apiErr})
}

// validationDetails renders validator field failures as structured, safe detail.
func validationDetails(fieldErrors validator.ValidationErrors) []map[string]any {
	out := make([]map[string]any, 0, len(fieldErrors))
	for _, fieldErr := range fieldErrors {
		out = append(out, map[string]any{
			"field":      fieldErr.Field(),
			"namespace":  fieldErr.Namespace(),
			"validation": fieldErr.Tag(),
			"param":      fieldErr.Param(),
		})
	}
	return out
}
