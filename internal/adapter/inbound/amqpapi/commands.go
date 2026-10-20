package amqpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/Azure/go-amqp"

	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/internal/command"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/helpers"
)

// authorizationProperty is the application property carrying the bearer
// token, named like the HTTP header.
const authorizationProperty = "authorization"

// handlerFunc decodes one command's body and runs it.
type handlerFunc func(ctx context.Context, body []byte) error

// idBody is the body of a delete command.
type idBody struct {
	ID string `json:"id" binding:"required"`
}

// newCommandTable maps message subjects to commands:
// <type>.create|update|delete for person and every entity type (named as in
// domain events, e.g. academicSession), and populate. Bodies are the
// /api/v1 request bodies; update carries its id in the body.
func newCommandTable(c *command.Client, catalog *readmodel.Client) map[string]handlerFunc {
	t := map[string]handlerFunc{
		"person.create": func(ctx context.Context, body []byte) error {
			var p domain.Person
			if err := helpers.DecodeJSON(body, &p); err != nil {
				return err
			}
			_, err := c.CreatePerson(ctx, p)
			return err
		},
		"person.update": func(ctx context.Context, body []byte) error {
			var p domain.Person
			if err := helpers.DecodeJSON(body, &p); err != nil {
				return err
			}
			if p.ID == "" {
				return errIDRequired
			}
			_, err := c.UpdatePerson(ctx, p.ID, p)
			return err
		},
		"person.delete": func(ctx context.Context, body []byte) error {
			var b idBody
			if err := helpers.DecodeJSON(body, &b); err != nil {
				return err
			}
			return c.DeletePerson(ctx, b.ID)
		},
		"populate": func(ctx context.Context, body []byte) error {
			var req command.PopulateRequest
			if err := helpers.DecodeJSON(body, &req); err != nil {
				return err
			}
			_, err := c.Populate(ctx, req)
			return err
		},
	}
	register(t, c, catalog.Organizations)
	register(t, c, catalog.AcademicSessions)
	register(t, c, catalog.Courses)
	register(t, c, catalog.Offerings)
	register(t, c, catalog.Enrollments)
	register(t, c, catalog.Programs)
	register(t, c, catalog.Results)
	return t
}

var errIDRequired = &domain.ValidationError{Field: "id", Msg: "required"}

// register adds the create/update/delete commands of one entity type.
func register[T any, PT domain.EntityPtr[T]](t map[string]handlerFunc, c *command.Client, svc *service.Resource[T, PT]) {
	typ := PT(new(T)).EntityType()
	t[typ+".create"] = func(ctx context.Context, body []byte) error {
		e := PT(new(T))
		if err := helpers.DecodeJSON(body, e); err != nil {
			return err
		}
		_, err := command.Create(ctx, c, svc, e)
		return err
	}
	t[typ+".update"] = func(ctx context.Context, body []byte) error {
		e := PT(new(T))
		if err := helpers.DecodeJSON(body, e); err != nil {
			return err
		}
		if e.GetID() == "" {
			return errIDRequired
		}
		_, err := command.Update(ctx, c, svc, e.GetID(), e)
		return err
	}
	t[typ+".delete"] = func(ctx context.Context, body []byte) error {
		var b idBody
		if err := helpers.DecodeJSON(body, &b); err != nil {
			return err
		}
		return command.Delete(ctx, c, svc, b.ID)
	}
}

// handle authorizes msg and runs the command its subject names.
func (s *Service) handle(ctx context.Context, msg *amqp.Message) error {
	if err := s.authorize(ctx, msg); err != nil {
		return err
	}
	subject := subjectOf(msg)
	h, ok := s.commands[subject]
	if !ok {
		return helpers.NewErrorDetailsWithStatus(helpers.ErrBadRequest, "unknown command subject "+`"`+subject+`"`, http.StatusBadRequest)
	}
	body, ok := bodyOf(msg)
	if !ok {
		return helpers.NewErrorDetailsWithStatus(helpers.ErrBadRequest, "body must be one data section or a string/binary value", http.StatusBadRequest)
	}
	if int64(len(body)) > s.cfg.MaxMessageBytes {
		return helpers.NewErrorWithStatus(helpers.ErrTooLarge, http.StatusRequestEntityTooLarge)
	}
	return h(ctx, body)
}

// authorize requires a token granting sis.write in the authorization
// property; a message without one is trusted only when the configured
// fallback scopes include sis.write.
func (s *Service) authorize(ctx context.Context, msg *amqp.Message) error {
	raw, present := msg.ApplicationProperties[authorizationProperty]
	if !present {
		if slices.Contains(s.cfg.Scopes, auth.ScopeWrite) {
			return nil
		}
		return errUnauthorized("missing " + authorizationProperty + " application property")
	}
	header, _ := raw.(string)
	tok, ok := auth.BearerToken(header)
	if !ok {
		return errUnauthorized(authorizationProperty + ` must be "Bearer <token>"`)
	}
	claims, err := s.verifier.Verify(ctx, tok)
	if errors.Is(err, auth.ErrIssuerUnavailable) {
		return err
	}
	if err != nil {
		s.log.Debug("token rejected", "err", err)
		return errUnauthorized("invalid token")
	}
	if !claims.HasScope(auth.ScopeWrite) {
		return helpers.NewErrorDetailsWithStatus("forbidden", "token lacks scope "+auth.ScopeWrite, http.StatusForbidden)
	}
	return nil
}

func errUnauthorized(detail string) error {
	return helpers.NewErrorDetailsWithStatus(helpers.ErrUnauthorized, detail, http.StatusUnauthorized)
}

// bodyOf returns the payload: one data section (the usual encoding), or a
// string or binary amqp-value.
func bodyOf(msg *amqp.Message) ([]byte, bool) {
	switch {
	case len(msg.Data) == 1:
		return msg.Data[0], true
	case len(msg.Data) > 1 || msg.Sequence != nil:
		return nil, false
	}
	switch v := msg.Value.(type) {
	case string:
		return []byte(v), true
	case []byte:
		return v, true
	}
	return nil, false
}
