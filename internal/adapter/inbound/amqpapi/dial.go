package amqpapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Azure/go-amqp"

	"github.com/SUNET/sis/pkg/model"
)

var dialTimeout = 10 * time.Second

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
func newDialer(ac model.AMQPAPI) (dialFunc, error) {
	u, err := url.Parse(ac.URL)
	if err != nil || (u.Scheme != "amqp" && u.Scheme != "amqps") || u.Hostname() == "" {
		return nil, errors.New("amqpapi: url must be amqp:// or amqps://host[:port]")
	}
	if ac.Prefetch <= 0 || ac.Prefetch > math.MaxInt32 {
		return nil, errors.New("amqpapi: prefetch must be positive")
	}
	opts := &amqp.ConnOptions{}
	if u.User == nil {
		opts.SASLType = amqp.SASLTypeAnonymous()
	}
	if len(ac.CAFiles) > 0 {
		if !strings.EqualFold(u.Scheme, "amqps") {
			return nil, errors.New("amqpapi: ca_files need an amqps:// url")
		}
		pool, err := certPool(ac.CAFiles)
		if err != nil {
			return nil, err
		}
		opts.TLSConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	credit := int32(ac.Prefetch) //nolint:gosec // bounded above
	return func(ctx context.Context) (link, error) {
		ctx, cancel := context.WithTimeout(ctx, dialTimeout)
		defer cancel()
		conn, err := amqp.Dial(ctx, ac.URL, opts)
		if err != nil {
			return nil, fmt.Errorf("dial %s: %w", u.Redacted(), err)
		}
		sess, err := conn.NewSession(ctx, nil)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("begin session: %w", err)
		}
		rcv, err := sess.NewReceiver(ctx, ac.Address, &amqp.ReceiverOptions{Credit: credit})
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("attach receiver to %s: %w", ac.Address, err)
		}
		return brokerLink{Receiver: rcv, conn: conn}, nil
	}, nil
}

// certPool returns the system roots plus the PEM certificates in files.
func certPool(files []string) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	for _, f := range files {
		pemData, err := os.ReadFile(f) //nolint:gosec // path comes from operator config
		if err != nil {
			return nil, fmt.Errorf("amqpapi: read ca file: %w", err)
		}
		if !pool.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("amqpapi: ca file %s holds no PEM certificate", f)
		}
	}
	return pool, nil
}
