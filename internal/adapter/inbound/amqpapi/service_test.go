package amqpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/go-amqp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	// acceptErr, when it holds an error, fails the next AcceptMessage.
	acceptErr chan error
	// closed gets a value on every Close.
	closed chan struct{}
}

func newFakeLink() *fakeLink {
	return &fakeLink{
		in: make(chan *amqp.Message, 16), settled: make(chan settlement, 16), fail: make(chan error, 1),
		acceptErr: make(chan error, 1), closed: make(chan struct{}, 16),
	}
}

// Receive returns a queued (prefetched) message even when ctx is done, as
// *amqp.Receiver does.
func (fake *fakeLink) Receive(ctx context.Context, _ *amqp.ReceiveOptions) (*amqp.Message, error) {
	select {
	case msg := <-fake.in:
		return msg, nil
	default:
	}
	select {
	case msg := <-fake.in:
		return msg, nil
	case err := <-fake.fail:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (fake *fakeLink) AcceptMessage(_ context.Context, msg *amqp.Message) error {
	select {
	case err := <-fake.acceptErr:
		return err
	default:
	}
	fake.settled <- settlement{msg: msg, outcome: "accepted"}
	return nil
}

func (fake *fakeLink) RejectMessage(_ context.Context, msg *amqp.Message, rejectErr *amqp.Error) error {
	fake.settled <- settlement{msg: msg, outcome: "rejected", err: rejectErr}
	return nil
}

func (fake *fakeLink) ModifyMessage(_ context.Context, msg *amqp.Message, opts *amqp.ModifyMessageOptions) error {
	if !opts.DeliveryFailed {
		return errors.New("modify without DeliveryFailed")
	}
	fake.settled <- settlement{msg: msg, outcome: "retry"}
	return nil
}

func (fake *fakeLink) Close(context.Context) error {
	select {
	case fake.closed <- struct{}{}:
	default:
	}
	return nil
}

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
	require.NoError(t, err)
	commands, err := command.New(t.Context(), cfg, catalog, log)
	require.NoError(t, err)
	fx := &fixture{link: newFakeLink(), catalog: catalog, iss: iss}
	amqpCfg := model.AMQPAPI{Address: "/queues/test", Prefetch: 4, MaxMessageBytes: 1 << 20, Scopes: scopes}
	dial := func(context.Context) (link, error) { return fx.link, nil }
	svc := start(t.Context(), amqpCfg, verifier, newCommandTable(commands, catalog), dial, log)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		assert.NoError(t, svc.Close(ctx), "Close")
	})
	return fx
}

func message(subject, token, body string) *amqp.Message {
	msg := &amqp.Message{Properties: &amqp.MessageProperties{Subject: &subject, MessageID: "m-" + subject}, Data: [][]byte{[]byte(body)}}
	if token != "" {
		msg.ApplicationProperties = map[string]any{authorizationProperty: "Bearer " + token}
	}
	return msg
}

// send delivers msg and returns its settlement.
func (fx *fixture) send(t *testing.T, msg *amqp.Message) settlement {
	t.Helper()
	fx.link.in <- msg
	select {
	case got := <-fx.link.settled:
		require.Same(t, msg, got.msg, "settled another message")
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("message not settled")
		return settlement{}
	}
}

func (fx *fixture) expect(t *testing.T, msg *amqp.Message, want string) settlement {
	t.Helper()
	got := fx.send(t, msg)
	desc := ""
	if got.err != nil {
		desc = string(got.err.Condition) + " " + got.err.Description
	}
	require.Equal(t, want, got.outcome, "%s: outcome %s", *msg.Properties.Subject, desc)
	return got
}

