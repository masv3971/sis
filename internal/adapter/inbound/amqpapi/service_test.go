package amqpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/Azure/go-amqp"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/internal/auth/authtest"
	"github.com/SUNET/sis/internal/command"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

type settlement struct {
	msg     *amqp.Message
	outcome string
	err     *amqp.Error
}

// fakeLink delivers queued messages and records how each was settled.
type fakeLink struct {
	in      chan *amqp.Message
	settled chan settlement
	fail    chan error
}

func newFakeLink() *fakeLink {
	return &fakeLink{in: make(chan *amqp.Message, 16), settled: make(chan settlement, 16), fail: make(chan error, 1)}
}

func (f *fakeLink) Receive(ctx context.Context, _ *amqp.ReceiveOptions) (*amqp.Message, error) {
	select {
	case m := <-f.in:
		return m, nil
	case err := <-f.fail:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeLink) AcceptMessage(_ context.Context, m *amqp.Message) error {
	f.settled <- settlement{msg: m, outcome: "accepted"}
	return nil
}

func (f *fakeLink) RejectMessage(_ context.Context, m *amqp.Message, e *amqp.Error) error {
	f.settled <- settlement{msg: m, outcome: "rejected", err: e}
	return nil
}

func (f *fakeLink) ModifyMessage(_ context.Context, m *amqp.Message, o *amqp.ModifyMessageOptions) error {
	if !o.DeliveryFailed {
		return errors.New("modify without DeliveryFailed")
	}
	f.settled <- settlement{msg: m, outcome: "retry"}
	return nil
}

func (f *fakeLink) Close(context.Context) error { return nil }

type fixture struct {
	link    *fakeLink
	catalog *readmodel.Client
	iss     *authtest.Issuer
}

func newFixture(t *testing.T, scopes ...string) *fixture {
	t.Helper()
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	catalog := &readmodel.Client{
		Persons:          service.New(memory.NewPersonRepository(), nil, nil, log),
		Organizations:    service.NewResource(memory.NewDocRepo[domain.Organization](), nil, nil, log),
		AcademicSessions: service.NewResource(memory.NewDocRepo[domain.AcademicSession](), nil, nil, log),
		Courses:          service.NewResource(memory.NewDocRepo[domain.Course](), nil, nil, log),
		Offerings:        service.NewResource(memory.NewDocRepo[domain.Offering](), nil, nil, log),
		Enrollments:      service.NewResource(memory.NewDocRepo[domain.Enrollment](), nil, nil, log),
		Programs:         service.NewResource(memory.NewDocRepo[domain.Program](), nil, nil, log),
		Results:          service.NewResource(memory.NewDocRepo[domain.Result](), nil, nil, log),
	}
	iss := authtest.NewIssuer(t)
	cfg := &model.Cfg{SIS: model.SIS{Auth: model.Auth{Issuers: []model.Issuer{iss.Config()}}}}
	verifier, err := auth.New(t.Context(), cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	commands, err := command.New(t.Context(), cfg, catalog, log)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{link: newFakeLink(), catalog: catalog, iss: iss}
	ac := model.AMQPAPI{Address: "/queues/test", Prefetch: 4, MaxMessageBytes: 1 << 20, Scopes: scopes}
	dial := func(context.Context) (link, error) { return f.link, nil }
	s := start(t.Context(), ac, verifier, newCommandTable(commands, catalog), dial, log)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return f
}

func message(subject, token, body string) *amqp.Message {
	m := &amqp.Message{Properties: &amqp.MessageProperties{Subject: &subject, MessageID: "m-" + subject}, Data: [][]byte{[]byte(body)}}
	if token != "" {
		m.ApplicationProperties = map[string]any{authorizationProperty: "Bearer " + token}
	}
	return m
}

// send delivers m and returns its settlement.
func (f *fixture) send(t *testing.T, m *amqp.Message) settlement {
	t.Helper()
	f.link.in <- m
	select {
	case s := <-f.link.settled:
		if s.msg != m {
			t.Fatalf("settled another message")
		}
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("message not settled")
		return settlement{}
	}
}

func (f *fixture) expect(t *testing.T, m *amqp.Message, want string) settlement {
	t.Helper()
	s := f.send(t, m)
	if s.outcome != want {
		desc := ""
		if s.err != nil {
			desc = string(s.err.Condition) + " " + s.err.Description
		}
		t.Fatalf("%s: outcome %s, want %s %s", *m.Properties.Subject, s.outcome, want, desc)
	}
	return s
}

func TestCommandsRunLikeREST(t *testing.T) {
	f := newFixture(t)
	tok := f.iss.Token(t, "uni", "sis.write")
	ctx := t.Context()

	f.expect(t, message("person.create", tok, `{"id":"p1","sourcedId":"s1","givenName":"A","familyName":"B","status":"active"}`), "accepted")
	f.expect(t, message("person.update", tok, `{"id":"p1","givenName":"Ann","familyName":"B","status":"active"}`), "accepted")
	p, err := f.catalog.Persons.GetPerson(ctx, "p1")
	if err != nil || p.GivenName != "Ann" || p.SourcedID != "s1" {
		t.Fatalf("person after update = %+v, %v", p, err)
	}

	f.expect(t, message("organization.create", tok, `{"id":"o1","sourcedId":"uni","name":"U"}`), "accepted")
	f.expect(t, message("course.create", tok, `{"id":"c1","title":"T","orgId":"o1"}`), "accepted")
	// References must be canonical ids, as on /api/v1 (readmodel.CheckRefs).
	f.expect(t, message("course.create", tok, `{"id":"c2","title":"T","orgId":"uni"}`), "rejected")
	f.expect(t, message("course.delete", tok, `{"id":"c1"}`), "accepted")
	if _, err := f.catalog.Courses.Get(ctx, "c1"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("course after delete: %v", err)
	}
	f.expect(t, message("person.delete", tok, `{"id":"p1"}`), "accepted")

	elm, err := os.ReadFile("../../../convert/testdata/edc_bsc.json")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"metadata": map[string]string{"input_format": "elm"}, "ttl": "1h", "data": json.RawMessage(elm)})
	f.expect(t, message("populate", tok, string(body)), "accepted")
	page, err := f.catalog.Results.List(ctx, port.ListParams{Limit: 10})
	if err != nil || page.Total == 0 {
		t.Fatalf("results after populate: total %d, %v", page.Total, err)
	}
}

