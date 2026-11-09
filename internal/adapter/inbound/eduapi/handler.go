// Package eduapi is the inbound Edu-API v1.0 read provider: it publishes the
// canonical data in Edu-API shapes (read-only GET) under dto.BasePath. Entry
// points are methods on Client; route declarations live in the httpserver
// package.
package eduapi

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sort"
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

// The fields a client may sort and filter on, by the name the published
// resource carries (dot notation for nested ones, as the spec has it), and
// the stored key each one reads. A field is listed only when its stored value
// is its published value. References (stored as ids, published as sourcedIds)
// and mapped or multi-valued fields are left out: sorting or filtering on
// them would disagree with what the response shows.
var (
	sourcedIDFields = helpers.ListFields{"sourcedId": "sourcedId"}
	personFields    = helpers.ListFields{
		"sourcedId":            "sourcedId",
		"legalName.familyName": "familyName",
		"legalName.givenName":  "givenName",
		"dateOfBirth":          "dateOfBirth",
	}
)

// ListPersons publishes persons in Edu-API shape.
func (c *Client) ListPersons(gc *gin.Context) {
	c.listPersonsWith(gc, personFields, c.catalog.Persons.ListPersons)
}

// listPersonsWith answers a person list request with the page fetch returns.
// fields are the names sort and filter accept; nil rejects both.
func (c *Client) listPersonsWith(gc *gin.Context, fields helpers.ListFields, fetch func(context.Context, port.ListParams) (port.Page[domain.Person], error)) {
	params, err := c.listParams(gc, fields)
	if err != nil {
		writeError(gc, err)
		return
	}
	page, err := fetch(gc.Request.Context(), params)
	if err != nil {
		writeError(gc, err)
		return
	}
	c.writePersons(gc, params, page)
}