func TestCommandsRunLikeREST(t *testing.T) {
	fx := newFixture(t)
	token := fx.iss.Token(t, "uni", "sis.write")
	ctx := t.Context()

	fx.expect(t, message("person.create", token, `{"id":"p1","sourcedId":"s1","givenName":"A","familyName":"B","status":"active"}`), "accepted")
	fx.expect(t, message("person.update", token, `{"id":"p1","givenName":"Ann","familyName":"B","status":"active"}`), "accepted")
	person, err := fx.catalog.Persons.GetPerson(ctx, "p1")
	require.NoError(t, err, "person after update")
	assert.Equal(t, "Ann", person.GivenName, "person after update")
	assert.Equal(t, "s1", person.SourcedID, "person after update")

	fx.expect(t, message("organization.create", token, `{"id":"o1","sourcedId":"uni","name":"U"}`), "accepted")
	fx.expect(t, message("course.create", token, `{"id":"c1","title":"T","orgId":"o1"}`), "accepted")
	// References must be canonical ids, as on /api/v1 (readmodel.CheckRefs).
	fx.expect(t, message("course.create", token, `{"id":"c2","title":"T","orgId":"uni"}`), "rejected")
	fx.expect(t, message("course.delete", token, `{"id":"c1"}`), "accepted")
	_, err = fx.catalog.Courses.Get(ctx, "c1")
	require.ErrorIs(t, err, domain.ErrNotFound, "course after delete")
	fx.expect(t, message("person.delete", token, `{"id":"p1"}`), "accepted")

	elm, err := os.ReadFile("../../../convert/testdata/edc_bsc.json")
	require.NoError(t, err)
	body, _ := json.Marshal(map[string]any{"metadata": map[string]string{"input_format": "elm"}, "ttl": "1h", "data": json.RawMessage(elm)})
	fx.expect(t, message("populate", token, string(body)), "accepted")
	page, err := fx.catalog.Results.List(ctx, port.ListParams{Limit: 10})
	require.NoError(t, err, "results after populate")
	assert.NotZero(t, page.Total, "results after populate")
}

func TestClientErrorsAreRejected(t *testing.T) {
	fx := newFixture(t)
	token := fx.iss.Token(t, "uni", "sis.write")
	cases := map[string]struct {
		msg  *amqp.Message
		cond amqp.ErrCond
	}{
		"unknown subject":   {message("person.patch", token, `{}`), amqp.ErrCondInvalidField},
		"unknown field":     {message("person.create", token, `{"givenName":"A","familyName":"B","status":"active","bogus":1}`), amqp.ErrCondInvalidField},
		"trailing json":     {message("person.create", token, `{"givenName":"A","familyName":"B","status":"active"} {}`), amqp.ErrCondInvalidField},
		"validation":        {message("person.create", token, `{"givenName":"A"}`), amqp.ErrCondInvalidField},
		"update without id": {message("course.update", token, `{"title":"T"}`), amqp.ErrCondInvalidField},
		"not found":         {message("person.delete", token, `{"id":"nobody"}`), amqp.ErrCondNotFound},
		"bad ttl":           {message("populate", token, `{"metadata":{"input_format":"elm"},"ttl":"soon","data":{}}`), amqp.ErrCondInvalidField},
		"no body":           {&amqp.Message{Properties: &amqp.MessageProperties{Subject: new("person.create")}, ApplicationProperties: map[string]any{authorizationProperty: "Bearer " + token}}, amqp.ErrCondInvalidField},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := fx.expect(t, tc.msg, "rejected")
			require.NotNil(t, got.err, "rejection")
			assert.Equal(t, tc.cond, got.err.Condition, "rejection = %+v", got.err)
		})
	}
	fx.expect(t, message("person.create", token, `{"id":"dup","givenName":"A","familyName":"B","status":"active"}`), "accepted")
	got := fx.expect(t, message("person.create", token, `{"id":"dup","givenName":"A","familyName":"B","status":"active"}`), "rejected")
	assert.Equal(t, amqp.ErrCondPreconditionFailed, got.err.Condition, "conflict condition")
}

func TestAuthorization(t *testing.T) {
	body := `{"givenName":"A","familyName":"B","status":"active"}`

	t.Run("token required without fallback scopes", func(t *testing.T) {
		fx := newFixture(t)
		cases := map[string]*amqp.Message{
			"no token":      message("person.create", "", body),
			"read only":     message("person.create", fx.iss.Token(t, "uni", "sis.read"), body),
			"invalid token": message("person.create", "not-a-jwt", body),
			"not bearer": func() *amqp.Message {
				msg := message("person.create", "", body)
				msg.ApplicationProperties = map[string]any{authorizationProperty: "Basic Zm9vOmJhcg=="}
				return msg
			}(),
		}
		for name, msg := range cases {
			t.Run(name, func(t *testing.T) {
				got := fx.expect(t, msg, "rejected")
				assert.Equal(t, amqp.ErrCondUnauthorizedAccess, got.err.Condition)
			})
		}
	})

	t.Run("fallback scopes trust messages without a token", func(t *testing.T) {
		fx := newFixture(t, auth.ScopeWrite)
		fx.expect(t, message("person.create", "", body), "accepted")
		// A token that is present is still checked.
		fx.expect(t, message("person.create", fx.iss.Token(t, "uni", "sis.read"), body), "rejected")
	})

	// RabbitMQ 4 converts an AMQP 0-9-1 longstr header above 4096 bytes to
	// AMQP 1.0 binary, so a long token arrives as []byte.
	t.Run("binary authorization", func(t *testing.T) {
		fx := newFixture(t)
		bin := func(v []byte) *amqp.Message {
			msg := message("person.create", "", body)
			msg.ApplicationProperties = map[string]any{authorizationProperty: v}
			return msg
		}
		fx.expect(t, bin([]byte("Bearer "+fx.iss.Token(t, "uni", "sis.write"))), "accepted")
		got := fx.expect(t, bin([]byte("Bearer "+fx.iss.Token(t, "uni", "sis.read"))), "rejected")
		assert.Equal(t, amqp.ErrCondUnauthorizedAccess, got.err.Condition, "read-only binary token")
		got = fx.expect(t, bin([]byte("Bearer \xff\xfe")), "rejected")
		assert.Equal(t, amqp.ErrCondUnauthorizedAccess, got.err.Condition, "non-UTF-8 binary")
	})

	t.Run("issuer outage is retried", func(t *testing.T) {
		fx := newFixture(t)
		token := fx.iss.Token(t, "uni", "sis.write")
		fx.iss.Down.Store(true)
		fx.expect(t, message("person.create", token, body), "retry")
	})
}

