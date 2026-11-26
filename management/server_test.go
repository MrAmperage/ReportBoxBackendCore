package management

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MrAmperage/ReportBoxBackendCore/health"
)

type fixture struct {
	s        *Server
	phase    atomic.Uint32
	reg      *health.Registry
	failures chan error
}

func newFixture(t *testing.T, cfg Config, m MetricsHandler) *fixture {
	t.Helper()
	f := &fixture{failures: make(chan error, 1)}
	f.reg = health.New(func() health.Phase { return health.Phase(f.phase.Load()) }, time.Second)
	var e error
	f.s, e = newServer(cfg, f.reg, slog.New(slog.NewTextHandler(io.Discard, nil)), func(e error) error { f.failures <- e; return nil }, m)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, c := context.WithTimeout(context.Background(), 2*time.Second)
		defer c()
		if e := f.s.close(ctx); e != nil {
			t.Error(e)
		}
	})
	return f
}
func (f *fixture) start(t *testing.T) {
	t.Helper()
	ctx, c := context.WithTimeout(context.Background(), 2*time.Second)
	defer c()
	f.phase.Store(uint32(health.Starting))
	if e := f.s.open(ctx); e != nil {
		t.Fatal(e)
	}
	if e := f.s.activate(ctx); e != nil {
		t.Fatal(e)
	}
}
func request(s *Server, method, path string, body io.Reader) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, body)
	r.Host = s.Address()
	if r.Host == "" {
		r.Host = s.cfg.Address
	}
	w := httptest.NewRecorder()
	s.serveHTTP(w, r)
	return w
}
func expect(t *testing.T, w *httptest.ResponseRecorder, code int) {
	t.Helper()
	if w.Code != code || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
	}
}
func TestEndpoints(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Address = "127.0.0.1:0"
	f := newFixture(t, cfg, MetricsHandler{})
	var down atomic.Bool
	var calls atomic.Int32
	if e := f.reg.Register(health.Check{Name: "database", Required: true, Run: func(context.Context) error {
		calls.Add(1)
		if down.Load() {
			return errors.New("secret database password")
		}
		return nil
	}}); e != nil {
		t.Fatal(e)
	}
	if e := f.reg.Register(health.Check{Name: "optional", Run: func(context.Context) error { return errors.New("private") }}); e != nil {
		t.Fatal(e)
	}
	expect(t, request(f.s, "GET", "/livez", nil), 503)
	f.start(t)
	expect(t, request(f.s, "GET", "/livez", nil), 200)
	expect(t, request(f.s, "GET", "/readyz", nil), 503)
	if calls.Load() != 0 {
		t.Fatal("probe during startup")
	}
	f.phase.Store(uint32(health.Running))
	expect(t, request(f.s, "GET", "/readyz", nil), 200)
	w := request(f.s, "GET", "/healthz", nil)
	expect(t, w, 503)
	if !strings.Contains(w.Body.String(), `"complete":true`) || !strings.Contains(w.Body.String(), `"name":"optional"`) {
		t.Fatal(w.Body.String())
	}
	down.Store(true)
	expect(t, request(f.s, "GET", "/readyz", nil), 503)
	n := calls.Load()
	expect(t, request(f.s, "GET", "/livez", nil), 200)
	if calls.Load() != n {
		t.Fatal("liveness probed DB")
	}
	w = request(f.s, "GET", "/healthz", nil)
	if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "private") {
		t.Fatal("raw error exposed")
	}
	down.Store(false)
	expect(t, request(f.s, "GET", "/readyz", nil), 200)
	for _, p := range []string{"/metrics", "/debug/pprof/", "/readyz/", "/readyz?x=1", "/%72eadyz", "/health/live"} {
		expect(t, request(f.s, "GET", p, nil), 404)
	}
	expect(t, request(f.s, "POST", "/readyz", nil), 405)
	expect(t, request(f.s, "GET", "/livez", strings.NewReader("x")), 400)
	w = request(f.s, "HEAD", "/livez", nil)
	expect(t, w, 200)
	if w.Body.Len() != 0 {
		t.Fatal("HEAD returned body")
	}
	f.phase.Store(uint32(health.Stopping))
	expect(t, request(f.s, "GET", "/readyz", nil), 503)
	expect(t, request(f.s, "GET", "/livez", nil), 200)
}
func TestRealListenerAndBindFailure(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Address = "127.0.0.1:0"
	f := newFixture(t, cfg, MetricsHandler{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "demo_total 1\n") })})
	if f.s.Address() != "" {
		t.Fatal("listener opened in constructor")
	}
	f.start(t)
	f.phase.Store(uint32(health.Running))
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	for _, path := range []string{"/livez", "/readyz", "/healthz", "/metrics"} {
		r, e := client.Get("http://" + f.s.Address() + path)
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(r.Body)
		r.Body.Close()
		if e != nil || r.StatusCode != 200 {
			t.Fatalf("%s: %d %s %v", path, r.StatusCode, b, e)
		}
	}
	cfg.Address = f.s.Address()
	other := newFixture(t, cfg, MetricsHandler{})
	if e := other.s.open(context.Background()); e == nil {
		t.Fatal("bind conflict accepted")
	}
	ctx, c := context.WithTimeout(context.Background(), time.Second)
	defer c()
	if e := f.s.drain(ctx); e != nil {
		t.Fatal(e)
	}
	if e := f.s.close(ctx); e != nil {
		t.Fatal(e)
	}
	if f.s.Address() != "" {
		t.Fatal("stale address")
	}
	select {
	case e := <-f.failures:
		t.Fatal(e)
	default:
	}
}
func TestOverloadAndDrain(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Address = "127.0.0.1:0"
	cfg.MaxConcurrent = 1
	entered, release := make(chan struct{}), make(chan struct{})
	f := newFixture(t, cfg, MetricsHandler{})
	if e := f.reg.Register(health.Check{Name: "slow", Required: true, Run: func(ctx context.Context) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}); e != nil {
		t.Fatal(e)
	}
	f.start(t)
	f.phase.Store(uint32(health.Running))
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- request(f.s, "GET", "/readyz", nil) }()
	<-entered
	expect(t, request(f.s, "GET", "/readyz", nil), 503)
	expect(t, request(f.s, "GET", "/livez", nil), 200)
	f.phase.Store(uint32(health.Stopping))
	_ = f.s.quiesce(context.Background())
	close(release)
	expect(t, <-result, 503)
	ctx, c := context.WithTimeout(context.Background(), time.Second)
	defer c()
	if e := f.s.drain(ctx); e != nil {
		t.Fatal(e)
	}
}
func TestUnexpectedServeFailure(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Address = "127.0.0.1:0"
	f := newFixture(t, cfg, MetricsHandler{})
	f.start(t)
	if e := f.s.listener.Close(); e != nil && !errors.Is(e, net.ErrClosed) {
		t.Fatal(e)
	}
	select {
	case <-f.failures:
	case <-time.After(time.Second):
		t.Fatal("failure not reported")
	}
}
func TestForcedDrainCancelsRequest(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Address = "127.0.0.1:0"
	entered, finished := make(chan struct{}), make(chan struct{})
	f := newFixture(t, cfg, MetricsHandler{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(finished) })})
	f.start(t)
	f.phase.Store(uint32(health.Running))
	client := &http.Client{Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	result := make(chan error, 1)
	go func() {
		r, e := client.Get("http://" + f.s.Address() + "/metrics")
		if r != nil {
			r.Body.Close()
		}
		result <- e
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request not admitted")
	}
	ctx, c := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer c()
	if e := f.s.drain(ctx); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("expected timeout, got %v", e)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("handler was not canceled")
	}
	<-result
}

