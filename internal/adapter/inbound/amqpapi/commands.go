package amqpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"unicode/utf8"

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
func newCommandTable(commands *command.Client, catalog *readmodel.Client) map[string]handlerFunc {
	table := map[string]handlerFunc{
		"person.create": func(ctx context.Context, body []byte) error {
			var person domain.Person
			if err := helpers.DecodeJSON(body, &person); err != nil {
				return err
			}
			_, err := commands.CreatePerson(ctx, person)
			return err
		},
		"person.update": func(ctx context.Context, body []byte) error {
			var person domain.Person
			if err := helpers.DecodeJSON(body, &person); err != nil {
				return err
			}
			if person.ID == "" {
				return errIDRequired
			}
			_, err := commands.UpdatePerson(ctx, person.ID, person)
			return err
		},
		"person.delete": func(ctx context.Context, body []byte) error {
			var target idBody
			if err := helpers.DecodeJSON(body, &target); err != nil {
				return err
			}
			return commands.DeletePerson(ctx, target.ID)
		},
		"populate": func(ctx context.Context, body []byte) error {
			var req command.PopulateRequest
			if err := helpers.DecodeJSON(body, &req); err != nil {
				return err
			}
			_, err := commands.Populate(ctx, req)
			return err
		},
	}
	register(table, commands, catalog.Organizations)
	register(table, commands, catalog.AcademicSessions)
	register(table, commands, catalog.Courses)
	register(table, commands, catalog.Offerings)
	register(table, commands, catalog.Enrollments)
	register(table, commands, catalog.Programs)
	register(table, commands, catalog.Results)
	return table
}

var errIDRequired = domain.Required("id")

// register adds the create/update/delete commands of one entity type.
func register[T any, PT domain.EntityPtr[T]](table map[string]handlerFunc, commands *command.Client, svc *service.Resource[T, PT]) {
	typ := PT(new(T)).EntityType()
	table[typ+".create"] = func(ctx context.Context, body []byte) error {
		entity := PT(new(T))
		if err := helpers.DecodeJSON(body, entity); err != nil {
			return err
		}
		_, err := command.Create(ctx, commands, svc, entity)
		return err
	}
	table[typ+".update"] = func(ctx context.Context, body []byte) error {
		entity := PT(new(T))
		if err := helpers.DecodeJSON(body, entity); err != nil {
			return err
		}
		if entity.GetID() == "" {
			return errIDRequired
		}
		_, err := command.Update(ctx, commands, svc, entity.GetID(), entity)
		return err
	}
	table[typ+".delete"] = func(ctx context.Context, body []byte) error {
		var target idBody
		if err := helpers.DecodeJSON(body, &target); err != nil {
			return err
		}
		return command.Delete(ctx, commands, svc, target.ID)
	}
}

// handle authorizes msg and runs the command its subject names.
func (s *Service) handle(ctx context.Context, msg *amqp.Message) error {
	if err := s.authorize(ctx, msg); err != nil {
		return err
	}
	subject := subjectOf(msg)
	run, ok := s.commands[subject]
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
	return run(ctx, body)
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
	token, ok := auth.BearerToken(headerString(raw))
	if !ok {
		return errUnauthorized(authorizationProperty + ` must be "Bearer <token>"`)
	}
	claims, err := s.verifier.Verify(ctx, token)
	if errors.Is(err, auth.ErrIssuerUnavailable) {
		return err
	}
	if err != nil {
		s.log.Debug("token rejected", "err", err)
		return errUnauthorized(helpers.MsgInvalidToken)
	}
	if !claims.HasScope(auth.ScopeWrite) {
		return helpers.NewErrorDetailsWithStatus(helpers.ErrForbidden, helpers.MsgLacksScope(auth.ScopeWrite), http.StatusForbidden)
	}
	return nil
}

// headerString returns a string or UTF-8 binary property value. RabbitMQ 4
// hands an AMQP 0-9-1 longstr header to AMQP 1.0 consumers as a string only
// up to 4096 bytes and as binary above that, so a long bearer token arrives
// as []byte. Anything else yields "", which BearerToken rejects.
func headerString(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case []byte:
		if utf8.Valid(v) {
			return string(v)
		}
	}
	return ""
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
