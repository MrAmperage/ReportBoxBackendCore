// Package orm integrates GORM with the PostgreSQL pool owned by BackendCore.
package orm

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/MrAmperage/ReportBoxBackendCore/internal/observe"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
	"github.com/MrAmperage/ReportBoxBackendCore/postgres"
	"github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/fx"
	dialect "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

// ParticipantName is the lifecycle dependency for participants using GORM.
const ParticipantName = "postgres.orm"

type owner struct {
	pool     *postgres.Pool
	observer *observe.Instruments
	gate     *gate
	db       *gorm.DB
	log      *slog.Logger
}

var _ gorm.ConnPool = (*gate)(nil)
var _ gorm.TxBeginner = (*gate)(nil)
var _ gorm.GetDBConnector = (*gate)(nil)

type ownerParams struct {
	fx.In
	Pool     *postgres.Pool
	Log      *slog.Logger
	Observer *observe.Instruments `optional:"true"`
}

func newOwner(p ownerParams) (*owner, error) {
	pool, log := p.Pool, p.Log
	g := new(gate)
	db, e := gorm.Open(dialect.New(dialect.Config{Conn: g}), &gorm.Config{DisableAutomaticPing: true, TranslateError: true, NowFunc: func() time.Time { return time.Now().UTC() }, Logger: safeLogger{log: log, observer: p.Observer, level: gormlog.Warn, slow: 200 * time.Millisecond}})
	if e != nil {
		return nil, errors.New("GORM initialization failed")
	}
	return &owner{pool: pool, observer: p.Observer, gate: g, db: db, log: log}, nil
}
func (o *owner) participant() lifecycle.Participant {
	return lifecycle.Participant{Name: ParticipantName, DependsOn: o.observer.Dependencies(postgres.ParticipantName), Open: o.open, Close: o.close}
}
func (o *owner) open(ctx context.Context) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	pool, e := o.pool.Native()
	if e != nil {
		return e
	}
	db := stdlib.OpenDBFromPool(pool)
	// pgxpool owns all idle connections; database/sql must not retain any.
	db.SetMaxIdleConns(0)
	db.SetMaxOpenConns(int(pool.Config().MaxConns))
	if e := ctx.Err(); e != nil {
		_ = db.Close()
		return e
	}
	o.gate.db.Store(db)
	return nil
}
func (o *owner) close(ctx context.Context) error {
	db := o.gate.db.Swap(nil)
	if db == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- db.Close() }()
	select {
	case e := <-done:
		if e != nil {
			return errors.New("GORM SQL adapter close failed")
		}
		return nil
	case <-ctx.Done():
		o.log.Error("GORM adapter close timed out")
		return ctx.Err()
	}
}

// Module exposes *gorm.DB through Fx. postgres.Module is required separately.
// Use the DB in application methods after startup; do not close it or mutate
// its shared configuration. No AutoMigrate is executed by this module.
func Module() fx.Option {
	return fx.Module("backendcore.postgres.orm", fx.Provide(newOwner, func(o *owner) *gorm.DB { return o.db }, fx.Annotate(func(o *owner) lifecycle.Participant { return o.participant() }, fx.ResultTags(`group:"backendcore.lifecycle"`))))
}
