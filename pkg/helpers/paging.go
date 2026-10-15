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
func ParseLimitOffset(gc *gin.Context, defaultLimit, maxLimit int) (port.ListParams, error) {
	defaultLimit, maxLimit, err := checkBounds(defaultLimit, maxLimit)
	if err != nil {
		return port.ListParams{}, err
	}
	params := port.ListParams{Limit: defaultLimit, Sort: gc.Query("sort"), Filter: gc.Query("filter")}
	if v, ok := gc.GetQuery("limit"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return port.ListParams{}, pagingError("limit", "must be a positive integer")
		}
		params.Limit = min(n, maxLimit)
	}
	if v, ok := gc.GetQuery("offset"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return port.ListParams{}, pagingError("offset", "must be a non-negative integer")
		}
		params.Offset = n
	}
	return params, nil
}

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
	if v, ok := gc.GetQuery("pageNumber"); ok {
		n, convErr := strconv.Atoi(v)
		if convErr != nil || n < 1 {
			return 0, 0, port.ListParams{}, pagingError("pageNumber", "must be a positive integer")
		}
		pageNumber = n
	}
	if v, ok := gc.GetQuery("pageSize"); ok {
		n, convErr := strconv.Atoi(v)
		if convErr != nil || n < 1 {
			return 0, 0, port.ListParams{}, pagingError("pageSize", "must be a positive integer")
		}
		pageSize = min(n, maxPageSize)
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
func checkBounds(def, maxSize int) (int, int, error) {
	if maxSize < 1 {
		return 0, 0, NewErrorWithStatus(ErrInternal, http.StatusInternalServerError)
	}
	if def < 1 || def > maxSize {
		def = maxSize
	}
	return def, maxSize, nil
}

func pagingError(field, msg string) *Error {
	return NewErrorDetailsWithStatus(ErrValidation,
		map[string]any{"field": field, "error": msg}, http.StatusBadRequest)
}
