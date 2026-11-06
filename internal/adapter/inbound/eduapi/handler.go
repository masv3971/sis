// Package eduapi is the inbound Edu-API v1.0 read provider: it publishes the
// canonical data in Edu-API shapes (read-only GET) under dto.BasePath. Entry
// points are methods on Client; route declarations live in the httpserver
// package.
package eduapi

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	dto "github.com/SUNET/sis/pkg/eduapi"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/helpers"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// Client bundles the Edu-API handlers with the read-model Client and a
// logger. It is a "Client" rather than a "Service" because it owns no
// runtime resources and has nothing to shut down.
//
// The receiver is named c to match the vc convention; the gin.Context
// parameter is therefore named gc to avoid shadowing.
type Client struct {
	cfg     *model.Cfg
	catalog *readmodel.Client
	mapper  dto.Mapper
	log     *logger.Log
}

// New builds the eduapi Client.
func New(_ context.Context, cfg *model.Cfg, catalog *readmodel.Client, log *logger.Log) (*Client, error) {
	mapper := dto.NewMapper(cfg.SIS.Adapter.Inbound.EduAPI.RecordLanguage)
	return &Client{cfg: cfg, catalog: catalog, mapper: mapper, log: log.New("eduapi")}, nil
}

// Mapper returns the mapper that tags records with the configured language,
// for the route table.
func (c *Client) Mapper() dto.Mapper { return c.mapper }

// ListPersons publishes persons in Edu-API shape.
func (c *Client) ListPersons(gc *gin.Context) {
	params, err := c.limitOffset(gc)
	if err != nil {
		writeError(gc, err)
		return
	}
	page, err := c.catalog.Persons.ListPersons(gc.Request.Context(), params)
	if err != nil {
		writeError(gc, err)
		return
	}
	out := make([]dto.Person, 0, len(page.Items))
	for _, person := range page.Items {
		out = append(out, c.mapper.Person(person))
	}
	pageHeaders(gc, params, page.Total)
	gc.JSON(http.StatusOK, out)
}

// GetPerson publishes a single person by sourcedId in Edu-API shape.
func (c *Client) GetPerson(gc *gin.Context) {
	person, err := c.catalog.Persons.GetPersonBySourcedID(gc.Request.Context(), gc.Param("id"))
	if err != nil {
		writeError(gc, err)
		return
	}
	gc.JSON(http.StatusOK, c.mapper.Person(person))
}

// ListCourseOfferings publishes offerings as course offerings. The dates the
// spec requires come from each offering's academic session.
func (c *Client) ListCourseOfferings(gc *gin.Context) {
	listWith(c, gc, c.catalog.Offerings, c.offeringDTOs)
}

// GetCourseOffering publishes one offering by sourcedId.
func (c *Client) GetCourseOffering(gc *gin.Context) {
	getWith(c, gc, c.catalog.Offerings, c.offeringDTOs)
}

// offeringDTOs maps offerings, whose course and academic session references
// are already sourcedIds, with one batched lookup each. The session supplies
// the dates and the course the title of an untitled offering.
func (c *Client) offeringDTOs(ctx context.Context, offerings []*domain.Offering) ([]dto.CourseOffering, error) {
	var sessionIDs, courseIDs []string
	for _, offering := range offerings {
		if offering.AcademicSessionID != "" {
			sessionIDs = append(sessionIDs, offering.AcademicSessionID)
		}
		if offering.Title == "" && offering.CourseID != "" {
			courseIDs = append(courseIDs, offering.CourseID)
		}
	}
	sessions := map[string]*domain.AcademicSession{}
	if len(sessionIDs) > 0 {
		found, err := c.catalog.AcademicSessions.GetBySourcedIDs(ctx, sessionIDs)
		if err != nil {
			return nil, err
		}
		for _, session := range found {
			sessions[session.SourcedID] = session
		}
	}
	courseTitles := map[string]string{}
	if len(courseIDs) > 0 {
		found, err := c.catalog.Courses.GetBySourcedIDs(ctx, courseIDs)
		if err != nil {
			return nil, err
		}
		for _, course := range found {
			courseTitles[course.SourcedID] = course.Title
		}
	}
	out := make([]dto.CourseOffering, 0, len(offerings))
	for _, offering := range offerings {
		out = append(out, c.mapper.CourseOffering(offering, sessions[offering.AcademicSessionID], courseTitles[offering.CourseID]))
	}
	return out, nil
}

// The generic handlers below could not be methods on *Client: Go does not
// allow type parameters on methods, so they are free functions that take
// *Client as the first argument. Each returns a gin.HandlerFunc the
// httpserver can mount directly.

// each lifts a per-record mapper to a batch mapper.
func each[PT, D any](toDTO func(PT) D) func(context.Context, []PT) ([]D, error) {
	return func(_ context.Context, items []PT) ([]D, error) {
		out := make([]D, 0, len(items))
		for _, e := range items {
			out = append(out, toDTO(e))
		}
		return out, nil
	}
}

// List builds a list handler for an entity type, mapping each item.
func List[T any, PT domain.EntityPtr[T], D any](c *Client, svc *service.Resource[T, PT], toDTO func(PT) D) gin.HandlerFunc {
	return func(gc *gin.Context) { listWith(c, gc, svc, each(toDTO)) }
}

// Get builds a get-by-sourcedId handler for an entity type.
func Get[T any, PT domain.EntityPtr[T], D any](c *Client, svc *service.Resource[T, PT], toDTO func(PT) D) gin.HandlerFunc {
	return func(gc *gin.Context) { getWith(c, gc, svc, each(toDTO)) }
}

