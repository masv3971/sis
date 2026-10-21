package amqpapi

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/Azure/go-amqp"

	"github.com/SUNET/sis/pkg/helpers"
	"github.com/SUNET/sis/pkg/model"
)

var dialTimeout = 10 * time.Second

// envelopeBytes is what a message may carry on top of its body: header,
// properties, annotations and the authorization token.
const envelopeBytes = 64 << 10

// link is the receiver side of a broker connection; *amqp.Receiver plus a
// Close that also closes the connection. Tests replace it with a fake.
type link interface {
	Receive(ctx context.Context, opts *amqp.ReceiveOptions) (*amqp.Message, error)
	AcceptMessage(ctx context.Context, msg *amqp.Message) error
	RejectMessage(ctx context.Context, msg *amqp.Message, e *amqp.Error) error
	ModifyMessage(ctx context.Context, msg *amqp.Message, opts *amqp.ModifyMessageOptions) error
	Close(ctx context.Context) error
}

// dialFunc opens a connection and a receiver link on the configured address.
type dialFunc func(ctx context.Context) (link, error)

type brokerLink struct {
	*amqp.Receiver
	conn *amqp.Conn
}

func (b brokerLink) Close(ctx context.Context) error {
	return errors.Join(b.Receiver.Close(ctx), b.conn.Close())
}

// newDialer checks the settings and returns the dialer. Credentials come from
// the URL's userinfo (SASL PLAIN); without them SASL ANONYMOUS is used.
func newDialer(cfg model.AMQPAPI) (dialFunc, error) {
	brokerURL, err := url.Parse(cfg.URL)
	if err != nil || (brokerURL.Scheme != "amqp" && brokerURL.Scheme != "amqps") || brokerURL.Hostname() == "" {
		return nil, errors.New("amqpapi: url must be amqp:// or amqps://host[:port]")
	}
	if cfg.Prefetch <= 0 || cfg.Prefetch > math.MaxInt32 {
		return nil, errors.New("amqpapi: prefetch must be positive")
	}
	connOpts := &amqp.ConnOptions{}
	if brokerURL.User == nil {
		connOpts.SASLType = amqp.SASLTypeAnonymous()
	}
	if len(cfg.CAFiles) > 0 {
		if !strings.EqualFold(brokerURL.Scheme, "amqps") {
			return nil, errors.New("amqpapi: ca_files need an amqps:// url")
		}
		pool, err := helpers.CertPool(cfg.CAFiles)
		if err != nil {
			return nil, fmt.Errorf("amqpapi: %w", err)
		}
		connOpts.TLSConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	receiverOpts := receiverOptions(cfg)
	return func(ctx context.Context) (link, error) {
		ctx, cancel := context.WithTimeout(ctx, dialTimeout)
		defer cancel()
		conn, err := amqp.Dial(ctx, cfg.URL, connOpts)
		if err != nil {
			return nil, fmt.Errorf("dial %s: %w", brokerURL.Redacted(), err)
		}
		session, err := conn.NewSession(ctx, nil)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("begin session: %w", err)
		}
		receiver, err := session.NewReceiver(ctx, cfg.Address, receiverOpts)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("attach receiver to %s: %w", cfg.Address, err)
		}
		return brokerLink{Receiver: receiver, conn: conn}, nil
	}, nil
}

// receiverOptions sets the credit to prefetch and caps the encoded message at
// max_message_bytes plus envelopeBytes. go-amqp buffers a whole delivery
// before Receive returns it, so without a wire cap a large message would be
// read into memory before the body cap or the token is checked. A delivery
// above the cap closes the link (message-size-exceeded): the broker returns
// the message, and its delivery limit dead-letters it.
//
// The sender (broker) must send deliveries unsettled: settlement is how SIS
// reports the outcome, and go-amqp's Accept/Reject/Modify are silent no-ops
// on a message that arrived settled, so a failed command would be lost while
// logged as redelivered. Requesting the mode makes the attach fail when the
// broker cannot honour it.
func receiverOptions(cfg model.AMQPAPI) *amqp.ReceiverOptions {
	return &amqp.ReceiverOptions{
		Credit:                    int32(cfg.Prefetch),                         //nolint:gosec // bounded by newDialer
		MaxMessageSize:            uint64(cfg.MaxMessageBytes) + envelopeBytes, //nolint:gosec // validated gt=0
		RequestedSenderSettleMode: new(amqp.SenderSettleModeUnsettled),
	}
}
