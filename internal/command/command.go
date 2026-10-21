// Package command holds the write operations every inbound transport runs
// (the /api/v1 REST handlers and the AMQP consumer), so a write behaves the
// same whichever way it arrives. Callers decode the payload; this package
// checks references and calls the services.
package command

import (
	"context"
	"time"

	"github.com/SUNET/sis/internal/convert"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// Client runs the write commands against the read-model services. It is a
// "Client" rather than a "Service" because it owns no runtime resources.
//
// The generic entity commands are free functions taking *Client first: Go
// does not allow type parameters on methods.
type Client struct {
	catalog *readmodel.Client
	convert *convert.Client
	now     func() time.Time
	log     *logger.Log
}

// New builds the command Client, with its own convert Client (pure, so
// nothing is shared or shut down).
func New(ctx context.Context, cfg *model.Cfg, catalog *readmodel.Client, log *logger.Log) (*Client, error) {
	conv, err := convert.New(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	return &Client{catalog: catalog, convert: conv, now: time.Now, log: log.New("command")}, nil
}

// CreatePerson creates p.
func (c *Client) CreatePerson(ctx context.Context, p domain.Person) (domain.Person, error) {
	return c.catalog.Persons.CreatePerson(ctx, p)
}

// UpdatePerson replaces the person id with p; id wins over p.ID.
func (c *Client) UpdatePerson(ctx context.Context, id string, p domain.Person) (domain.Person, error) {
	p.ID = id
	return c.catalog.Persons.UpdatePerson(ctx, p)
}

// DeletePerson deletes the person id.
func (c *Client) DeletePerson(ctx context.Context, id string) error {
	return c.catalog.Persons.DeletePerson(ctx, id)
}

// Create creates e. References to other records must be their canonical ids
// (see readmodel.CheckRefs).
func Create[T any, PT domain.EntityPtr[T]](ctx context.Context, c *Client, svc *service.Resource[T, PT], e PT) (PT, error) {
	if err := c.catalog.CheckRefs(ctx, e); err != nil {
		return nil, err
	}
	return svc.Create(ctx, e)
}

// Update replaces the entity id with e; id wins over e's own id. References
// are checked as in Create.
func Update[T any, PT domain.EntityPtr[T]](ctx context.Context, c *Client, svc *service.Resource[T, PT], id string, e PT) (PT, error) {
	e.SetID(id)
	if err := c.catalog.CheckRefs(ctx, e); err != nil {
		return nil, err
	}
	return svc.Update(ctx, e)
}

// Delete deletes the entity id.
func Delete[T any, PT domain.EntityPtr[T]](ctx context.Context, _ *Client, svc *service.Resource[T, PT], id string) error {
	return svc.Delete(ctx, id)
}