// listWith answers a list request with a bare JSON array, as the spec
// requires, and X-Total-Count.
func listWith[T any, PT domain.EntityPtr[T], D any](c *Client, gc *gin.Context, svc *service.Resource[T, PT], toDTOs func(context.Context, []PT) ([]D, error)) {
	params, err := c.limitOffset(gc)
	if err != nil {
		writeError(gc, err)
		return
	}
	ctx := gc.Request.Context()
	page, err := svc.List(ctx, params)
	if err != nil {
		writeError(gc, err)
		return
	}
	items, err := publishRefs(ctx, c, page.Items...)
	if err != nil {
		writeError(gc, err)
		return
	}
	out, err := toDTOs(ctx, items)
	if err != nil {
		writeError(gc, err)
		return
	}
	pageHeaders(gc, params, page.Total)
	gc.JSON(http.StatusOK, out)
}

func getWith[T any, PT domain.EntityPtr[T], D any](c *Client, gc *gin.Context, svc *service.Resource[T, PT], toDTOs func(context.Context, []PT) ([]D, error)) {
	ctx := gc.Request.Context()
	e, err := svc.GetBySourcedID(ctx, gc.Param("id"))
	if err != nil {
		writeError(gc, err)
		return
	}
	items, err := publishRefs(ctx, c, e)
	if err != nil {
		writeError(gc, err)
		return
	}
	if len(items) == 0 {
		writeError(gc, domain.ErrNotFound)
		return
	}
	out, err := toDTOs(ctx, items)
	if err != nil {
		writeError(gc, err)
		return
	}
	gc.JSON(http.StatusOK, out[0])
}

// publishRefs rewrites the items' references to other records into those
// records' sourcedIds (see readmodel.Client.PublishRefs), so every reference
// in the response can be followed through this provider, and returns the
// items that are publishable: orphans (a required reference no longer
// resolves) are dropped, so a page may hold fewer than limit items while
// X-Total-Count still counts stored records. A free function for the same
// reason as List/Get: it is generic over PT.
func publishRefs[PT domain.Entity](ctx context.Context, c *Client, items ...PT) ([]PT, error) {
	entities := make([]domain.Entity, len(items))
	for i, e := range items {
		entities[i] = e
	}
	publishable, err := c.catalog.PublishRefs(ctx, entities...)
	if err != nil {
		return nil, err
	}
	out := make([]PT, 0, len(items))
	for i, e := range items {
		if publishable[i] {
			out = append(out, e)
		}
	}
	return out, nil
}

// pageHeaders sets X-Total-Count (stored records) and the RFC 8288 Link
// header the spec declares (first, prev, next, last). The links carry the
// limit actually applied, which max_limit may have lowered, so a client that
// follows rel="next" never skips records; advancing offset by the limit it
// asked for would. Orphans are dropped from a page, so a page can be short
// without being the last: rely on next, not on the page length.
func pageHeaders(gc *gin.Context, params port.ListParams, total int) {
	gc.Header("X-Total-Count", strconv.Itoa(total))

	limit, offset := params.Limit, params.Offset
	links := []string{pageLink(gc, "first", limit, 0)}
	if offset > 0 {
		links = append(links, pageLink(gc, "prev", limit, max(0, offset-limit)))
	}
	if offset < total && limit < total-offset {
		links = append(links, pageLink(gc, "next", limit, offset+limit))
	}
	if total > 0 {
		links = append(links, pageLink(gc, "last", limit, (total-1)/limit*limit))
	}
	gc.Header("Link", strings.Join(links, ", "))
}

// pageLink is one Link entry for the request's own path and query, with limit
// and offset replaced. The reference is relative to the request, which
// RFC 8288 allows, so it survives a reverse proxy.
func pageLink(gc *gin.Context, rel string, limit, offset int) string {
	query := gc.Request.URL.Query()
	query.Set("limit", strconv.Itoa(limit))
	query.Set("offset", strconv.Itoa(offset))
	return fmt.Sprintf("<%s?%s>; rel=%q", gc.Request.URL.EscapedPath(), query.Encode(), rel)
}

// limitOffset parses the limit/offset query parameters. An omitted limit
// takes sis.adapter.inbound.eduapi.default_limit; a given one must be
// positive and is clamped to .max_limit, so a caller can never request an
// unbounded page. sort and filter are optional in the spec and answered
// with 400.
func (c *Client) limitOffset(gc *gin.Context) (port.ListParams, error) {
	eduapiCfg := c.cfg.SIS.Adapter.Inbound.EduAPI
	return helpers.ParseLimitOffset(gc, eduapiCfg.DefaultLimit, eduapiCfg.MaxLimit)
}

// AuthError renders a rejected token or scope as the spec's error body, for
// auth.RenderErrors on the Edu-API route group.
func (c *Client) AuthError(gc *gin.Context, status int, msg string) {
	gc.JSON(status, dto.NewStatusInfo(status, msg))
}

// writeError answers with the spec's error body, Imsx_StatusInfo, for the
// status the shared classifier picks. The detail of an unclassified error is
// dropped, as in helpers.WriteError.
func writeError(gc *gin.Context, err error) {
	apiErr := helpers.NewErrorFromError(err)
	if apiErr == nil {
		apiErr = helpers.NewErrorWithStatus(helpers.ErrInternal, http.StatusInternalServerError)
	}
	if apiErr.IsUnclassified() {
		_ = gc.Error(err)
		apiErr = helpers.NewErrorWithStatus(apiErr.Title, apiErr.Status())
	}
	description := apiErr.Title
	if apiErr.Err != nil {
		description = fmt.Sprintf("%s: %v", apiErr.Title, apiErr.Err)
	}
	status := apiErr.Status()
	gc.JSON(status, dto.NewStatusInfo(status, description))
}
