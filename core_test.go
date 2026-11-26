package backendcore

import (
	"context"
	"testing"
	"time"

	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/config"
	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
	"go.uber.org/fx"
)

func TestCore(t *testing.T) {
	var reg *health.Registry
	opened := false
	a := NewWithConfig(app.Info{Name: "test"}, config.Defaults(), fx.Populate(&reg), fx.Provide(fx.Annotate(func() lifecycle.Participant {
		return lifecycle.Participant{Name: "test", Open: func(context.Context) error { opened = true; return nil }, Close: func(context.Context) error { opened = false; return nil }}
	}, fx.ResultTags(`group:"backendcore.lifecycle"`))))
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
	if opened || reg.Ready(context.Background()) {
		t.Fatal("premature start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !opened || !reg.Ready(ctx) {
		t.Fatal("not ready")
	}
	if err := a.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if opened || reg.Ready(ctx) {
		t.Fatal("not stopped")
	}
}
func TestInvalidConfig(t *testing.T) {
	called := false
	a := NewWithConfig(app.Info{Name: "test"}, config.Config{}, fx.Invoke(func() { called = true }))
	if a.Err() == nil || called {
		t.Fatal("invalid configuration constructed application")
	}
}
