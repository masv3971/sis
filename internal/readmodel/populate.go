// populate.go stores a converted domain.Bundle (POST /api/v1/populate).
package readmodel

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
)

// Populate actions.
const (
	ActionCreated = "created"
	ActionUpdated = "updated"
)

// PopulateItem reports what Populate did with one record.
type PopulateItem struct {
	Type      string `json:"type"`
	SourcedID string `json:"sourcedId"`
	ID        string `json:"id"`
	Action    string `json:"action"`
}

// Populate upserts every record of b by sourcedId, so the records expire at
// expiresAt (nil: never; see service.MergeExpiry for records that already
// exist). b's references are sourcedIds (see domain.Bundle); each must name a
// record in b or an already stored one.
//
// It works in two passes. The first validates every record and resolves
// every reference without writing anything, so a malformed bundle (invalid
// record, duplicate sourcedId, unknown reference, organization parent cycle)
// fails with a *domain.ValidationError and stores nothing. The second
// upserts in dependency order (organizations parents-first, academic
// sessions, courses, programs, offerings, persons, enrollments, results),
// rewriting each reference to the canonical id of its target. There is no
// cross-record transaction: a storage failure in the second pass leaves the
// records written so far, and since every write is an upsert by sourcedId,
// retrying the same request converges.
//
// Populate consumes b: it assigns ids and rewrites references in place.
func (c *Client) Populate(ctx context.Context, b *domain.Bundle, expiresAt *time.Time) ([]PopulateItem, error) {
	p := &populator{c: c, ids: map[refKey]string{}, inBundle: map[refKey]bool{}, expiresAt: expiresAt}
	orgs, err := p.plan(ctx, b)
	if err != nil {
		return nil, err
	}

	items := make([]PopulateItem, 0, b.Len())
	add := func(typ, sid, id string, created bool) {
		action := ActionUpdated
		if created {
			action = ActionCreated
		}
		items = append(items, PopulateItem{Type: typ, SourcedID: sid, ID: id, Action: action})
		p.ids[refKey{typ, sid}] = id
	}
	steps := []func() error{
		func() error { return upsertAll(ctx, p, c.Organizations, orgs, add) },
		func() error { return upsertAll(ctx, p, c.AcademicSessions, b.AcademicSessions, add) },
		func() error { return upsertAll(ctx, p, c.Courses, b.Courses, add) },
		func() error { return upsertAll(ctx, p, c.Programs, b.Programs, add) },
		func() error { return upsertAll(ctx, p, c.Offerings, b.Offerings, add) },
		func() error {
			for _, person := range b.Persons {
				out, created, err := c.Persons.UpsertPersonWithExpiry(ctx, person, expiresAt)
				if err != nil {
					return fmt.Errorf("populate person %s: %w", person.SourcedID, err)
				}
				add("person", out.SourcedID, out.ID, created)
			}
			return nil
		},
		func() error { return upsertAll(ctx, p, c.Enrollments, b.Enrollments, add) },
		func() error { return upsertAll(ctx, p, c.Results, b.Results, add) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return items, err
		}
	}
	return items, nil
}

type populator struct {
	c *Client
	// ids maps a referenced record to its canonical id: filled in the first
	// pass for references to stored records, and in the second as bundle
	// records are written.
	ids       map[refKey]string
	inBundle  map[refKey]bool
	expiresAt *time.Time
}

// plan is the first, read-only pass. It returns the organizations ordered
// parents-first.
func (p *populator) plan(ctx context.Context, b *domain.Bundle) ([]*domain.Organization, error) {
	var all []domain.Entity
	for _, person := range b.Persons {
		if err := p.member("person", person.SourcedID, person.Validate()); err != nil {
			return nil, err
		}
	}
	collect := func(es ...domain.Entity) error {
		for _, e := range es {
			if err := p.member(e.EntityType(), e.GetSourcedID(), e.Validate()); err != nil {
				return err
			}
			all = append(all, e)
		}
		return nil
	}
	for _, err := range []error{
		collect(entities(b.Organizations)...), collect(entities(b.AcademicSessions)...),
		collect(entities(b.Courses)...), collect(entities(b.Programs)...),
		collect(entities(b.Offerings)...), collect(entities(b.Enrollments)...),
		collect(entities(b.Results)...),
	} {
		if err != nil {
			return nil, err
		}
	}

	for _, e := range all {
		for _, r := range refsOf(e) {
			if err := p.resolveStored(ctx, e, r); err != nil {
				return nil, err
			}
		}
	}
	return orderOrgs(b.Organizations)
}

