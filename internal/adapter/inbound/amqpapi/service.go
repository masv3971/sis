// Package amqpapi is the inbound AMQP 1.0 adapter: it consumes write commands
// from one broker address (a queue) and runs them through the same command
// layer as the /api/v1 REST handlers. There are no replies; the outcome is
// the message's settlement: accepted, rejected (the broker dead-letters it),
// or modified as failed (the broker redelivers it, up to its delivery limit).
//
// Service vs Client: this is a Service because it owns the broker connection
// and the consumer goroutines, which Close stops.
package amqpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Azure/go-amqp"

	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/internal/command"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/helpers"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// Reconnect backoff bounds, how long a link must stay up before the backoff
// starts over, and the time allowed to settle one message.
var (
	minRetryDelay = time.Second
	maxRetryDelay = 30 * time.Second
	stableLink    = time.Minute
	settleTimeout = 5 * time.Second
)

// maxRejectionBytes bounds the description of a rejection. A disposition
// travels in one frame and AMQP lets a peer cap frames at 512 bytes; the rest
// of the frame takes well under 112. A larger frame would fail the whole
// connection instead of rejecting one message.
const maxRejectionBytes = 400

// tokenVerifier is the part of *auth.Verifier the consumer needs.
type tokenVerifier interface {
	Verify(ctx context.Context, raw string) (*auth.Claims, error)
}

// Service consumes sis.adapter.inbound.amqpapi.address.
type Service struct {
	cfg      model.AMQPAPI
	verifier tokenVerifier
	commands map[string]handlerFunc
	dial     dialFunc
	// stopRecv stops taking new messages; abort also cancels the commands
	// in flight, once Close's deadline passes.
	stopRecv context.CancelFunc
	abort    context.CancelFunc
	done     chan struct{}
	log      *logger.Log
}

// New starts consuming in the background and returns at once: a broker that
// is down is retried with backoff rather than failing startup. Settings
// errors (URL, CA files) fail New.
func New(ctx context.Context, cfg *model.Cfg, verifier *auth.Verifier, commands *command.Client, catalog *readmodel.Client, log *logger.Log) (*Service, error) {
	ac := cfg.SIS.Adapter.Inbound.AMQPAPI
	dial, err := newDialer(ac)
	if err != nil {
		return nil, err
	}
	log = log.New("amqpapi")
	if cfg.Common.Production && len(ac.Scopes) > 0 {
		log.Warn("messages without a token are trusted with sis.adapter.inbound.amqpapi.scopes", "scopes", ac.Scopes)
	}
	return start(ctx, ac, verifier, newCommandTable(commands, catalog), dial, log), nil
}

func start(ctx context.Context, ac model.AMQPAPI, verifier tokenVerifier, commands map[string]handlerFunc, dial dialFunc, log *logger.Log) *Service {
	workCtx, abort := context.WithCancel(context.WithoutCancel(ctx))
	recvCtx, stopRecv := context.WithCancel(workCtx)
	s := &Service{
		cfg: ac, verifier: verifier, commands: commands, dial: dial,
		stopRecv: stopRecv, abort: abort, done: make(chan struct{}), log: log,
	}
	go s.run(recvCtx, workCtx)
	return s
}

// Close stops receiving and waits for the commands in flight to be settled.
// When ctx is done first they are cancelled (and released to the broker).
func (s *Service) Close(ctx context.Context) error {
	s.log.Info("stopping amqp consumer")
	s.stopRecv()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		s.abort()
		return ctx.Err()
	}
}

