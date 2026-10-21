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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	session, err := conn.NewSession(ctx, nil)
	require.NoError(t, err)
	sender, err := session.NewSender(ctx, queue, nil)
	require.NoError(t, err)
	deadLetters, err := session.NewReceiver(ctx, dlq, nil)
	require.NoError(t, err)
	drain(t, deadLetters)

	var attempts atomic.Int32
	accepted := make(chan struct{}, 4)
	table := map[string]handlerFunc{
		"ok":    func(context.Context, []byte) error { accepted <- struct{}{}; return nil },
		"bad":   func(context.Context, []byte) error { return errIDRequired },
		"flaky": func(context.Context, []byte) error { attempts.Add(1); return errors.New("database unavailable") },
	}
	amqpCfg := model.AMQPAPI{URL: url, Address: queue, Prefetch: 1, MaxMessageBytes: 1 << 20, Scopes: []string{auth.ScopeWrite}}
	dial, err := newDialer(amqpCfg)
	require.NoError(t, err)
	svc := start(ctx, amqpCfg, nil, table, dial, logger.NewWithWriter("test", false, "error", io.Discard))
	t.Cleanup(func() { _ = svc.Close(context.Background()) })

	send := func(subject string) {
		t.Helper()
		msg := &amqp.Message{Properties: &amqp.MessageProperties{Subject: &subject, MessageID: subject}, Data: [][]byte{[]byte(`{}`)}}
		require.NoError(t, sender.Send(ctx, msg, nil))
	}

	send("ok")
	select {
	case <-accepted:
	case <-time.After(10 * time.Second):
		t.Fatal("ok command not consumed")
	}

	send("bad")
	require.Equal(t, "bad", receive(t, deadLetters).Properties.MessageID, "dead-lettered")

	send("flaky")
	require.Equal(t, "flaky", receive(t, deadLetters).Properties.MessageID, "dead-lettered")
	assert.GreaterOrEqual(t, attempts.Load(), int32(2), "flaky command must be redelivered")
}

func receive(t *testing.T, receiver *amqp.Receiver) *amqp.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	msg, err := receiver.Receive(ctx, nil)
	require.NoError(t, err, "receive from dead-letter queue")
	_ = receiver.AcceptMessage(ctx, msg)
	return msg
}

// drain discards leftovers of earlier runs.
func drain(t *testing.T, receiver *amqp.Receiver) {
	t.Helper()
	for {
		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		msg, err := receiver.Receive(ctx, nil)
		cancel()
		if err != nil {
			return
		}
		_ = receiver.AcceptMessage(t.Context(), msg)
	}
}