// TestAMQP091Type: RabbitMQ hands an AMQP 0-9-1 message's type to AMQP 1.0
// consumers as the x-basic-type annotation.
func TestAMQP091Type(t *testing.T) {
	fx := newFixture(t, auth.ScopeWrite)
	msg := &amqp.Message{
		Annotations: amqp.Annotations{"x-basic-type": "person.create"},
		Properties:  &amqp.MessageProperties{MessageID: "legacy"},
		Data:        [][]byte{[]byte(`{"givenName":"A","familyName":"B","status":"active"}`)},
	}
	fx.link.in <- msg
	got := <-fx.link.settled
	assert.Equal(t, "accepted", got.outcome, "%+v", got.err)
}

func TestReconnectsAfterLinkFailure(t *testing.T) {
	minRetryDelay = 10 * time.Millisecond
	t.Cleanup(func() { minRetryDelay = time.Second })
	fx := newFixture(t, auth.ScopeWrite)
	fx.link.fail <- &amqp.LinkError{}
	fx.expect(t, message("person.create", "", `{"givenName":"A","familyName":"B","status":"active"}`), "accepted")
}

// TestSettlementFailureReconnects: a delivery left unsettled holds its
// credit, so the link is closed and reopened (the broker redelivers it)
// rather than left to stall.
func TestSettlementFailureReconnects(t *testing.T) {
	minRetryDelay = 10 * time.Millisecond
	t.Cleanup(func() { minRetryDelay = time.Second })
	fx := newFixture(t, auth.ScopeWrite)
	body := `{"givenName":"A","familyName":"B","status":"active"}`
	fx.link.acceptErr <- context.DeadlineExceeded
	fx.link.in <- message("person.create", "", body)
	select {
	case <-fx.link.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("link not closed after a failed settlement")
	}
	fx.expect(t, message("person.create", "", body), "accepted")
}

// TestCloseStopsDispatching: messages the link already holds are not run
// once Close has begun; closing the link releases them to the broker.
func TestCloseStopsDispatching(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	fake := newFakeLink()
	started, release := make(chan struct{}), make(chan struct{})
	var runs atomic.Int32
	table := map[string]handlerFunc{"slow": func(context.Context, []byte) error {
		if runs.Add(1) == 1 {
			close(started)
			<-release
		}
		return nil
	}}
	amqpCfg := model.AMQPAPI{Prefetch: 1, MaxMessageBytes: 1 << 20, Scopes: []string{auth.ScopeWrite}}
	svc := start(t.Context(), amqpCfg, nil, table, func(context.Context) (link, error) { return fake, nil }, log)

	first := message("slow", "", `{}`)
	fake.in <- first
	<-started
	fake.in <- message("slow", "", `{}`) // prefetched, not yet dispatched
	svc.stopRecv()                       // what Close does first
	close(release)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, svc.Close(ctx), "Close")
	got := <-fake.settled
	assert.Same(t, first, got.msg, "first settlement")
	assert.Equal(t, "accepted", got.outcome, "first settlement")
	assert.Equal(t, int32(1), runs.Load(), "runs after Close")
	assert.Empty(t, fake.settled, "more settlements after Close")
}