// member registers a bundle record, rejecting an invalid record or a
// duplicate sourcedId.
func (p *populator) member(typ, sid string, validateErr error) error {
	if validateErr != nil {
		if ve, ok := errors.AsType[*domain.ValidationError](validateErr); ok {
			return &domain.ValidationError{Field: typ + "[" + sid + "]." + ve.Field, Msg: ve.Msg}
		}
		return validateErr
	}
	if sid == "" {
		return &domain.ValidationError{Field: typ + ".sourcedId", Msg: "required"}
	}
	k := refKey{typ, sid}
	if p.inBundle[k] {
		return &domain.ValidationError{Field: typ + "[" + sid + "]", Msg: "duplicate sourcedId"}
	}
	p.inBundle[k] = true
	return nil
}

// resolveStored checks a reference of e names a record in the bundle or a
// stored one, recording the stored record's canonical id.
func (p *populator) resolveStored(ctx context.Context, e domain.Entity, r ref) error {
	sid := *r.ptr
	k := refKey{r.typ, sid}
	if sid == "" || p.inBundle[k] {
		return nil
	}
	if _, done := p.ids[k]; done {
		return nil
	}
	id, err := p.c.storedID(ctx, r.typ, sid)
	if errors.Is(err, domain.ErrNotFound) {
		return &domain.ValidationError{
			Field: e.EntityType() + "[" + e.GetSourcedID() + "]." + r.field,
			Msg:   "no " + r.typ + " with sourcedId " + sid + " in the document or stored",
		}
	}
	if err != nil {
		return err
	}
	p.ids[k] = id
	return nil
}

// storedID returns the canonical id of the stored typ record with sourcedId.
func (c *Client) storedID(ctx context.Context, typ, sid string) (string, error) {
	var (
		e   domain.Entity
		err error
	)
	switch typ {
	case "person":
		p, err := c.Persons.GetPersonBySourcedID(ctx, sid)
		return p.ID, err
	case "organization":
		e, err = c.Organizations.GetBySourcedID(ctx, sid)
	case "academicSession":
		e, err = c.AcademicSessions.GetBySourcedID(ctx, sid)
	case "course":
		e, err = c.Courses.GetBySourcedID(ctx, sid)
	case "program":
		e, err = c.Programs.GetBySourcedID(ctx, sid)
	case "offering":
		e, err = c.Offerings.GetBySourcedID(ctx, sid)
	default:
		panic("readmodel: unknown reference type " + typ)
	}
	if err != nil {
		return "", err
	}
	return e.GetID(), nil
}

// orderOrgs orders organizations so every parent in the slice precedes its
// children, rejecting a parent cycle.
func orderOrgs(orgs []*domain.Organization) ([]*domain.Organization, error) {
	bySID := make(map[string]*domain.Organization, len(orgs))
	for _, o := range orgs {
		bySID[o.SourcedID] = o
	}
	const (
		visiting = 1
		done     = 2
	)
	state := map[string]int{}
	out := make([]*domain.Organization, 0, len(orgs))
	var visit func(o *domain.Organization) error
	visit = func(o *domain.Organization) error {
		switch state[o.SourcedID] {
		case done:
			return nil
		case visiting:
			return &domain.ValidationError{Field: "organization[" + o.SourcedID + "].parentId", Msg: "parent cycle"}
		}
		state[o.SourcedID] = visiting
		if parent, ok := bySID[o.ParentID]; ok {
			if err := visit(parent); err != nil {
				return err
			}
		}
		state[o.SourcedID] = done
		out = append(out, o)
		return nil
	}
	for _, o := range orgs {
		if err := visit(o); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// upsertAll rewrites each record's references to canonical ids and upserts
// it. A free function because it is generic over the entity type.
func upsertAll[T any, PT domain.EntityPtr[T]](ctx context.Context, p *populator, svc *service.Resource[T, PT], es []PT, add func(typ, sid, id string, created bool)) error {
	for _, e := range es {
		for _, r := range refsOf(e) {
			if *r.ptr != "" {
				*r.ptr = p.ids[refKey{r.typ, *r.ptr}]
			}
		}
		e.SetID("")
		out, created, err := svc.UpsertWithExpiry(ctx, e, p.expiresAt)
		if err != nil {
			return fmt.Errorf("populate %s %s: %w", svc.TypeName(), e.GetSourcedID(), err)
		}
		add(svc.TypeName(), out.GetSourcedID(), out.GetID(), created)
	}
	return nil
}

func entities[PT domain.Entity](es []PT) []domain.Entity {
	out := make([]domain.Entity, len(es))
	for i, e := range es {
		out[i] = e
	}
	return out
}
