package nats

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
	native "github.com/nats-io/nats.go"
)

// Connection owns the native connection. Consumers must not close it themselves.
type Connection struct {
	cfg          Config
	name         string
	logger       *slog.Logger
	report       lifecycle.ReportFailure
	conn         atomic.Pointer[native.Conn]
	closing      atomic.Bool
	drainFailed  atomic.Bool
	policyFailed atomic.Bool
	closed       chan struct{}
}

func newConnection(c Config, production bool, info app.Info, log *slog.Logger, reg *health.Registry, report lifecycle.ReportFailure) (*Connection, error) {
	if err := c.Validate(production); err != nil {
		return nil, err
	}
	o := &Connection{cfg: c, name: info.Name, logger: log, report: report, closed: make(chan struct{})}
	if err := reg.Register(health.Check{Name: "nats.connection", Required: true, Run: func(context.Context) error {
		nc := o.conn.Load()
		if nc == nil || !nc.IsConnected() || o.closing.Load() || o.policyFailed.Load() || isPolicyError(nc.LastError()) {
			return errors.New("NATS unavailable")
		}
		return nil
	}}); err != nil {
		return nil, err
	}
	return o, nil
}

// Conn returns the shared native connection after Open. During reconnect it is
// still valid; callers must handle transport errors. Do not Close or Drain it.
func (o *Connection) Conn() (*native.Conn, error) {
	nc := o.conn.Load()
	if nc == nil || nc.IsClosed() || o.closing.Load() {
		return nil, errors.New("NATS connection unavailable")
	}
	return nc, nil
}

func (o *Connection) participant() lifecycle.Participant {
	return lifecycle.Participant{Name: "nats.connection", Open: o.open, Close: o.close}
}
func (o *Connection) open(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timeout := o.cfg.ConnectTimeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline))
		if timeout <= 0 {
			return context.DeadlineExceeded
		}
	}
	opts := []native.Option{
		native.Name(o.name), native.Timeout(timeout), native.DrainTimeout(o.cfg.DrainTimeout),
		native.MaxReconnects(o.cfg.MaxReconnect), native.ReconnectWait(o.cfg.ReconnectWait),
		native.ReconnectBufSize(-1), native.IgnoreDiscoveredServers(),
		native.DisconnectErrHandler(func(_ *native.Conn, err error) {
			// Normal client Close also invokes this callback, with a nil error.
			if err != nil && o.closing.Load() {
				o.drainFailed.Store(true)
			}
			if err != nil {
				o.logger.Warn("NATS disconnected")
			}
		}),
		native.ReconnectHandler(func(_ *native.Conn) { o.logger.Info("NATS reconnected") }),
		native.ErrorHandler(func(_ *native.Conn, _ *native.Subscription, err error) { o.noteAsyncError(err) }),
		native.ClosedHandler(func(_ *native.Conn) {
			close(o.closed)
			if !o.closing.Load() {
				if err := o.report(errors.New("NATS connection permanently closed")); err != nil {
					o.logger.Error("NATS shutdown request failed")
				}
			}
		}),
	}
	if o.cfg.CredentialsFile != "" {
		opts = append(opts, native.UserCredentials(o.cfg.CredentialsFile))
	}
	if o.cfg.Token != "" {
		opts = append(opts, native.Token(o.cfg.Token))
	}
	if strings.HasPrefix(o.cfg.URL, "tls://") {
		tc := &tls.Config{MinVersion: tls.VersionTLS12}
		if o.cfg.CAFile != "" {
			p, e := x509.SystemCertPool()
			if e != nil {
				return errors.New("NATS trust store unavailable")
			}
			b, e := os.ReadFile(o.cfg.CAFile)
			if e != nil || !p.AppendCertsFromPEM(b) {
				return errors.New("NATS CA unavailable")
			}
			tc.RootCAs = p
		}
		if o.cfg.CertFile != "" {
			p, e := tls.LoadX509KeyPair(o.cfg.CertFile, o.cfg.KeyFile)
			if e != nil {
				return errors.New("NATS key pair unavailable")
			}
			tc.Certificates = []tls.Certificate{p}
		}
		opts = append(opts, native.Secure(tc))
	}
	nc, err := native.Connect(o.cfg.URL, opts...)
	if err != nil {
		return errors.New("NATS initial connection failed")
	}
	if err := ctx.Err(); err != nil {
		o.closing.Store(true)
		nc.Close()
		return err
	}
	o.conn.Store(nc)
	return nil
}

// Policy violations can be reported asynchronously with no subscription identity.
// Conservatively latch readiness until restart rather than claim recovery on PONG.
func isPolicyError(err error) bool {
	return errors.Is(err, native.ErrPermissionViolation) || errors.Is(err, native.ErrMaxSubscriptionsExceeded)
}

func (o *Connection) noteAsyncError(err error) {
	if isPolicyError(err) {
		o.policyFailed.Store(true)
	}
	if o.closing.Load() {
		o.drainFailed.Store(true)
	}
	o.logger.Warn("NATS asynchronous error")
}

func (o *Connection) close(ctx context.Context) error {
	o.closing.Store(true)
	nc := o.conn.Swap(nil)
	if nc == nil {
		return nil
	}
	defer nc.Close()
	if nc.IsClosed() {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	drain, cancel := context.WithTimeout(ctx, o.cfg.DrainTimeout)
	defer cancel()
	if err := nc.Drain(); err != nil {
		return errors.New("NATS connection drain failed")
	}
	select {
	case <-o.closed:
		if o.drainFailed.Load() {
			return errors.New("NATS connection closed with error")
		}
		return nil
	case <-drain.Done():
		return drain.Err()
	}
}
