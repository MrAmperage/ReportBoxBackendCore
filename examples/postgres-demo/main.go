// postgres-demo reads a row from a separately migrated PostgreSQL table.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	backendcore "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/postgres"
	"github.com/MrAmperage/ReportBoxBackendCore/postgres/orm"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

type Note struct {
	ID   int64 `gorm:"primaryKey"`
	Text string
}
type Repository struct{ db *gorm.DB }

func newRepository(db *gorm.DB) *Repository                   { return &Repository{db: db} }
func (r *Repository) First(ctx context.Context) (Note, error) { return gorm.G[Note](r.db).First(ctx) }
func run() (err error) {
	var r *Repository
	a := backendcore.New(app.Info{Name: "postgres-demo"}, postgres.Module(), orm.Module(), fx.Provide(newRepository), fx.Populate(&r))
	if err = a.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = a.Start(ctx); err != nil {
		return err
	}
	defer func() {
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if e := a.Stop(stop); err == nil {
			err = e
		}
	}()
	note, err := r.First(ctx)
	if err != nil {
		return err
	}
	fmt.Println("Note ID:", note.ID)
	return nil
}
func main() {
	if err := run(); err != nil {
		log.Fatal("PostgreSQL demo failed")
	}
}
