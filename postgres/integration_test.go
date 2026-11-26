package postgres_test

import (
	"context"
	"net"
	"os"
	"strings"
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

// A listener that never speaks PostgreSQL forces the initial handshake to time out.
// This test requires no external database.
func TestFailedStartupDoesNotPublishPool(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	cfg := postgres.DefaultConfig()
	cfg.DSN = "postgres://u:startup-test-secret@" + listener.Addr().String() + "/db?sslmode=disable"
	cfg.AllowPlaintext = true
	cfg.ConnectTimeout = 200 * time.Millisecond
	var owner *postgres.Pool
	a := backendcore.New(app.Info{Name: "postgres-failed-startup"}, postgres.Module(cfg), fx.Populate(&owner))
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = a.Start(ctx)
	if err == nil {
		t.Fatal("startup unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), "startup-test-secret") {
		t.Fatal("startup error leaked a credential")
	}
	if _, err := owner.Native(); err == nil {
		t.Fatal("failed startup published a pool")
	}
}
