package helpers

import (
	"math"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/core/port"
)

// ParseLimitOffset parses OneRoster-style limit/offset query parameters into
// port.ListParams. An absent limit takes defaultLimit; a present one must be a
// positive integer. Either way the result is clamped to maxLimit (the
// calling API's sis.adapter.inbound.<api>.max_limit). Repositories treat a non-positive Limit as
// "unbounded", so this is where the bound is enforced. offset must be a
// non-negative integer. Malformed values are a 400 rather than silently
// ignored.
//
// OneRoster's sort and filter are not implemented by any repository yet, so a
// non-empty value is a 400 too: answering with unfiltered rows in the default
// order would look like success while ignoring what the client asked for.
func ParseLimitOffset(gc *gin.Context, defaultLimit, maxLimit int) (port.ListParams, error) {
	defaultLimit, maxLimit, err := checkBounds(defaultLimit, maxLimit)
	if err != nil {
		return port.ListParams{}, err
	}
	for _, name := range unsupportedListParams {
		if gc.Query(name) != "" {
			return port.ListParams{}, pagingError(name, "not supported")
		}
	}
	params := port.ListParams{Limit: defaultLimit}
	if raw, ok := gc.GetQuery("limit"); ok {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			return port.ListParams{}, pagingError("limit", "must be a positive integer")
		}
		params.Limit = min(limit, maxLimit)
	}
	if raw, ok := gc.GetQuery("offset"); ok {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return port.ListParams{}, pagingError("offset", "must be a non-negative integer")
		}
		params.Offset = offset
	}
	return params, nil
}

// unsupportedListParams are OneRoster list query parameters that are rejected
// until both backends implement them consistently.
var unsupportedListParams = []string{"sort", "filter"}

// ParsePageNumberSize parses OEAPI-style 1-based pageNumber/pageSize query
// parameters. pageSize is clamped to maxPageSize
// (sis.adapter.inbound.oeapi.max_page_size), and a pageNumber whose offset
// (pageNumber-1)*pageSize would overflow int is rejected. It returns the
// effective pageNumber/pageSize (for the response envelope) alongside the
// equivalent ListParams.
func ParsePageNumberSize(gc *gin.Context, defaultPageSize, maxPageSize int) (pageNumber, pageSize int, params port.ListParams, err error) {
	if defaultPageSize, maxPageSize, err = checkBounds(defaultPageSize, maxPageSize); err != nil {
		return 0, 0, port.ListParams{}, err
	}
	pageNumber, pageSize = 1, defaultPageSize
	if raw, ok := gc.GetQuery("pageNumber"); ok {
		number, convErr := strconv.Atoi(raw)
		if convErr != nil || number < 1 {
			return 0, 0, port.ListParams{}, pagingError("pageNumber", "must be a positive integer")
		}
		pageNumber = number
	}
	if raw, ok := gc.GetQuery("pageSize"); ok {
		size, convErr := strconv.Atoi(raw)
		if convErr != nil || size < 1 {
			return 0, 0, port.ListParams{}, pagingError("pageSize", "must be a positive integer")
		}
		pageSize = min(size, maxPageSize)
	}
	if pageNumber-1 > math.MaxInt/pageSize {
		return 0, 0, port.ListParams{}, pagingError("pageNumber", "out of range")
	}
	params = port.ListParams{Limit: pageSize, Offset: (pageNumber - 1) * pageSize}
	return pageNumber, pageSize, params, nil
}

// checkBounds fails closed on a non-positive max (which repositories would
// read as "unbounded") and clamps the default into [1, max]. Config
// validation already enforces both > 0; this guards callers that build a
// model.Cfg by hand (e.g. tests) without defaults applied.
func checkBounds(defaultSize, maxSize int) (int, int, error) {
	if maxSize < 1 {
		return 0, 0, NewErrorWithStatus(ErrInternal, http.StatusInternalServerError)
	}
	if defaultSize < 1 || defaultSize > maxSize {
		defaultSize = maxSize
	}
	return defaultSize, maxSize, nil
}

func pagingError(field, msg string) *Error {
	return NewErrorDetailsWithStatus(ErrValidation,
		map[string]any{"field": field, "error": msg}, http.StatusBadRequest)
}