func TestClientErrorsAreRejected(t *testing.T) {
	f := newFixture(t)
	tok := f.iss.Token(t, "uni", "sis.write")
	cases := map[string]struct {
		msg  *amqp.Message
		cond amqp.ErrCond
	}{
		"unknown subject":   {message("person.patch", tok, `{}`), amqp.ErrCondInvalidField},
		"unknown field":     {message("person.create", tok, `{"givenName":"A","familyName":"B","status":"active","bogus":1}`), amqp.ErrCondInvalidField},
		"trailing json":     {message("person.create", tok, `{"givenName":"A","familyName":"B","status":"active"} {}`), amqp.ErrCondInvalidField},
		"validation":        {message("person.create", tok, `{"givenName":"A"}`), amqp.ErrCondInvalidField},
		"update without id": {message("course.update", tok, `{"title":"T"}`), amqp.ErrCondInvalidField},
		"not found":         {message("person.delete", tok, `{"id":"nobody"}`), amqp.ErrCondNotFound},
		"bad ttl":           {message("populate", tok, `{"metadata":{"input_format":"elm"},"ttl":"soon","data":{}}`), amqp.ErrCondInvalidField},
		"no body":           {&amqp.Message{Properties: &amqp.MessageProperties{Subject: new("person.create")}, ApplicationProperties: map[string]any{authorizationProperty: "Bearer " + tok}}, amqp.ErrCondInvalidField},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := f.expect(t, c.msg, "rejected")
			if s.err == nil || s.err.Condition != c.cond {
				t.Fatalf("rejection = %+v, want condition %s", s.err, c.cond)
			}
		})
	}
	f.expect(t, message("person.create", tok, `{"id":"dup","givenName":"A","familyName":"B","status":"active"}`), "accepted")
	if s := f.expect(t, message("person.create", tok, `{"id":"dup","givenName":"A","familyName":"B","status":"active"}`), "rejected"); s.err.Condition != amqp.ErrCondPreconditionFailed {
		t.Fatalf("conflict condition = %s", s.err.Condition)
	}
}