func TestReceiverOptionsCapMessageSize(t *testing.T) {
	opts := receiverOptions(model.AMQPAPI{Prefetch: 7, MaxMessageBytes: 1000})
	assert.Equal(t, int32(7), opts.Credit, "credit")
	assert.Equal(t, uint64(1000+envelopeBytes), opts.MaxMessageSize, "max size")
}

// TestReceiverOptionsRequireUnsettled: settlement reports the outcome, so the
// broker must not pre-settle deliveries.
func TestReceiverOptionsRequireUnsettled(t *testing.T) {
	opts := receiverOptions(model.AMQPAPI{Prefetch: 1, MaxMessageBytes: 1})
	require.NotNil(t, opts.RequestedSenderSettleMode, "RequestedSenderSettleMode")
	assert.Equal(t, amqp.SenderSettleModeUnsettled, *opts.RequestedSenderSettleMode, "RequestedSenderSettleMode")
}

// TestRejectionIsBounded: an error quoting a huge field name must not make a
// disposition frame the broker refuses (that would fail the connection).
func TestRejectionIsBounded(t *testing.T) {
	fx := newFixture(t, auth.ScopeWrite)
	body := `{"givenName":"A","familyName":"B","status":"active","` + strings.Repeat("x", 200_000) + `":1}`
	got := fx.expect(t, message("person.create", "", body), "rejected")
	require.Equal(t, amqp.ErrCondInvalidField, got.err.Condition, "rejection")
	require.LessOrEqual(t, len(got.err.Description), maxRejectionBytes, "rejection description bytes")
	var parsed struct{ Title string }
	require.NoError(t, json.Unmarshal([]byte(got.err.Description), &parsed), "description %q", got.err.Description)
	require.NotEmpty(t, parsed.Title, "description %q", got.err.Description)
	// A short one keeps its details.
	got = fx.expect(t, message("person.create", "", `{"givenName":"A","familyName":"B","status":"active","bogus":1}`), "rejected")
	assert.Contains(t, got.err.Description, "bogus", "description lost its details")
}

// TestBackoffGrowsOverShortLivedLinks: a link that attaches and then fails at
// once does not reset the backoff.
func TestBackoffGrowsOverShortLivedLinks(t *testing.T) {
	minRetryDelay, maxRetryDelay = 10*time.Millisecond, 80*time.Millisecond
	t.Cleanup(func() { minRetryDelay, maxRetryDelay = time.Second, 30*time.Second })
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	dials := make(chan time.Time, 16)
	dial := func(context.Context) (link, error) {
		dials <- time.Now()
		fake := newFakeLink()
		fake.fail <- &amqp.LinkError{}
		return fake, nil
	}
	amqpCfg := model.AMQPAPI{Prefetch: 1, MaxMessageBytes: 1 << 20}
	svc := start(t.Context(), amqpCfg, nil, nil, dial, log)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	var at []time.Time
	for range 5 {
		select {
		case d := <-dials:
			at = append(at, d)
		case <-time.After(5 * time.Second):
			t.Fatal("no redial")
		}
	}
	// Delays 10, 20, 40, 80 ms: the fourth gap is at least 80 ms.
	assert.GreaterOrEqual(t, at[4].Sub(at[3]), maxRetryDelay, "fourth redial gap")
}

// TestRejectionIsLogged: the log carries the description the broker got
// (field and reason), so an operator can tell why a command was
// dead-lettered.
func TestRejectionIsLogged(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter("test", false, "warn", &buf)
	fake := newFakeLink()
	table := map[string]handlerFunc{"bad": func(context.Context, []byte) error {
		return &domain.ValidationError{Field: "ttl", Msg: "must be a positive Go duration"}
	}}
	amqpCfg := model.AMQPAPI{Prefetch: 1, MaxMessageBytes: 1 << 20, Scopes: []string{auth.ScopeWrite}}
	svc := start(t.Context(), amqpCfg, nil, table, func(context.Context) (link, error) { return fake, nil }, log)
	fx := &fixture{link: fake}
	got := fx.expect(t, message("bad", "", `{}`), "rejected")
	// Close waits for process to finish, so the log line is written.
	require.NoError(t, svc.Close(t.Context()))
	out := buf.String()
	for _, want := range []string{"command rejected", "ttl", "must be a positive Go duration", string(amqp.ErrCondInvalidField)} {
		assert.Contains(t, out, want, "log lacks %q", want)
	}
	// The text handler quotes the value.
	assert.Contains(t, out, "error="+strconv.Quote(got.err.Description), "log lacks the broker's description")
}

