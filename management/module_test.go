package management_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	backendcore "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/management"
	"go.uber.org/fx"
)

func TestModule(t *testing.T) {
	cfg := management.DefaultConfig()
	cfg.Address = "127.0.0.1:0"
	var s *management.Server
	a := backendcore.New(app.Info{Name: "management-test"}, management.Module(cfg), fx.Populate(&s))
	if e := a.Err(); e != nil {
		t.Fatal(e)
	}
	if s.Address() != "" {
		t.Fatal("unexpected listener")
	}
	ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	if e := a.Start(ctx); e != nil {
		t.Fatal(e)
	}
	defer func() {
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if e := a.Stop(stop); e != nil {
			t.Error(e)
		}
	}()
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	r, e := client.Get("http://" + s.Address() + "/readyz")
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
}
