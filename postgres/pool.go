package postgres

import (
	"context"
	"errors"
	"log/slog"
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
	u, _ := c.endpoint()
	cfg, e := pgxpool.ParseConfig(c.DSN)
	if e != nil {
		return nil, errors.New("invalid PostgreSQL driver configuration")
	}
	// Do not allow ambient driver configuration to replace the explicit endpoint or TLS policy.
	port := 5432
	if u.Port() != "" {
		port, _ = strconv.Atoi(u.Port())
	}
	cc := cfg.ConnConfig
	if cc.Host != u.Hostname() || int(cc.Port) != port || cc.User != u.User.Username() || cc.Database != u.Path[1:] || len(cc.Fallbacks) != 0 {
		return nil, errors.New("PostgreSQL driver endpoint differs from explicit URL")
	}
	if u.Query().Get("sslmode") == "verify-full" {
		if cc.TLSConfig == nil || cc.TLSConfig.InsecureSkipVerify || cc.TLSConfig.ServerName != u.Hostname() {
			return nil, errors.New("PostgreSQL TLS verification required")
		}
	} else if cc.TLSConfig != nil {
		return nil, errors.New("unexpected PostgreSQL TLS settings")
	}
	cc.ConnectTimeout = c.ConnectTimeout
	cc.RuntimeParams["application_name"] = info.Name
	cc.RuntimeParams["timezone"] = "UTC"
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
	ctx, cancel := context.WithTimeout(ctx, p.connectTimeout)
	defer cancel()
	if e := ctx.Err(); e != nil {
		return e
	}
	pool, e := pgxpool.NewWithConfig(ctx, p.cfg)
	if e != nil {
		return errors.New("PostgreSQL pool initialization failed")
	}
	if e := pool.Ping(ctx); e != nil {
		return errors.Join(errors.New("PostgreSQL initial ping failed"), p.closeNative(ctx, pool))
	}
	if e := ctx.Err(); e != nil {
		return errors.Join(e, p.closeNative(ctx, pool))
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