// run keeps one receiver link open, reconnecting with backoff. The backoff
// starts over only after a link stayed up for stableLink, so a broker that
// accepts the attach and then drops the link at once is not redialled at the
// minimum interval forever.
func (s *Service) run(recvCtx, workCtx context.Context) {
	defer close(s.done)
	delay := minRetryDelay
	for recvCtx.Err() == nil {
		l, err := s.dial(recvCtx)
		if err == nil {
			s.log.Info("consuming commands", "address", s.cfg.Address, "prefetch", s.cfg.Prefetch)
			up := time.Now()
			err = s.consume(recvCtx, workCtx, l)
			closeCtx, cancel := context.WithTimeout(context.WithoutCancel(workCtx), settleTimeout)
			_ = l.Close(closeCtx)
			cancel()
			if time.Since(up) >= stableLink {
				delay = minRetryDelay
			}
		}
		if recvCtx.Err() != nil {
			return
		}
		s.log.Warn("broker link down; reconnecting", "err", err, "retry_in", delay.String())
		select {
		case <-recvCtx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(2*delay, maxRetryDelay)
	}
}

// consume runs up to prefetch commands at once until the link fails, a
// settlement fails or recvCtx is cancelled, then waits for the commands in
// flight to be settled. A failed settlement leaves its delivery unsettled
// and the link without that credit, so the link is given up: run closes it
// and reconnects, and the broker redelivers what was not settled. A message
// received after the stop is not dispatched; closing the link releases it.
func (s *Service) consume(recvCtx, workCtx context.Context, l link) error {
	linkCtx, fail := context.WithCancelCause(recvCtx)
	defer fail(nil)
	var wg sync.WaitGroup
	defer wg.Wait()
	slots := make(chan struct{}, s.cfg.Prefetch)
	for {
		// Receive hands out prefetched messages even when its ctx is done,
		// so check before taking a slot and again before dispatching.
		if linkCtx.Err() != nil {
			return context.Cause(linkCtx)
		}
		select {
		case slots <- struct{}{}:
		case <-linkCtx.Done():
			return context.Cause(linkCtx)
		}
		msg, err := l.Receive(linkCtx, nil)
		if linkCtx.Err() != nil {
			return context.Cause(linkCtx)
		}
		if err != nil {
			return err
		}
		wg.Go(func() {
			defer func() { <-slots }()
			if err := s.process(workCtx, l, msg); err != nil {
				fail(err)
			}
		})
	}
}

// process runs one command and settles its message. It returns the error of
// a failed settlement.
func (s *Service) process(ctx context.Context, l link, msg *amqp.Message) error {
	began := time.Now()
	err := s.handle(ctx, msg)
	e := helpers.NewErrorFromError(err)

	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
	defer cancel()
	var outcome string
	var settleErr error
	var rej *amqp.Error
	switch {
	case e == nil:
		outcome = "accepted"
		settleErr = l.AcceptMessage(settleCtx, msg)
	case e.Status() >= http.StatusInternalServerError:
		outcome = "retry"
		settleErr = l.ModifyMessage(settleCtx, msg, &amqp.ModifyMessageOptions{DeliveryFailed: true})
	default:
		outcome = "rejected"
		rej = rejection(e)
		settleErr = l.RejectMessage(settleCtx, msg, rej)
	}

	props := msg.Properties
	if props == nil {
		props = &amqp.MessageProperties{}
	}
	attrs := []any{
		"subject", subjectOf(msg), "message_id", idString(props.MessageID),
		"correlation_id", idString(props.CorrelationID), "outcome", outcome,
		"duration", time.Since(began).String(),
	}
	switch {
	case settleErr != nil:
		s.log.Error("settle message", append(attrs, "err", settleErr)...)
		return fmt.Errorf("settle message: %w", settleErr)
	case outcome == "retry":
		s.log.Error("command failed; redelivering", append(attrs, "err", err)...)
	case outcome == "rejected":
		// The same bounded description the broker got, so the log names
		// the field and the reason (e.g. an invalid ttl), not just a title.
		s.log.Warn("command rejected", append(attrs, "condition", string(rej.Condition), "error", rej.Description)...)
	default:
		s.log.Info("command", attrs...)
	}
	return nil
}

// rejection describes a client error for the dead-lettered message. The
// detail of a classified *helpers.Error is safe to return. A description
// above maxRejectionBytes (e.g. a decoder error quoting a huge field name)
// keeps only the title.
func rejection(e *helpers.Error) *amqp.Error {
	desc, err := json.Marshal(e)
	if err != nil || len(desc) > maxRejectionBytes {
		desc, err = json.Marshal(&helpers.Error{Title: e.Title, Err: "details omitted: too long"})
		if err != nil || len(desc) > maxRejectionBytes {
			desc = []byte(`{"title":"rejected"}`)
		}
	}
	cond := amqp.ErrCondInvalidField
	switch e.Status() {
	case http.StatusUnauthorized, http.StatusForbidden:
		cond = amqp.ErrCondUnauthorizedAccess
	case http.StatusNotFound:
		cond = amqp.ErrCondNotFound
	case http.StatusConflict:
		cond = amqp.ErrCondPreconditionFailed
	case http.StatusRequestEntityTooLarge:
		cond = amqp.ErrCondMessageSizeExceeded
	}
	return &amqp.Error{Condition: cond, Description: string(desc)}
}

// subjectOf returns the command name: the subject property, or the type
// property of a message an AMQP 0-9-1 client published to RabbitMQ.
func subjectOf(msg *amqp.Message) string {
	if msg.Properties != nil && msg.Properties.Subject != nil {
		return *msg.Properties.Subject
	}
	if t, ok := msg.Annotations["x-basic-type"].(string); ok {
		return t
	}
	return ""
}

// idString renders a message-id or correlation-id, which AMQP allows to be a
// string, UUID, ulong or binary.
func idString(id any) string {
	switch v := id.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return fmt.Sprintf("%x", v)
	case amqp.UUID:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}