func TestLoopbackHostPolicy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Address = "127.0.0.1:0"
	f := newFixture(t, cfg, MetricsHandler{})
	var calls atomic.Int32
	if err := f.reg.Register(health.Check{Name: "probe", Required: true, Run: func(context.Context) error { calls.Add(1); return nil }}); err != nil {
		t.Fatal(err)
	}
	f.start(t)
	f.phase.Store(uint32(health.Running))
	_, port, err := net.SplitHostPort(f.s.Address())
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	for _, tc := range []struct {
		host string
		code int
	}{
		{f.s.Address(), 200}, {"localhost:" + port, 200}, {"attacker.example:" + port, 421}, {"127.0.0.1:1", 421}, {"127.0.0.2:" + port, 421},
	} {
		req, err := http.NewRequest(http.MethodGet, "http://"+f.s.Address()+"/readyz", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = tc.host
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.code {
			t.Errorf("Host %q: got %d, want %d", tc.host, resp.StatusCode, tc.code)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("rejected requests reached probes: %d", calls.Load())
	}
}

func TestHostPolicyIPv6AndRemote(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Address = "[::1]:7613"
	s := newFixture(t, cfg, MetricsHandler{}).s
	for _, host := range []string{"[::1]:7613", "[0:0:0:0:0:0:0:1]:7613", "LOCALHOST:7613"} {
		if !s.validHost(host) {
			t.Errorf("rejected %q", host)
		}
	}
	for _, host := range []string{"attacker.example:7613", "[::1]:1", "", "[::2]:7613"} {
		if s.validHost(host) {
			t.Errorf("accepted %q", host)
		}
	}
	cfg.Address = "127.0.0.1:80"
	s = newFixture(t, cfg, MetricsHandler{}).s
	if !s.validHost("localhost") || s.validHost("attacker.example") {
		t.Fatal("wrong default port handling")
	}
	cfg.Address = "0.0.0.0:7613"
	cfg.AllowRemote = true
	s = newFixture(t, cfg, MetricsHandler{}).s
	if !s.validHost("auth-service:7613") {
		t.Fatal("private DNS name rejected")
	}
}

func TestProbePanicReturnsSanitized503(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Address = "127.0.0.1:0"
	f := newFixture(t, cfg, MetricsHandler{})
	if err := f.reg.Register(health.Check{Name: "database", Required: true, Run: func(context.Context) error { panic("private password") }}); err != nil {
		t.Fatal(err)
	}
	f.start(t)
	f.phase.Store(uint32(health.Running))
	expect(t, request(f.s, "GET", "/readyz", nil), 503)
	w := request(f.s, "GET", "/healthz", nil)
	expect(t, w, 503)
	if !strings.Contains(w.Body.String(), `"complete":true`) || !strings.Contains(w.Body.String(), `"healthy":false`) || strings.Contains(w.Body.String(), "private") {
		t.Fatal(w.Body.String())
	}
	expect(t, request(f.s, "GET", "/livez", nil), 200)
	expect(t, request(f.s, "GET", "http://"+f.s.Address()+"/livez", nil), 404)
}
