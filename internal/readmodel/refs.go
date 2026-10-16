package readmodel

import (
	"context"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
)

// References between records (organization parent, course org, offering
// course/session/org, enrollment person/offering) are stored as the
// referenced record's canonical id, and only as that: ids and sourcedIds are
// independently unique, so the same string can be one record's id and another
// record's sourcedId, and a reference that may hold either is ambiguous.
// CheckRefs enforces the single namespace on ingress; PublishRefs translates
// it to sourcedIds on egress.
//
// Nothing stops a referenced record from being deleted later (the document
// store has no foreign keys, and a delete-time check would race a concurrent
// create), so egress must cope with references that no longer resolve. An
// optional reference is simply dropped; a record whose required reference is
// gone is an orphan and is not published at all (see PublishRefs).

// ref is one reference field of an entity.
type ref struct {
	typ      string  // referenced entity type
	field    string  // JSON field name, for validation errors
	ptr      *string // the field itself
	required bool    // the entity is meaningless without it (mirrors Validate)
}

func refsOf(e domain.Entity) []ref {
	switch v := e.(type) {
	case *domain.Organization:
		return []ref{{"organization", "parentId", &v.ParentID, false}}
	case *domain.Course:
		return []ref{{"organization", "orgId", &v.OrgID, false}}
	case *domain.Offering:
		return []ref{
			{"course", "courseId", &v.CourseID, true},
			{"academicSession", "academicSessionId", &v.AcademicSessionID, false},
			{"organization", "orgId", &v.OrgID, false},
		}
	case *domain.Enrollment:
		return []ref{
			{"person", "personId", &v.PersonID, true},
			{"offering", "offeringId", &v.OfferingID, true},
		}
	}
	return nil
}

type refKey struct{ typ, id string }

// CheckRefs rejects an entity whose references are not the canonical ids of
// existing records, with a domain.ValidationError naming the first offending
// field. Call it on ingress (create/update) so stored references are always
// canonical ids: a sourcedId, or an id that matches nothing, is refused
// rather than stored and guessed at later. An orphaned offering (its course
// is gone) counts as missing, since nothing referencing it could be
// published.
func (c *Client) CheckRefs(ctx context.Context, e domain.Entity) error {
	refs := refsOf(e)
	found, err := c.resolve(ctx, refs)
	if err != nil {
		return err
	}
	for _, r := range refs {
		if *r.ptr == "" {
			continue
		}
		if _, ok := found[refKey{r.typ, *r.ptr}]; !ok {
			return &domain.ValidationError{Field: r.field, Msg: "must be the id of an existing " + r.typ}
		}
	}
	return nil
}

// PublishRefs rewrites the references each entity holds from canonical ids to
// the referenced records' sourcedIds. The standards providers publish and
// look up every resource by sourcedId, so a reference must carry the
// sourcedId to be followable.
//
// Call it in the provider layer, before mapping to a DTO. It mutates the
// given values, which are fresh copies from the repository. A reference that
// matches no record (e.g. its target was deleted) is cleared so a broken
// link is never published; it is never reinterpreted as a sourcedId.
//
// The result reports, per entity, whether it is publishable. It is false for
// an orphan: a record whose required reference (enrollment personId or
// offeringId, offering courseId) no longer resolves, including an enrollment
// whose offering is itself an orphan. Providers must omit orphans from lists
// and answer 404 for them, since publishing them would emit a mandatory
// relationship clients cannot follow.
//
// Lookups are batched: one repository call per referenced entity type (plus
// one for the courses of referenced offerings), so a page costs at most six
// lookups however many records it holds.
func (c *Client) PublishRefs(ctx context.Context, entities ...domain.Entity) ([]bool, error) {
	per := make([][]ref, len(entities))
	var refs []ref
	for i, e := range entities {
		per[i] = refsOf(e)
		refs = append(refs, per[i]...)
	}
	found, err := c.resolve(ctx, refs)
	if err != nil {
		return nil, err
	}
	publishable := make([]bool, len(entities))
	for i, rs := range per {
		publishable[i] = true
		for _, r := range rs {
			if *r.ptr == "" {
				continue
			}
			sid, ok := found[refKey{r.typ, *r.ptr}]
			if !ok && r.required {
				publishable[i] = false
			}
			*r.ptr = sid
		}
	}
	return publishable, nil
}

// resolve looks up every distinct non-empty reference in refs, one batched
// call per type, and maps each one that exists to its record's sourcedId.
func (c *Client) resolve(ctx context.Context, refs []ref) (map[refKey]string, error) {
	byType := map[string][]string{}
	seen := map[refKey]bool{}
	for _, r := range refs {
		k := refKey{r.typ, *r.ptr}
		if k.id == "" || seen[k] {
			continue
		}
		seen[k] = true
		byType[r.typ] = append(byType[r.typ], k.id)
	}
	found := make(map[refKey]string, len(seen))
	for typ, ids := range byType {
		sids, err := c.sourcedIDs(ctx, typ, ids)
		if err != nil {
			return nil, err
		}
		for id, sid := range sids {
			found[refKey{typ, id}] = sid
		}
	}
	return found, nil
}

// sourcedIDs maps each id in ids that names an existing typ record to that
// record's sourcedId, in a single repository call.
func (c *Client) sourcedIDs(ctx context.Context, typ string, ids []string) (map[string]string, error) {
	switch typ {
	case "person":
		ps, err := c.Persons.GetPersons(ctx, ids)
		if err != nil {
			return nil, err
		}
		out := make(map[string]string, len(ps))
		for _, p := range ps {
			out[p.ID] = p.SourcedID
		}
		return out, nil
	case "organization":
		return entitySourcedIDs(ctx, c.Organizations, ids)
	case "academicSession":
		return entitySourcedIDs(ctx, c.AcademicSessions, ids)
	case "course":
		return entitySourcedIDs(ctx, c.Courses, ids)
	case "offering":
		return c.offeringSourcedIDs(ctx, ids)
	}
	panic("readmodel: unknown reference type " + typ)
}

// offeringSourcedIDs is entitySourcedIDs for offerings, except that an
// orphaned offering (its required course is gone) does not resolve: the
// providers do not publish it, so a reference to it could not be followed.
// Costs one extra batched lookup for the offerings' courses.
func (c *Client) offeringSourcedIDs(ctx context.Context, ids []string) (map[string]string, error) {
	offerings, err := c.Offerings.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	courseIDs := make([]string, 0, len(offerings))
	for _, o := range offerings {
		courseIDs = append(courseIDs, o.CourseID)
	}
	courses, err := entitySourcedIDs(ctx, c.Courses, courseIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(offerings))
	for _, o := range offerings {
		if _, ok := courses[o.CourseID]; ok {
			out[o.ID] = o.SourcedID
		}
	}
	return out, nil
}

func entitySourcedIDs[T any, PT domain.EntityPtr[T]](ctx context.Context, svc *service.Resource[T, PT], ids []string) (map[string]string, error) {
	es, err := svc.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(es))
	for _, e := range es {
		out[e.GetID()] = e.GetSourcedID()
	}
	return out, nil
}
