package readmodel

import (
	"context"
	"errors"

	"github.com/SUNET/sis/internal/core/domain"
)

// PublishRefs rewrites the references each entity holds to other records
// (organization parent, course org, offering course/session/org, enrollment
// person/offering) from canonical ids to the referenced records' sourcedIds.
// The standards providers publish and look up every resource by sourcedId,
// so a reference must carry the sourcedId to be followable.
//
// Call it in the provider layer, before mapping to a DTO. It mutates the
// given values, which are fresh copies from the repository. A reference that
// already holds a sourcedId is kept as-is; one that matches no record is
// cleared so a broken link is never published. Lookups are memoised for the
// call, so a list page costs one lookup per distinct referenced record.
func (c *Client) PublishRefs(ctx context.Context, entities ...domain.Entity) error {
	r := refResolver{c: c, seen: map[refKey]string{}}
	for _, e := range entities {
		if err := r.rewrite(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

type refKey struct{ typ, id string }

type refResolver struct {
	c    *Client
	seen map[refKey]string
}

func (r *refResolver) rewrite(ctx context.Context, e domain.Entity) error {
	var refs []*string
	var types []string
	add := func(typ string, field *string) {
		refs = append(refs, field)
		types = append(types, typ)
	}
	switch v := e.(type) {
	case *domain.Organization:
		add("organization", &v.ParentID)
	case *domain.Course:
		add("organization", &v.OrgID)
	case *domain.Offering:
		add("course", &v.CourseID)
		add("academicSession", &v.AcademicSessionID)
		add("organization", &v.OrgID)
	case *domain.Enrollment:
		add("person", &v.PersonID)
		add("offering", &v.OfferingID)
	}
	for i, field := range refs {
		sid, err := r.sourcedID(ctx, types[i], *field)
		if err != nil {
			return err
		}
		*field = sid
	}
	return nil
}

// sourcedID returns the sourcedId of the typ record whose canonical id is id.
// If no record has that id but one has it as sourcedId, id is already public
// and is returned unchanged. Otherwise it returns "".
func (r *refResolver) sourcedID(ctx context.Context, typ, id string) (string, error) {
	if id == "" {
		return "", nil
	}
	k := refKey{typ, id}
	if sid, ok := r.seen[k]; ok {
		return sid, nil
	}
	byID, bySourcedID := r.lookups(typ)
	sid, err := byID(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		if _, err = bySourcedID(ctx, id); err == nil {
			sid = id
		}
	}
	if errors.Is(err, domain.ErrNotFound) {
		sid, err = "", nil
	}
	if err != nil {
		return "", err
	}
	r.seen[k] = sid
	return sid, nil
}

type lookupFn func(ctx context.Context, key string) (string, error)

// lookups returns the get-by-id and get-by-sourcedId functions for typ, each
// yielding the record's sourcedId.
func (r *refResolver) lookups(typ string) (byID, bySourcedID lookupFn) {
	c := r.c
	switch typ {
	case "person":
		return func(ctx context.Context, k string) (string, error) {
				p, err := c.Persons.GetPerson(ctx, k)
				return p.SourcedID, err
			}, func(ctx context.Context, k string) (string, error) {
				p, err := c.Persons.GetPersonBySourcedID(ctx, k)
				return p.SourcedID, err
			}
	case "organization":
		return entityLookups(c.Organizations.Get, c.Organizations.GetBySourcedID)
	case "academicSession":
		return entityLookups(c.AcademicSessions.Get, c.AcademicSessions.GetBySourcedID)
	case "course":
		return entityLookups(c.Courses.Get, c.Courses.GetBySourcedID)
	case "offering":
		return entityLookups(c.Offerings.Get, c.Offerings.GetBySourcedID)
	}
	panic("readmodel: unknown reference type " + typ)
}

func entityLookups[PT domain.Entity](get, getBySourcedID func(context.Context, string) (PT, error)) (lookupFn, lookupFn) {
	wrap := func(f func(context.Context, string) (PT, error)) lookupFn {
		return func(ctx context.Context, k string) (string, error) {
			e, err := f(ctx, k)
			if err != nil {
				return "", err
			}
			return e.GetSourcedID(), nil
		}
	}
	return wrap(get), wrap(getBySourcedID)
}