func TestAuthorization(t *testing.T) {
	body := `{"givenName":"A","familyName":"B","status":"active"}`

	t.Run("token required without fallback scopes", func(t *testing.T) {
		f := newFixture(t)
		cases := map[string]*amqp.Message{
			"no token":      message("person.create", "", body),
			"read only":     message("person.create", f.iss.Token(t, "uni", "sis.read"), body),
			"invalid token": message("person.create", "not-a-jwt", body),
			"not bearer": func() *amqp.Message {
				m := message("person.create", "", body)
				m.ApplicationProperties = map[string]any{authorizationProperty: "Basic Zm9vOmJhcg=="}
				return m
			}(),
		}
		for name, m := range cases {
			t.Run(name, func(t *testing.T) {
				if s := f.expect(t, m, "rejected"); s.err.Condition != amqp.ErrCondUnauthorizedAccess {
					t.Fatalf("condition = %s", s.err.Condition)
				}
			})
		}
	})

	t.Run("fallback scopes trust messages without a token", func(t *testing.T) {
		f := newFixture(t, auth.ScopeWrite)
		f.expect(t, message("person.create", "", body), "accepted")
		// A token that is present is still checked.
		f.expect(t, message("person.create", f.iss.Token(t, "uni", "sis.read"), body), "rejected")
	})

	t.Run("issuer outage is retried", func(t *testing.T) {
		f := newFixture(t)
		tok := f.iss.Token(t, "uni", "sis.write")
		f.iss.Down.Store(true)
		f.expect(t, message("person.create", tok, body), "retry")
	})
}

// TestAMQP091Type: RabbitMQ hands an AMQP 0-9-1 message's type to AMQP 1.0
// consumers as the x-basic-type annotation.
func TestAMQP091Type(t *testing.T) {
	f := newFixture(t, auth.ScopeWrite)
	m := &amqp.Message{
		Annotations: amqp.Annotations{"x-basic-type": "person.create"},
		Properties:  &amqp.MessageProperties{MessageID: "legacy"},
		Data:        [][]byte{[]byte(`{"givenName":"A","familyName":"B","status":"active"}`)},
	}
	f.link.in <- m
	if s := <-f.link.settled; s.outcome != "accepted" {
		t.Fatalf("outcome %s: %+v", s.outcome, s.err)
	}
}

func TestReconnectsAfterLinkFailure(t *testing.T) {
	minRetryDelay = 10 * time.Millisecond
	t.Cleanup(func() { minRetryDelay = time.Second })
	f := newFixture(t, auth.ScopeWrite)
	f.link.fail <- &amqp.LinkError{}
	f.expect(t, message("person.create", "", `{"givenName":"A","familyName":"B","status":"active"}`), "accepted")
}

// TestInternalErrorsAreRetried: an unclassified failure (e.g. the database
// is down) is not the message's fault, so it goes back to the broker.
func TestInternalErrorsAreRetried(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	l := newFakeLink()
	table := map[string]handlerFunc{"boom": func(context.Context, []byte) error { return errors.New("connection refused") }}
	ac := model.AMQPAPI{Prefetch: 1, MaxMessageBytes: 1 << 20, Scopes: []string{auth.ScopeWrite}}
	s := start(t.Context(), ac, nil, table, func(context.Context) (link, error) { return l, nil }, log)
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	f := &fixture{link: l}
	f.expect(t, message("boom", "", `{}`), "retry")
}
