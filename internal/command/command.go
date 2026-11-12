// Package command holds the write operations every inbound transport runs
// (the /api/v1 REST handlers and the AMQP consumer), so a write behaves the
// same whichever way it arrives. Callers decode the payload; this package
// checks references and calls the services.
package command

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SUNET/sis/internal/convert"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
	"github.com/SUNET/sis/pkg/trace"
)

// Client runs the write commands against the read-model services. It is a
// "Client" rather than a "Service" because it owns no runtime resources.
//
// The generic entity commands are free functions taking *Client first: Go
// does not allow type parameters on methods.
type Client struct {
	catalog   *readmodel.Client
	converter *convert.Client
	now       func() time.Time
	log       *logger.Log
}

// New builds the command Client, with its own convert Client (pure, so
// nothing is shared or shut down).
func New(ctx context.Context, cfg *model.Cfg, catalog *readmodel.Client, log *logger.Log) (*Client, error) {
	converter, err := convert.New(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	return &Client{catalog: catalog, converter: converter, now: time.Now, log: log.New("command")}, nil
}

// CreatePerson creates person.
func (c *Client) CreatePerson(ctx context.Context, person domain.Person) (domain.Person, error) {
	return c.catalog.Persons.CreatePerson(ctx, person)
}

// UpdatePerson replaces the person id with person; id wins over person.ID.
func (c *Client) UpdatePerson(ctx context.Context, id string, person domain.Person) (domain.Person, error) {
	person.ID = id
	return c.catalog.Persons.UpdatePerson(ctx, person)
}

// DeletePerson deletes the person id together with their enrollments and
// results, which mean nothing without the person.
//
// The dependants go first, so a failure part-way leaves the person in place
// and the delete can simply be retried. Each dependant delete emits its own
// event. There is no transaction across the two stores: an enrollment or
// result created for the person while this runs can survive it, as an
// orphan that the providers never publish (see readmodel.PublishRefs).
func (c *Client) DeletePerson(ctx context.Context, id string) (err error) {
	ctx, end := trace.Start(ctx, "command person.delete")
	defer func() { end(err) }()
	if _, err := c.catalog.Persons.GetPerson(ctx, id); err != nil {
		return err
	}
	if err := deleteByPerson(ctx, c.catalog.Enrollments, id); err != nil {
		return fmt.Errorf("delete enrollments of person %s: %w", id, err)
	}
	if err := deleteByPerson(ctx, c.catalog.Results, id); err != nil {
		return fmt.Errorf("delete results of person %s: %w", id, err)
	}
	return c.catalog.Persons.DeletePerson(ctx, id)
}

// cascadeBatch is how many dependants DeletePerson reads per round.
const cascadeBatch = 500

// deleteByPerson deletes every entity of svc whose personId is personID. It
// rereads the first page until it comes back empty, so records added or
// removed meanwhile cannot make it skip one. A record deleted concurrently,
// or moved to another person since it was listed (ErrNotFound), is not this
// person's to delete. A nil svc (a partial catalog in tests) has nothing to
// delete.
func deleteByPerson[T any, PT domain.EntityPtr[T]](ctx context.Context, svc *service.Resource[T, PT], personID string) error {
	if svc == nil {
		return nil
	}
	for {
		page, err := svc.ListByRef(ctx, "personId", personID, port.ListParams{Limit: cascadeBatch})
		if err != nil {
			return err
		}
		if len(page.Items) == 0 {
			return nil
		}
		// Every listed record is now deleted, gone, or no longer this
		// person's, so the next read makes progress.
		for _, e := range page.Items {
			if err := svc.DeleteByRef(ctx, e.GetID(), "personId", personID); err != nil && !errors.Is(err, domain.ErrNotFound) {
				return err
			}
		}
	}
}

// Create creates entity. References to other records must be their canonical
// ids (see readmodel.CheckRefs).
func Create[T any, PT domain.EntityPtr[T]](ctx context.Context, c *Client, svc *service.Resource[T, PT], entity PT) (out PT, err error) {
	ctx, end := trace.Start(ctx, "command "+svc.TypeName()+".create")
	defer func() { end(err) }()
	if err := c.catalog.CheckRefs(ctx, entity); err != nil {
		return nil, err
	}
	return svc.Create(ctx, entity)
}

// Update replaces the entity id with entity; id wins over entity's own id.
// References are checked as in Create.
func Update[T any, PT domain.EntityPtr[T]](ctx context.Context, c *Client, svc *service.Resource[T, PT], id string, entity PT) (out PT, err error) {
	ctx, end := trace.Start(ctx, "command "+svc.TypeName()+".update")
	defer func() { end(err) }()
	entity.SetID(id)
	if err := c.catalog.CheckRefs(ctx, entity); err != nil {
		return nil, err
	}
	return svc.Update(ctx, entity)
}

// Delete deletes the entity id.
func Delete[T any, PT domain.EntityPtr[T]](ctx context.Context, _ *Client, svc *service.Resource[T, PT], id string) error {
	return svc.Delete(ctx, id)
}
