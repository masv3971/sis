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

// ref is one reference field of an entity.
type ref struct {
	typ   string  // referenced entity type
	field string  // JSON field name, for validation errors
	ptr   *string // the field itself
}

func refsOf(e domain.Entity) []ref {
	switch v := e.(type) {
	case *domain.Organization:
		return []ref{{"organization", "parentId", &v.ParentID}}
	case *domain.Course:
		return []ref{{"organization", "orgId", &v.OrgID}}
	case *domain.Offering:
		return []ref{
			{"course", "courseId", &v.CourseID},
			{"academicSession", "academicSessionId", &v.AcademicSessionID},
			{"organization", "orgId", &v.OrgID},
		}
	case *domain.Enrollment:
		return []ref{
			{"person", "personId", &v.PersonID},
			{"offering", "offeringId", &v.OfferingID},
		}
	}
	return nil
}

type refKey struct{ typ, id string }

// CheckRefs rejects an entity whose references are not the canonical ids of
// existing records, with a domain.ValidationError naming the first offending
// field. Call it on ingress (create/update) so stored references are always
// canonical ids: a sourcedId, or an id that matches nothing, is refused
// rather than stored and guessed at later.
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
// Lookups are batched: one repository call per referenced entity type, so a
// page costs at most five lookups however many records it holds.
func (c *Client) PublishRefs(ctx context.Context, entities ...domain.Entity) error {
	var refs []ref
	for _, e := range entities {
		refs = append(refs, refsOf(e)...)
	}
	found, err := c.resolve(ctx, refs)
	if err != nil {
		return err
	}
	for _, r := range refs {
		if *r.ptr != "" {
			*r.ptr = found[refKey{r.typ, *r.ptr}]
		}
	}
	return nil
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
		return entitySourcedIDs(ctx, c.Offerings, ids)
	}
	panic("readmodel: unknown reference type " + typ)
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
