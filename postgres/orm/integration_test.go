package orm_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	backendcore "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/postgres"
	"github.com/MrAmperage/ReportBoxBackendCore/postgres/orm"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

type item struct {
	ID   int64 `gorm:"primaryKey;autoIncrement:false"`
	Name string
}

func TestORMRequiresPool(t *testing.T) {
	a := backendcore.New(app.Info{Name: "orm-invalid"}, orm.Module())
	if a.Err() == nil {
		t.Fatal("missing postgres module accepted")
	}
}

func TestORMSharedPoolAndTransactions(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	cfg := postgres.DefaultConfig()
	cfg.DSN = dsn
	cfg.AllowPlaintext = true
	cfg.MaxConns = 2
	var db *gorm.DB
	var owner *postgres.Pool
	a := backendcore.New(app.Info{Name: "postgres-orm-test"}, orm.Module(), postgres.Module(cfg), fx.Populate(&db, &owner))
	if e := a.Err(); e != nil {
		t.Fatal(e)
	}
	if _, e := db.DB(); !errors.Is(e, orm.ErrUnavailable) {
		t.Fatalf("unexpected early DB result: %v", e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if e := db.WithContext(ctx).Exec("SELECT 1").Error; !errors.Is(e, orm.ErrUnavailable) {
		t.Fatalf("unexpected early query: %v", e)
	}
	if e := a.Start(ctx); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		stop, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if e := a.Stop(stop); e != nil {
			t.Error(e)
		}
		if _, e := db.DB(); !errors.Is(e, orm.ErrUnavailable) {
			t.Errorf("ORM still available after stop: %v", e)
		}
	})
	pool, e := owner.Native()
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, e := db.DB()
	if e != nil {
		t.Fatal(e)
	}
	if sqlDB.Stats().MaxOpenConnections != 2 {
		t.Fatal("unexpected SQL adapter connection limit")
	}
	// The two native acquisitions exhaust the entire physical pool, including ORM.
	func() {
		first, e := pool.Acquire(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer first.Release()
		second, e := pool.Acquire(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer second.Release()
		short, c := context.WithTimeout(ctx, 150*time.Millisecond)
		defer c()
		if e := db.WithContext(short).Exec("SELECT 1").Error; e == nil || short.Err() == nil {
			t.Fatalf("ORM escaped shared pool limit: %v", e)
		}
	}()
	var suffix [8]byte
	if _, e = rand.Read(suffix[:]); e != nil {
		t.Fatal(e)
	}
	table := "backendcore_orm_test_" + hex.EncodeToString(suffix[:])
	if e = db.WithContext(ctx).Exec("CREATE TABLE " + table + " (id bigint PRIMARY KEY, name text NOT NULL)").Error; e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		clean, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if e := db.WithContext(clean).Exec("DROP TABLE " + table).Error; e != nil {
			t.Error(e)
		}
	})
	if e = gorm.G[item](db).Table(table).Create(ctx, &item{ID: 1, Name: "first"}); e != nil {
		t.Fatal(e)
	}
	got, e := gorm.G[item](db).Table(table).Where("id = ?", 1).First(ctx)
	if e != nil || got.Name != "first" {
		t.Fatalf("CRUD failed: %v", e)
	}
	var name string
	if e = pool.QueryRow(ctx, "SELECT name FROM "+table+" WHERE id = $1", 1).Scan(&name); e != nil || name != "first" {
		t.Fatalf("native query failed: %v", e)
	}
	rollback := errors.New("rollback test")
	if e = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := gorm.G[item](tx).Table(table).Create(ctx, &item{ID: 2, Name: "rolled back"}); e != nil {
			return e
		}
		return rollback
	}); !errors.Is(e, rollback) {
		t.Fatalf("transaction failed: %v", e)
	}
	if _, e = gorm.G[item](db).Table(table).Where("id = ?", 2).First(ctx); !errors.Is(e, gorm.ErrRecordNotFound) {
		t.Fatalf("rollback not applied: %v", e)
	}
	if e = gorm.G[item](db).Table(table).Create(ctx, &item{ID: 1, Name: "duplicate"}); !errors.Is(e, gorm.ErrDuplicatedKey) {
		t.Fatalf("duplicate mapping: %v", e)
	}
}
