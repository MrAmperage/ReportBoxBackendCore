package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	backendcore "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/postgres"
	"go.uber.org/fx"
)

func TestPoolLifecycle(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	cfg := postgres.DefaultConfig()
	cfg.DSN = dsn
	cfg.AllowPlaintext = true
	cfg.MaxConns = 2
	var owner *postgres.Pool
	var reg *health.Registry
	a := backendcore.New(app.Info{Name: "postgres-test"}, postgres.Module(cfg), fx.Populate(&owner, &reg))
	if e := a.Err(); e != nil {
		t.Fatal(e)
	}
	if _, e := owner.Native(); e == nil {
		t.Fatal("pool accessible before startup")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e := a.Start(ctx); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		stop, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if e := a.Stop(stop); e != nil {
			t.Error(e)
		}
		if _, e := owner.Native(); e == nil {
			t.Error("pool accessible after stop")
		}
	})
	if !reg.Ready(ctx) {
		t.Fatal("not ready")
	}
	pool, e := owner.Native()
	if e != nil {
		t.Fatal(e)
	}
	var result int
	if e = pool.QueryRow(ctx, "SELECT 1").Scan(&result); e != nil || result != 1 {
		t.Fatalf("unexpected query result: %v", e)
	}
}

func TestMissingDSNFailsDuringGraphConstruction(t *testing.T) {
	cfg := postgres.DefaultConfig()
	a := backendcore.New(app.Info{Name: "postgres-invalid"}, postgres.Module(cfg))
	if a.Err() == nil {
		t.Fatal("invalid config accepted")
	}
}