// TestCommandTimeoutIsRetried: a command running past command_timeout is
// cancelled, and its message goes back to the broker rather than holding a
// prefetch slot until shutdown.
func TestCommandTimeoutIsRetried(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	fake := newFakeLink()
	table := map[string]handlerFunc{"stuck": func(ctx context.Context, _ []byte) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	amqpCfg := model.AMQPAPI{Prefetch: 1, MaxMessageBytes: 1 << 20, CommandTimeout: 50 * time.Millisecond, Scopes: []string{auth.ScopeWrite}}
	svc := start(t.Context(), amqpCfg, nil, table, func(context.Context) (link, error) { return fake, nil }, log)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	fx := &fixture{link: fake}
	fx.expect(t, message("stuck", "", `{}`), "retry")
}

// TestLateCreateIsAcceptedOnce: a create without id or sourcedId that
// commits and then runs past command_timeout (e.g. slow event logging) is
// accepted, so it is not redelivered into a second record under a fresh id.
func TestLateCreateIsAcceptedOnce(t *testing.T) {
	fx := newFixture(t)
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	commands, err := command.New(t.Context(), &model.Cfg{}, fx.catalog, log)
	require.NoError(t, err)
	create := newCommandTable(commands, fx.catalog)["person.create"]
	table := map[string]handlerFunc{"person.create": func(ctx context.Context, body []byte) error {
		err := create(ctx, body)
		<-ctx.Done() // the deadline passes after the insert
		return err
	}}
	fake := newFakeLink()
	amqpCfg := model.AMQPAPI{Prefetch: 1, MaxMessageBytes: 1 << 20, CommandTimeout: 50 * time.Millisecond, Scopes: []string{auth.ScopeWrite}}
	svc := start(t.Context(), amqpCfg, nil, table, func(context.Context) (link, error) { return fake, nil }, log)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })

	(&fixture{link: fake}).expect(t, message("person.create", "", `{"givenName":"A","familyName":"B","status":"active"}`), "accepted")
	page, err := fx.catalog.Persons.ListPersons(t.Context(), port.ListParams{})
	require.NoError(t, err)
	assert.Equal(t, 1, page.Total, "one delivery, one record")
}

// TestStalledDiscoveryFreesSlots: with discovery hanging, concurrent
// deliveries end at command_timeout and go back to the broker, instead of
// holding their prefetch slots until the HTTP timeout.
func TestStalledDiscoveryFreesSlots(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	iss := authtest.NewIssuer(t)
	iss.Stall.Store(true)
	verifier, err := auth.New(t.Context(), &model.Cfg{SIS: model.SIS{Auth: model.Auth{Issuers: []model.Issuer{iss.Config()}}}}, log)
	require.NoError(t, err)
	table := map[string]handlerFunc{"noop": func(context.Context, []byte) error { return nil }}
	fake := newFakeLink()
	amqpCfg := model.AMQPAPI{Prefetch: 3, MaxMessageBytes: 1 << 20, CommandTimeout: 100 * time.Millisecond}
	svc := start(t.Context(), amqpCfg, verifier, table, func(context.Context) (link, error) { return fake, nil }, log)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })

	token := iss.Token(t, "x", auth.ScopeWrite)
	msgs := []*amqp.Message{message("noop", token, `{}`), message("noop", token, `{}`), message("noop", token, `{}`)}
	began := time.Now()
	for _, msg := range msgs {
		fake.in <- msg
	}
	for range msgs {
		select {
		case got := <-fake.settled:
			assert.Equal(t, "retry", got.outcome)
		case <-time.After(5 * time.Second):
			t.Fatal("message not settled")
		}
	}
	assert.Less(t, time.Since(began), 2*time.Second, "deliveries must end at command_timeout")
}

// TestInternalErrorsAreRetried: an unclassified failure (e.g. the database
// is down) is not the message's fault, so it goes back to the broker.
func TestInternalErrorsAreRetried(t *testing.T) {
	log := logger.NewWithWriter("test", false, "error", io.Discard)
	fake := newFakeLink()
	table := map[string]handlerFunc{"boom": func(context.Context, []byte) error { return errors.New("connection refused") }}
	amqpCfg := model.AMQPAPI{Prefetch: 1, MaxMessageBytes: 1 << 20, Scopes: []string{auth.ScopeWrite}}
	svc := start(t.Context(), amqpCfg, nil, table, func(context.Context) (link, error) { return fake, nil }, log)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	fx := &fixture{link: fake}
	fx.expect(t, message("boom", "", `{}`), "retry")
}
