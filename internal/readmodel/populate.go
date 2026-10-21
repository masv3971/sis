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
// Populate checks ctx before every write, so a cancelled or expired ctx
// stops it even on a store that ignores ctx (memory); the error wraps
// ctx.Err().
//
// Populate consumes b: it assigns ids and rewrites references in place.
func (c *Client) Populate(ctx context.Context, b *domain.Bundle, expiresAt *time.Time) ([]PopulateItem, error) {
	p := &populator{client: c, ids: map[refKey]string{}, inBundle: map[refKey]bool{}, expiresAt: expiresAt}
	orgs, err := p.plan(ctx, b)
	if err != nil {
		return nil, err
	}

	items := make([]PopulateItem, 0, b.Len())
	add := func(typ, sourcedID, id string, created bool) {
		action := ActionUpdated
		if created {
			action = ActionCreated
		}
		items = append(items, PopulateItem{Type: typ, SourcedID: sourcedID, ID: id, Action: action})
		p.ids[refKey{typ, sourcedID}] = id
	}
	steps := []func() error{
		func() error { return upsertAll(ctx, p, c.Organizations, orgs, add) },
		func() error { return upsertAll(ctx, p, c.AcademicSessions, b.AcademicSessions, add) },
		func() error { return upsertAll(ctx, p, c.Courses, b.Courses, add) },
		func() error { return upsertAll(ctx, p, c.Programs, b.Programs, add) },
		func() error { return upsertAll(ctx, p, c.Offerings, b.Offerings, add) },
		func() error {
			for _, person := range b.Persons {
				if err := ctx.Err(); err != nil {
					return fmt.Errorf("populate person %s: %w", person.SourcedID, err)
				}
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
	client *Client
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
	collect := func(records ...domain.Entity) error {
		for _, e := range records {
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
func (p *populator) member(typ, sourcedID string, validateErr error) error {
	if validateErr != nil {
		if ve, ok := errors.AsType[*domain.ValidationError](validateErr); ok {
			return &domain.ValidationError{Field: typ + "[" + sourcedID + "]." + ve.Field, Msg: ve.Msg}
		}
		return validateErr
	}
	if sourcedID == "" {
		return domain.Required(typ + ".sourcedId")
	}
	key := refKey{typ, sourcedID}
	if p.inBundle[key] {
		return &domain.ValidationError{Field: typ + "[" + sourcedID + "]", Msg: "duplicate sourcedId"}
	}
	p.inBundle[key] = true
	return nil
}

// resolveStored checks every reference of records names a record in the
// bundle or a stored one, recording the stored records' canonical ids. Stored
// records are looked up in one batched call per type.
func (p *populator) resolveStored(ctx context.Context, records []domain.Entity) error {
	type use struct {
		entity    domain.Entity
		reference ref
	}
	var uses []use
	byType := map[string][]string{}
	seen := map[refKey]bool{}
	for _, e := range records {
		for _, reference := range refsOf(e) {
			key := refKey{reference.typ, *reference.target}
			if key.id == "" || p.inBundle[key] {
				continue
			}
			uses = append(uses, use{e, reference})
			if !seen[key] {
				seen[key] = true
				byType[reference.typ] = append(byType[reference.typ], key.id)
			}
		}
	}
	for typ, sourcedIDs := range byType {
		ids, err := p.client.storedIDs(ctx, typ, sourcedIDs)
		if err != nil {
			return err
		}
		for sourcedID, id := range ids {
			p.ids[refKey{typ, sourcedID}] = id
		}
	}
	for _, pending := range uses {
		if _, ok := p.ids[refKey{pending.reference.typ, *pending.reference.target}]; !ok {
			return &domain.ValidationError{
				Field: pending.entity.EntityType() + "[" + pending.entity.GetSourcedID() + "]." + pending.reference.field,
				Msg:   "no " + pending.reference.typ + " with sourcedId " + *pending.reference.target + " in the document or stored",
			}
		}
	}
	return nil
}

// storedIDs maps each sourcedId in sourcedIDs that names a stored typ record
// to that record's canonical id, in a single repository call.
func (c *Client) storedIDs(ctx context.Context, typ string, sourcedIDs []string) (map[string]string, error) {
	switch typ {
	case "person":
		persons, err := c.Persons.GetPersonsBySourcedIDs(ctx, sourcedIDs)
		if err != nil {
			return nil, err
		}
		out := make(map[string]string, len(persons))
		for _, person := range persons {
			out[person.SourcedID] = person.ID
		}
		return out, nil
	case "organization":
		return entityIDs(ctx, c.Organizations, sourcedIDs)
	case "academicSession":
		return entityIDs(ctx, c.AcademicSessions, sourcedIDs)
	case "course":
		return entityIDs(ctx, c.Courses, sourcedIDs)
	case "program":
		return entityIDs(ctx, c.Programs, sourcedIDs)
	case "offering":
		return entityIDs(ctx, c.Offerings, sourcedIDs)
	}
	return nil, errUnknownRefType(typ)
}

func entityIDs[T any, PT domain.EntityPtr[T]](ctx context.Context, svc *service.Resource[T, PT], sourcedIDs []string) (map[string]string, error) {
	records, err := svc.GetBySourcedIDs(ctx, sourcedIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(records))
	for _, e := range records {
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
func upsertAll[T any, PT domain.EntityPtr[T]](ctx context.Context, p *populator, svc *service.Resource[T, PT], records []PT, add func(typ, sourcedID, id string, created bool)) error {
	for _, e := range records {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("populate %s %s: %w", svc.TypeName(), e.GetSourcedID(), err)
		}
		for _, reference := range refsOf(e) {
			if *reference.target != "" {
				*reference.target = p.ids[refKey{reference.typ, *reference.target}]
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

func entities[PT domain.Entity](records []PT) []domain.Entity {
	out := make([]domain.Entity, len(records))
	for i, e := range records {
		out[i] = e
	}
	return out
}
