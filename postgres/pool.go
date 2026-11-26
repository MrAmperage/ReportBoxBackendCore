package postgres

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ParticipantName is the lifecycle dependency for users of the shared pool.
const ParticipantName = "postgres.pool"

// Pool is a passive owner of the native pgx pool. The pool belongs to Core.
type Pool struct {
	cfg            *pgxpool.Config
	connectTimeout time.Duration
	log            *slog.Logger
	pool           atomic.Pointer[pgxpool.Pool]
}

func newPool(c Config, production bool, info app.Info, log *slog.Logger, reg *health.Registry) (*Pool, error) {
	if e := c.Validate(production); e != nil {
		return nil, e
	}
	if e := c.validateDriverEnvironment(os.LookupEnv); e != nil {
		return nil, e
	}
	u, _ := c.endpoint()
	// Resolve the default port explicitly so PGPORT cannot change the endpoint.
	port := 5432
	if u.Port() != "" {
		port, _ = strconv.Atoi(u.Port())
	}
	u.Host = net.JoinHostPort(u.Hostname(), strconv.Itoa(port))
	cfg, e := pgxpool.ParseConfig(u.String())
	if e != nil {
		return nil, errors.New("invalid PostgreSQL driver configuration")
	}
	// Do not allow ambient driver configuration to replace the explicit endpoint or TLS policy.
	cc := cfg.ConnConfig
	if cc.Host != u.Hostname() || int(cc.Port) != port || cc.User != u.User.Username() || cc.Database != u.Path[1:] || len(cc.Fallbacks) != 0 {
		return nil, errors.New("PostgreSQL driver endpoint differs from explicit URL")
	}
	if u.Query().Get("sslmode") == "verify-full" {
		if cc.TLSConfig == nil || cc.TLSConfig.InsecureSkipVerify || cc.TLSConfig.ServerName != u.Hostname() {
			return nil, errors.New("PostgreSQL TLS verification required")
		}
		// Modify the parsed config in place to preserve pgx file-backed certificate reload.
		cc.TLSConfig.MinVersion = tls.VersionTLS12
	} else if cc.TLSConfig != nil {
		return nil, errors.New("unexpected PostgreSQL TLS settings")
	}
	cc.ConnectTimeout = c.ConnectTimeout
	cc.RuntimeParams = map[string]string{"application_name": info.Name, "timezone": "UTC"}
	cfg.MaxConns = c.MaxConns
	cfg.MinConns = c.MinConns
	cfg.MinIdleConns = 0
	cfg.MaxConnLifetime = c.MaxConnLifetime
	cfg.MaxConnIdleTime = c.MaxConnIdleTime
	cfg.HealthCheckPeriod = c.HealthCheckPeriod
	cfg.PingTimeout = c.HealthTimeout
	p := &Pool{cfg: cfg, connectTimeout: c.ConnectTimeout, log: log}
	if e := reg.Register(health.Check{Name: ParticipantName, Required: true, Timeout: c.HealthTimeout, Run: func(ctx context.Context) error {
		pool, e := p.Native()
		if e != nil {
			return e
		}
		if pool.Ping(ctx) != nil {
			return errors.New("PostgreSQL unavailable")
		}
		return nil
	}}); e != nil {
		return nil, e
	}
	return p, nil
}

// Native returns the live pool during startup, runtime and drain. Never close it.
func (p *Pool) Native() (*pgxpool.Pool, error) {
	if pool := p.pool.Load(); pool != nil {
		return pool, nil
	}
	return nil, errors.New("PostgreSQL pool unavailable")
}
func (p *Pool) participant() lifecycle.Participant {
	return lifecycle.Participant{Name: ParticipantName, Open: p.open, Close: p.close}
}
func (p *Pool) open(ctx context.Context) error {
	connectCtx, cancel := context.WithTimeout(ctx, p.connectTimeout)
	defer cancel()
	if e := connectCtx.Err(); e != nil {
		return e
	}
	pool, e := pgxpool.NewWithConfig(connectCtx, p.cfg)
	if e != nil {
		return errors.New("PostgreSQL pool initialization failed")
	}
	// The connection timeout may already have expired. Cleanup uses the
	// caller's remaining startup budget, not the expired connection context.
	cleanup := func(err error) error {
		cancel()
		cleanupCtx, done := context.WithTimeout(ctx, p.connectTimeout)
		defer done()
		return errors.Join(err, p.closeNative(cleanupCtx, pool))
	}
	if e := pool.Ping(connectCtx); e != nil {
		return cleanup(errors.New("PostgreSQL initial ping failed"))
	}
	if e := connectCtx.Err(); e != nil {
		return cleanup(e)
	}
	p.pool.Store(pool)
	return nil
}
func (p *Pool) close(ctx context.Context) error {
	pool := p.pool.Swap(nil)
	if pool == nil {
		return nil
	}
	return p.closeNative(ctx, pool)
}

func (p *Pool) closeNative(ctx context.Context, pool *pgxpool.Pool) error {
	// pgxpool.Close has no context and waits for checked-out connections.
	done := make(chan struct{})
	go func() { pool.Close(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		p.log.Error("PostgreSQL pool close timed out; outstanding resources may remain")
		return ctx.Err()
	}
}
