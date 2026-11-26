package management_test

import (
	"context"
	"io"
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

// Verify the future Telemetry provider is optional and has the intended DI type.
func TestModuleMetricsProvider(t *testing.T) {
	cfg := management.DefaultConfig()
	cfg.Address = "127.0.0.1:0"
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			var s *management.Server
			options := []fx.Option{management.Module(cfg), fx.Populate(&s)}
			if enabled {
				options = append(options, fx.Provide(func() management.MetricsHandler {
					return management.MetricsHandler{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "demo_total 1\n") })}
				}))
			}
			a := backendcore.New(app.Info{Name: "management-metrics-test"}, options...)
			if e := a.Err(); e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if e := a.Start(ctx); e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() {
				stop, done := context.WithTimeout(context.Background(), 5*time.Second)
				defer done()
				if e := a.Stop(stop); e != nil {
					t.Error(e)
				}
			})
			client := &http.Client{Timeout: time.Second}
			defer client.CloseIdleConnections()
			r, e := client.Get("http://" + s.Address() + "/metrics")
			if e != nil {
				t.Fatal(e)
			}
			defer r.Body.Close()
			if !enabled {
				if r.StatusCode != http.StatusNotFound {
					t.Fatal(r.StatusCode)
				}
				return
			}
			body, e := io.ReadAll(r.Body)
			if e != nil || r.StatusCode != http.StatusOK || string(body) != "demo_total 1\n" {
				t.Fatalf("metrics: %d %q %v", r.StatusCode, body, e)
			}
		})
	}
}
