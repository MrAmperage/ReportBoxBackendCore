package static_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	backendcore "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/management"
	"github.com/MrAmperage/ReportBoxBackendCore/static"
	"go.uber.org/fx"
)

func build(t *testing.T) static.Config {
	t.Helper()
	c := static.DefaultConfig()
	c.Root = t.TempDir()
	c.Address = "127.0.0.1:0"
	c.SPA = true
	if err := os.WriteFile(filepath.Join(c.Root, "index.html"), []byte("shell"), 0600); err != nil {
		t.Fatal(err)
	}
	return c
}
func TestLifecycle(t *testing.T) {
	c := build(t)
	var s *static.Server
	var m *management.Server
	mc := management.DefaultConfig()
	mc.Address = "127.0.0.1:0"
	a := backendcore.New(app.Info{Name: "static-test"}, static.Module(c), management.Module(mc), fx.Populate(&s, &m))
	if a.Err() != nil {
		t.Fatal(a.Err())
	}
	if s.Address() != "" {
		t.Fatal("bound before startup")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(ctx)
	address := s.Address()
	client := &http.Client{Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	for _, tc := range []struct {
		url  string
		code int
		body string
	}{{"http://" + address + "/", 200, "shell"}, {"http://" + address + "/missing.js", 404, ""}, {"http://" + m.Address() + "/readyz", 200, ""}, {"http://" + address + "/readyz", 404, ""}} {
		r, e := client.Get(tc.url)
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(r.Body)
		r.Body.Close()
		if e != nil || r.StatusCode != tc.code || (tc.body != "" && string(b) != tc.body) {
			t.Fatalf("%s %d %s %v", tc.url, r.StatusCode, b, e)
		}
	}
	req, err := http.NewRequest("GET", "http://"+address+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "untrusted.example:8080"
	r, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("unexpected Host accepted: %d", r.StatusCode)
	}
	if err := a.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if s.Address() != "" {
		t.Fatal("address remains")
	}
	l, e := net.Listen("tcp", address)
	if e != nil {
		t.Fatal(e)
	}
	l.Close()
}
func TestStartupFailure(t *testing.T) {
	for _, mode := range []string{"missing", "no-index", "symlink", "bind"} {
		t.Run(mode, func(t *testing.T) {
			c := build(t)
			switch mode {
			case "missing":
				c.Root = filepath.Join(c.Root, "missing")
			case "no-index":
				if e := os.Remove(filepath.Join(c.Root, "index.html")); e != nil {
					t.Fatal(e)
				}
			case "symlink":
				if e := os.Symlink(t.TempDir(), filepath.Join(c.Root, "external")); e != nil {
					t.Skip(e)
				}
			case "bind":
				l, e := net.Listen("tcp", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				defer l.Close()
				c.Address = l.Addr().String()
			}
			var s *static.Server
			a := backendcore.New(app.Info{Name: "static-test"}, static.Module(c), fx.Populate(&s))
			if a.Err() != nil {
				t.Fatal(a.Err())
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if e := a.Start(ctx); e == nil {
				a.Stop(ctx)
				t.Fatal("startup succeeded")
			}
			if s.Address() != "" {
				t.Fatal("published partial startup")
			}
		})
	}
}

// Even an out-of-contract late symlink cannot escape the confined root.
func TestStandaloneRootConfinement(t *testing.T) {
	c := build(t)
	c.SPA = false
	if err := os.Remove(filepath.Join(c.Root, "index.html")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Root, "remote.js"), []byte("public"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "private.js")
	if err := os.WriteFile(outside, []byte("private-content"), 0600); err != nil {
		t.Fatal(err)
	}
	var s *static.Server
	a := backendcore.New(app.Info{Name: "static-root-test"}, static.Module(c), fx.Populate(&s))
	if a.Err() != nil {
		t.Fatal(a.Err())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.Stop(stop); err != nil {
			t.Error(err)
		}
	}()
	client := &http.Client{Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	get := func(path string) (int, string) {
		t.Helper()
		r, e := client.Get("http://" + s.Address() + path)
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		b, e := io.ReadAll(r.Body)
		if e != nil {
			t.Fatal(e)
		}
		return r.StatusCode, string(b)
	}
	if code, body := get("/remote.js"); code != 200 || body != "public" {
		t.Fatal(code, body)
	}
	if code, _ := get("/"); code != 404 {
		t.Fatal(code)
	}
	if e := os.Symlink(outside, filepath.Join(c.Root, "escape.js")); e != nil {
		t.Skip(e)
	}
	if code, body := get("/escape.js"); code == 200 || body == "private-content" {
		t.Fatal("escaped root", code)
	}
}
