package orm

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync/atomic"
)

// ErrUnavailable is returned when a repository is used outside the database lifecycle.
var ErrUnavailable = errors.New("PostgreSQL ORM unavailable")

// gate is a stable GORM connection-pool reference. It does not own a connection
// or start an opener goroutine until its lifecycle participant opens the adapter.
type gate struct{ db atomic.Pointer[sql.DB] }

func (g *gate) GetDBConn() (*sql.DB, error) {
	if db := g.db.Load(); db != nil {
		return db, nil
	}
	return nil, ErrUnavailable
}
func (g *gate) PrepareContext(ctx context.Context, q string) (*sql.Stmt, error) {
	db, e := g.GetDBConn()
	if e != nil {
		return nil, e
	}
	return db.PrepareContext(ctx, q)
}
func (g *gate) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	db, e := g.GetDBConn()
	if e != nil {
		return nil, e
	}
	return db.ExecContext(ctx, q, args...)
}
func (g *gate) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	db, e := g.GetDBConn()
	if e != nil {
		return nil, e
	}
	return db.QueryContext(ctx, q, args...)
}
func (g *gate) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	if db, e := g.GetDBConn(); e == nil {
		return db.QueryRowContext(ctx, q, args...)
	}
	// sql.Row has no public error constructor. Use a rejecting connector only on
	// invalid early/late use; this temporary handle cannot open network resources.
	db := sql.OpenDB(rejectedConnector{})
	defer db.Close()
	return db.QueryRowContext(ctx, q, args...)
}
func (g *gate) BeginTx(ctx context.Context, o *sql.TxOptions) (*sql.Tx, error) {
	db, e := g.GetDBConn()
	if e != nil {
		return nil, e
	}
	return db.BeginTx(ctx, o)
}

type rejectedConnector struct{}

func (rejectedConnector) Connect(context.Context) (driver.Conn, error) { return nil, ErrUnavailable }
func (rejectedConnector) Driver() driver.Driver                        { return rejectedDriver{} }

type rejectedDriver struct{}

func (rejectedDriver) Open(string) (driver.Conn, error) { return nil, ErrUnavailable }
