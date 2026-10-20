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

	if err := p.resolveStored(ctx, all); err != nil {
		return nil, err
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

// resolveStored checks every reference of es names a record in the bundle or
// a stored one, recording the stored records' canonical ids. Stored records
// are looked up in one batched call per type.
func (p *populator) resolveStored(ctx context.Context, es []domain.Entity) error {
	type use struct {
		e domain.Entity
		r ref
	}
	var uses []use
	byType := map[string][]string{}
	seen := map[refKey]bool{}
	for _, e := range es {
		for _, r := range refsOf(e) {
			k := refKey{r.typ, *r.ptr}
			if k.id == "" || p.inBundle[k] {
				continue
			}
			uses = append(uses, use{e, r})
			if !seen[k] {
				seen[k] = true
				byType[r.typ] = append(byType[r.typ], k.id)
			}
		}
	}
	for typ, sids := range byType {
		ids, err := p.c.storedIDs(ctx, typ, sids)
		if err != nil {
			return err
		}
		for sid, id := range ids {
			p.ids[refKey{typ, sid}] = id
		}
	}
	for _, u := range uses {
		if _, ok := p.ids[refKey{u.r.typ, *u.r.ptr}]; !ok {
			return &domain.ValidationError{
				Field: u.e.EntityType() + "[" + u.e.GetSourcedID() + "]." + u.r.field,
				Msg:   "no " + u.r.typ + " with sourcedId " + *u.r.ptr + " in the document or stored",
			}
		}
	}
	return nil
}

// storedIDs maps each sourcedId in sids that names a stored typ record to
// that record's canonical id, in a single repository call.
func (c *Client) storedIDs(ctx context.Context, typ string, sids []string) (map[string]string, error) {
	switch typ {
	case "person":
		ps, err := c.Persons.GetPersonsBySourcedIDs(ctx, sids)
		if err != nil {
			return nil, err
		}
		out := make(map[string]string, len(ps))
		for _, p := range ps {
			out[p.SourcedID] = p.ID
		}
		return out, nil
	case "organization":
		return entityIDs(ctx, c.Organizations, sids)
	case "academicSession":
		return entityIDs(ctx, c.AcademicSessions, sids)
	case "course":
		return entityIDs(ctx, c.Courses, sids)
	case "program":
		return entityIDs(ctx, c.Programs, sids)
	case "offering":
		return entityIDs(ctx, c.Offerings, sids)
	}
	return nil, fmt.Errorf("readmodel: unknown reference type %q", typ)
}

func entityIDs[T any, PT domain.EntityPtr[T]](ctx context.Context, svc *service.Resource[T, PT], sids []string) (map[string]string, error) {
	es, err := svc.GetBySourcedIDs(ctx, sids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(es))
	for _, e := range es {
		out[e.GetSourcedID()] = e.GetID()
	}
	return out, nil
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
