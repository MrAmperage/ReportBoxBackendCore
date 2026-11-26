package orm

import (
	"context"
	"errors"
	"testing"
)

func TestGateBeforeOpenAndAfterClose(t *testing.T) {
	g := new(gate)
	ctx := context.Background()
	if _, e := g.GetDBConn(); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e := g.ExecContext(ctx, "SELECT 1"); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e := g.QueryContext(ctx, "SELECT 1"); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e := g.PrepareContext(ctx, "SELECT 1"); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e := g.BeginTx(ctx, nil); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	var n int
	if e := g.QueryRowContext(ctx, "SELECT 1").Scan(&n); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
}