func (c *Client) writePersons(gc *gin.Context, params port.ListParams, page port.Page[domain.Person]) {
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

// Roles that make a person a student or staff member of an offering. The
// spec puts role on the enrollment, so these select enrollments.
var (
	studentRoles = []domain.Role{domain.RoleStudent}
	staffRoles   = []domain.Role{domain.RoleStaff, domain.RoleFaculty}
)

// ListStudents publishes the persons with the student role.
func (c *Client) ListStudents(gc *gin.Context) {
	c.listPersonsWith(gc, personFields, func(ctx context.Context, params port.ListParams) (port.Page[domain.Person], error) {
		return c.catalog.Persons.ListPersonsByRoles(ctx, studentRoles, params)
	})
}

// ListStaff publishes the persons with the staff or faculty role.
func (c *Client) ListStaff(gc *gin.Context) {
	c.listPersonsWith(gc, personFields, func(ctx context.Context, params port.ListParams) (port.Page[domain.Person], error) {
		return c.catalog.Persons.ListPersonsByRoles(ctx, staffRoles, params)
	})
}

// ListOfferingEnrollments publishes the enrollments on one course offering.
func (c *Client) ListOfferingEnrollments(gc *gin.Context) {
	offering, ok := c.publishableOffering(gc)
	if !ok {
		return
	}
	listFrom(c, gc, sourcedIDFields, func(ctx context.Context, params port.ListParams) (port.Page[*domain.Enrollment], error) {
		return c.catalog.Enrollments.ListByRef(ctx, "offeringId", offering.ID, params)
	}, each(c.mapper.Enrollment))
}

// ListOfferingStudents publishes the persons enrolled on one course offering
// as students.
func (c *Client) ListOfferingStudents(gc *gin.Context) { c.listOfferingPersons(gc, studentRoles) }

// ListOfferingStaff publishes the persons enrolled on one course offering as
// staff.
func (c *Client) ListOfferingStaff(gc *gin.Context) { c.listOfferingPersons(gc, staffRoles) }

// listOfferingPersons pages in memory, so it cannot sort or filter.
func (c *Client) listOfferingPersons(gc *gin.Context, roles []domain.Role) {
	offering, ok := c.publishableOffering(gc)
	if !ok {
		return
	}
	c.listPersonsWith(gc, nil, func(ctx context.Context, params port.ListParams) (port.Page[domain.Person], error) {
		enrollments, err := c.catalog.Enrollments.ListByRefs(ctx, "offeringId", []string{offering.ID})
		if err != nil {
			return port.Page[domain.Person]{}, err
		}
		var personIDs []string
		seen := map[string]bool{}
		for _, enrollment := range enrollments {
			if slices.Contains(roles, enrollment.Role) && !seen[enrollment.PersonID] {
				seen[enrollment.PersonID] = true
				personIDs = append(personIDs, enrollment.PersonID)
			}
		}
		persons, err := c.catalog.Persons.GetPersons(ctx, personIDs)
		if err != nil {
			return port.Page[domain.Person]{}, err
		}
		sort.Slice(persons, func(i, j int) bool {
			a, b := persons[i], persons[j]
			if a.FamilyName != b.FamilyName {
				return a.FamilyName < b.FamilyName
			}
			if a.GivenName != b.GivenName {
				return a.GivenName < b.GivenName
			}
			return a.ID < b.ID
		})
		return slicePage(persons, params), nil
	})
}

// ListSessionOfferings publishes the course offerings in one academic
// session.
func (c *Client) ListSessionOfferings(gc *gin.Context) {
	session, ok := c.sessionParent(gc)
	if !ok {
		return
	}
	listFrom(c, gc, sourcedIDFields, func(ctx context.Context, params port.ListParams) (port.Page[*domain.Offering], error) {
		return c.catalog.Offerings.ListByRef(ctx, "academicSessionId", session.ID, params)
	}, c.offeringDTOs)
}

// ListSessionEnrollments publishes the enrollments on the offerings of one
// academic session. It pages in memory, so it cannot sort or filter.
func (c *Client) ListSessionEnrollments(gc *gin.Context) {
	session, ok := c.sessionParent(gc)
	if !ok {
		return
	}
	listFrom(c, gc, nil, func(ctx context.Context, params port.ListParams) (port.Page[*domain.Enrollment], error) {
		offerings, err := c.catalog.Offerings.ListByRefs(ctx, "academicSessionId", []string{session.ID})
		if err != nil {
			return port.Page[*domain.Enrollment]{}, err
		}
		offeringIDs := make([]string, 0, len(offerings))
		for _, offering := range offerings {
			offeringIDs = append(offeringIDs, offering.ID)
		}
		if len(offeringIDs) == 0 {
			return port.Page[*domain.Enrollment]{}, nil
		}
		enrollments, err := c.catalog.Enrollments.ListByRefs(ctx, "offeringId", offeringIDs)
		if err != nil {
			return port.Page[*domain.Enrollment]{}, err
		}
		sort.Slice(enrollments, func(i, j int) bool { return enrollments[i].SourcedID < enrollments[j].SourcedID })
		return slicePage(enrollments, params), nil
	}, each(c.mapper.Enrollment))
}

// publishableOffering resolves the :id offering, answering 404 itself when it
// does not exist or is an orphan, as getWith does.
func (c *Client) publishableOffering(gc *gin.Context) (*domain.Offering, bool) {
	ctx := gc.Request.Context()
	offering, err := c.catalog.Offerings.GetBySourcedID(ctx, gc.Param("id"))
	if err != nil {
		writeError(gc, err)
		return nil, false
	}
	// The ID is not a reference, so PublishRefs leaves it alone.
	items, err := publishRefs(ctx, c, offering)
	if err != nil {
		writeError(gc, err)
		return nil, false
	}
	if len(items) == 0 {
		writeError(gc, domain.ErrNotFound)
		return nil, false
	}
	return offering, true
}

// sessionParent resolves the :id academic session, answering 404 itself.
func (c *Client) sessionParent(gc *gin.Context) (*domain.AcademicSession, bool) {
	session, err := c.catalog.AcademicSessions.GetBySourcedID(gc.Request.Context(), gc.Param("id"))
	if err != nil {
		writeError(gc, err)
		return nil, false
	}
	return session, true
}

// slicePage pages an in-memory result, for the nested routes whose rows come
// from more than one lookup. The total counts every item. offset and limit are
// never added, so a huge offset cannot overflow.
func slicePage[T any](items []T, params port.ListParams) port.Page[T] {
	lo := min(max(params.Offset, 0), len(items))
	hi := len(items)
	if params.Limit > 0 && params.Limit < hi-lo {
		hi = lo + params.Limit
	}
	return port.Page[T]{Items: items[lo:hi], Total: len(items)}
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
	listFrom(c, gc, sourcedIDFields, svc.List, toDTOs)
}

// listFrom is listWith for any source of pages, e.g. a nested route's
// ListByRef. fields are the names sort and filter accept; nil rejects both.
func listFrom[PT domain.Entity, D any](c *Client, gc *gin.Context, fields helpers.ListFields, fetch func(context.Context, port.ListParams) (port.Page[PT], error), toDTOs func(context.Context, []PT) ([]D, error)) {
	params, err := c.listParams(gc, fields)
	if err != nil {
		writeError(gc, err)
		return
	}
	ctx := gc.Request.Context()
	page, err := fetch(ctx, params)
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

// listParams parses the limit, offset, sort, orderBy and filter query
// parameters. An omitted limit takes sis.adapter.inbound.eduapi.default_limit;
// a given one must be positive and is clamped to .max_limit, so a caller can
// never request an unbounded page. sort and filter are optional in the spec:
// a list that supports them passes its fields, one that does not passes nil
// and answers either with 400.
func (c *Client) listParams(gc *gin.Context, fields helpers.ListFields) (port.ListParams, error) {
	eduapiCfg := c.cfg.SIS.Adapter.Inbound.EduAPI
	return helpers.ParseListQuery(gc, eduapiCfg.DefaultLimit, eduapiCfg.MaxLimit, fields)
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
	info := dto.NewStatusInfo(status, description)
	if apiErr.Title == helpers.ErrInvalidFilterField {
		info = info.WithMinor(dto.MinorInvalidFilterField)
	}
	gc.JSON(status, info)
}
