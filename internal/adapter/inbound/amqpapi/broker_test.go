package amqpapi

import (
	"context"
	"errors"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/go-amqp"

	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// TestBroker runs the consumer against a real AMQP 1.0 broker. It uses its
// own queues from developer_tools/rabbitmq/definitions.json,
// /queues/sis.test.commands (delivery limit, dead-lettering through
// sis.test.dlx) and /queues/sis.test.commands.dlq, never the dev
// application's sis.commands: SIS may be consuming that queue, and draining
// its dead-letter queue would destroy dev traffic.
// Set SIS_TEST_AMQP_URL, e.g. amqp://sis:sis@rabbitmq:5672.
func TestBroker(t *testing.T) {
	url := os.Getenv("SIS_TEST_AMQP_URL")
	if url == "" {
		t.Skip("SIS_TEST_AMQP_URL not set")
	}
	const queue, dlq = "/queues/sis.test.commands", "/queues/sis.test.commands.dlq"
	ctx := t.Context()
	conn, err := amqp.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	sess, err := conn.NewSession(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	snd, err := sess.NewSender(ctx, queue, nil)
	if err != nil {
		t.Fatal(err)
	}
	dead, err := sess.NewReceiver(ctx, dlq, nil)
	if err != nil {
		t.Fatal(err)
	}
	drain(t, dead)

	var attempts atomic.Int32
	accepted := make(chan struct{}, 4)
	table := map[string]handlerFunc{
		"ok":    func(context.Context, []byte) error { accepted <- struct{}{}; return nil },
		"bad":   func(context.Context, []byte) error { return errIDRequired },
		"flaky": func(context.Context, []byte) error { attempts.Add(1); return errors.New("database unavailable") },
	}
	ac := model.AMQPAPI{URL: url, Address: queue, Prefetch: 1, MaxMessageBytes: 1 << 20, Scopes: []string{auth.ScopeWrite}}
	dial, err := newDialer(ac)
	if err != nil {
		t.Fatal(err)
	}
	s := start(ctx, ac, nil, table, dial, logger.NewWithWriter("test", false, "error", io.Discard))
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	send := func(subject string) {
		t.Helper()
		m := &amqp.Message{Properties: &amqp.MessageProperties{Subject: &subject, MessageID: subject}, Data: [][]byte{[]byte(`{}`)}}
		if err := snd.Send(ctx, m, nil); err != nil {
			t.Fatal(err)
		}
	}

	send("ok")
	select {
	case <-accepted:
	case <-time.After(10 * time.Second):
		t.Fatal("ok command not consumed")
	}

	send("bad")
	if m := receive(t, dead); m.Properties.MessageID != "bad" {
		t.Fatalf("dead-lettered %v, want bad", m.Properties.MessageID)
	}

	send("flaky")
	if m := receive(t, dead); m.Properties.MessageID != "flaky" {
		t.Fatalf("dead-lettered %v, want flaky", m.Properties.MessageID)
	}
	if n := attempts.Load(); n < 2 {
		t.Fatalf("flaky command ran %d times; want it redelivered", n)
	}
}

func receive(t *testing.T, r *amqp.Receiver) *amqp.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	m, err := r.Receive(ctx, nil)
	if err != nil {
		t.Fatalf("receive from dead-letter queue: %v", err)
	}
	_ = r.AcceptMessage(ctx, m)
	return m
}

// drain discards leftovers of earlier runs.
func drain(t *testing.T, r *amqp.Receiver) {
	t.Helper()
	for {
		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		m, err := r.Receive(ctx, nil)
		cancel()
		if err != nil {
			return
		}
		_ = r.AcceptMessage(t.Context(), m)
	}
}
