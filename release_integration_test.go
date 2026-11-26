package backendcore_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	core "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/config"
	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/jetstream"
	"github.com/MrAmperage/ReportBoxBackendCore/management"
	broker "github.com/MrAmperage/ReportBoxBackendCore/nats"
	"github.com/MrAmperage/ReportBoxBackendCore/postgres"
	"github.com/MrAmperage/ReportBoxBackendCore/postgres/orm"
	"github.com/MrAmperage/ReportBoxBackendCore/rpc"
	"github.com/MrAmperage/ReportBoxBackendCore/telemetry"
	nats "github.com/nats-io/nats.go"
	js "github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"go.uber.org/fx"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm"
)

const releasePrefix = "/reportbox.release.v1.Smoke/"
const releaseProcedure = releasePrefix + "Run"

// Verify the actual graph with all modules, including optional Telemetry, before
// opening any resources. The integration test below exercises that same graph.
func TestReleaseGraph(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("telemetry=%t", enabled), func(t *testing.T) {
			nc := broker.DefaultConfig()
			pc := postgres.DefaultConfig()
			pc.DSN = "postgres://test:test@127.0.0.1:5432/test?sslmode=disable"
			sc, mc := rpc.DefaultServerConfig(), management.DefaultConfig()
			sc.Address, mc.Address = "127.0.0.1:0", "127.0.0.1:0"
			var pool *postgres.Pool
			var connection *broker.Connection
			var s *rpc.Server
			var m *management.Server
			opts := []fx.Option{orm.Module(), jetstream.Module(), rpc.ClientModule(rpc.DefaultClientConfig()), management.Module(mc), rpc.ServerModule(sc), postgres.Module(pc), broker.Module(nc), rpc.ProvideHandler(func() rpc.Handler {
				return rpc.Handler{AllowUnauthenticated: true, Build: func(opts ...connect.HandlerOption) (string, http.Handler) {
					return releasePrefix, connect.NewUnaryHandler(releaseProcedure, func(_ context.Context, r *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error) {
						return connect.NewResponse(r.Msg), nil
					}, opts...)
				}}
			}), fx.Populate(&pool, &connection, &s, &m)}
			if enabled {
				opts = append(opts, telemetry.Module(telemetry.DefaultConfig()))
			}
			a := core.NewWithConfig(app.Info{Name: "release-graph"}, config.Defaults(), opts...)
			if err := a.Err(); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Native(); err == nil {
				t.Fatal("PostgreSQL opened during DI")
			}
			if _, err := connection.Conn(); err == nil {
				t.Fatal("NATS opened during DI")
			}
			if s.Address() != "" || m.Address() != "" {
				t.Fatal("listener opened during DI")
			}
		})
	}
}

type releaseHandlerParams struct {
	fx.In
	DB        *gorm.DB
	NATS      *broker.Connection
	JS        *jetstream.Client
	Telemetry *telemetry.Runtime `optional:"true"`
}

// Uses dedicated disposable servers. No production tables or streams are changed.
func TestReleaseIntegration(t *testing.T) {
	url, dsn := os.Getenv("NATS_TEST_URL"), os.Getenv("TEST_POSTGRES_DSN")
	if url == "" || dsn == "" {
		t.Skip("set both NATS_TEST_URL and TEST_POSTGRES_DSN to isolated test servers")
	}
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("telemetry=%t", enabled), func(t *testing.T) { testReleaseIntegration(t, url, dsn, enabled) })
	}
}

func testReleaseIntegration(t *testing.T, url, dsn string, enabled bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	admin, err := nats.Connect(url, nats.Timeout(3*time.Second))
	if err != nil {
		t.Fatal("test NATS connection failed")
	}
	defer admin.Close()
	remote, err := js.New(admin)
	if err != nil {
		t.Fatal("test JetStream initialization failed")
	}
	name := fmt.Sprintf("RELEASE_%d", time.Now().UnixNano())
	eventSubject, jobSubject := "test.release."+name+".event", "test.release."+name+".job"
	if _, err = remote.CreateStream(ctx, js.StreamConfig{Name: name, Subjects: []string{jobSubject}, Storage: js.MemoryStorage, MaxAge: time.Minute, MaxBytes: 1 << 20}); err != nil {
		t.Fatal("test stream creation failed")
	}
	defer func() {
		clean, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		if err := remote.DeleteStream(clean, name); err != nil {
			t.Error("test stream cleanup failed")
		}
	}()
	consumer, err := remote.CreateConsumer(ctx, name, js.ConsumerConfig{Durable: "worker", AckPolicy: js.AckExplicitPolicy, AckWait: 10 * time.Second, MaxDeliver: 3, MaxAckPending: 4})
	if err != nil {
		t.Fatal("test consumer creation failed")
	}

	// Capture actual OTLP spans in a local collector. No external tracing service.
	var mu sync.Mutex
	exported := map[string]int{}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var req collector.ExportTraceServiceRequest
		if r.URL.Path != "/v1/traces" || e != nil || proto.Unmarshal(b, &req) != nil {
			http.Error(w, "invalid", 400)
			return
		}
		mu.Lock()
		for _, rs := range req.GetResourceSpans() {
			for _, ss := range rs.GetScopeSpans() {
				for _, span := range ss.GetSpans() {
					exported[span.GetName()]++
				}
			}
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer endpoint.Close()

	var server *rpc.Server
	var client *rpc.Client
	var manager *management.Server
	var pool *postgres.Pool
	var connection *broker.Connection
	var registry *health.Registry
	var db *gorm.DB
	nc := broker.DefaultConfig()
	nc.URL = url
	nc.AllowPlaintext = true
	pc := postgres.DefaultConfig()
	pc.DSN = dsn
	pc.AllowPlaintext = true
	sc, mc := rpc.DefaultServerConfig(), management.DefaultConfig()
	sc.Address, mc.Address = "127.0.0.1:0", "127.0.0.1:0"
	tc := telemetry.DefaultConfig()
	tc.Endpoint = endpoint.URL + "/v1/traces"
	tc.AllowPlaintext = true
	tc.SampleRatio = 1
	events, jobs := make(chan trace.TraceID, 4), make(chan trace.TraceID, 4)
	checkDB := func(ctx context.Context, db *gorm.DB) error {
		var value int
		if err := db.WithContext(ctx).Raw("SELECT 1").Scan(&value).Error; err != nil || value != 1 {
			return errors.New("test query failed")
		}
		return nil
	}
	opts := []fx.Option{orm.Module(), jetstream.Module(), rpc.ClientModule(rpc.DefaultClientConfig()), management.Module(mc), rpc.ServerModule(sc), postgres.Module(pc), broker.Module(nc), fx.Populate(&server, &client, &manager, &pool, &connection, &registry, &db),
		broker.ProvideSubscription(func(db *gorm.DB) broker.Subscription {
			return broker.Subscription{Subject: eventSubject, Timeout: 3 * time.Second, PendingMessages: 16, PendingBytes: 65536, Handle: func(ctx context.Context, m *nats.Msg) error {
				if string(m.Data) != "smoke" {
					return errors.New("invalid event")
				}
				if err := checkDB(ctx, db); err != nil {
					return err
				}
				select {
				case events <- trace.SpanContextFromContext(ctx).TraceID():
				case <-ctx.Done():
					return ctx.Err()
				}
				return nil
			}}
		}),
		jetstream.ProvideConsumer(func(db *gorm.DB) jetstream.Consumer {
			return jetstream.Consumer{Stream: name, Durable: "worker", Concurrency: 1, HandleTimeout: 3 * time.Second, AckTimeout: 2 * time.Second, RetryDelay: 100 * time.Millisecond, Handle: func(ctx context.Context, d jetstream.Delivery) error {
				if string(d.Data) != "smoke" {
					return errors.New("invalid job")
				}
				if err := checkDB(ctx, db); err != nil {
					return err
				}
				select {
				case jobs <- trace.SpanContextFromContext(ctx).TraceID():
				case <-ctx.Done():
					return ctx.Err()
				}
				return nil
			}}
		}),
		rpc.ProvideHandler(func(p releaseHandlerParams) rpc.Handler {
			return rpc.Handler{AllowUnauthenticated: true, Build: func(options ...connect.HandlerOption) (string, http.Handler) {
				return releasePrefix, connect.NewUnaryHandler(releaseProcedure, func(ctx context.Context, req *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error) {
					if err := checkDB(ctx, p.DB); err != nil {
						return nil, err
					}
					n, err := p.NATS.Conn()
					if err != nil {
						return nil, err
					}
					j, err := p.JS.JetStream()
					if err != nil {
						return nil, err
					}
					makeMsg := func(subject string) *nats.Msg {
						m := nats.NewMsg(subject)
						m.Data = []byte(req.Msg.Value)
						if p.Telemetry != nil {
							p.Telemetry.Propagator().Inject(ctx, propagation.HeaderCarrier(http.Header(m.Header)))
						}
						return m
					}
					if err = n.PublishMsg(makeMsg(eventSubject)); err != nil {
						return nil, err
					}
					if _, err = j.PublishMsg(ctx, makeMsg(jobSubject)); err != nil {
						return nil, err
					}
					return connect.NewResponse(wrapperspb.String(trace.SpanContextFromContext(ctx).TraceID().String())), nil
				}, options...)
			}}
		}),
	}
	if enabled {
		opts = append(opts, telemetry.Module(tc))
	}
	a := core.NewWithConfig(app.Info{Name: "release-smoke"}, config.Defaults(), opts...)
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		stopCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if e := a.Stop(stopCtx); e != nil {
			t.Error(e)
		}
	}
	defer stop()
	if !registry.Ready(ctx) {
		t.Fatal("combined graph not ready")
	}
	for _, protocol := range []rpc.Protocol{rpc.Connect, rpc.GRPC, rpc.GRPCWeb} {
		target, e := client.Target("http://"+server.Address(), protocol)
		if e != nil {
			t.Fatal(e)
		}
		call := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](target.HTTPClient, target.BaseURL+releaseProcedure, target.Options...)
		res, e := call.CallUnary(ctx, connect.NewRequest(wrapperspb.String("smoke")))
		if e != nil {
			t.Fatal(e)
		}
		for _, received := range []chan trace.TraceID{events, jobs} {
			select {
			case id := <-received:
				if enabled && (!id.IsValid() || id.String() != res.Msg.Value) {
					t.Fatal("trace context lost across messaging")
				}
				if !enabled && id.IsValid() {
					t.Fatal("unexpected trace without Telemetry")
				}
			case <-ctx.Done():
				t.Fatal("message not processed")
			}
		}
	}
	for {
		info, e := consumer.Info(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if info.NumAckPending == 0 && info.AckFloor.Stream >= 3 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("durable messages not acknowledged")
		case <-time.After(20 * time.Millisecond):
		}
	}
	hc := &http.Client{Timeout: 5 * time.Second}
	defer hc.CloseIdleConnections()
	for _, path := range []string{"/livez", "/readyz", "/healthz", "/metrics"} {
		res, e := hc.Get("http://" + manager.Address() + path)
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(res.Body)
		res.Body.Close()
		want := 200
		if path == "/metrics" && !enabled {
			want = 404
		}
		if e != nil || res.StatusCode != want {
			t.Fatalf("%s: status %d", path, res.StatusCode)
		}
		if path == "/metrics" && enabled {
			for _, metric := range []string{"reportbox_rpc_requests_total", "reportbox_nats_connected", "reportbox_postgres_connections_acquired", `operation="gorm.query"`, `operation="jetstream.ack"`} {
				if !strings.Contains(string(b), metric) {
					t.Errorf("missing metric %s", metric)
				}
			}
		}
	}
	stop()
	if registry.Ready(context.Background()) || registry.Live() || server.Address() != "" || manager.Address() != "" {
		t.Error("runtime still available after stop")
	}
	if _, e := pool.Native(); e == nil {
		t.Error("pool still available")
	}
	if _, e := connection.Conn(); e == nil {
		t.Error("NATS still available")
	}
	if _, e := db.DB(); !errors.Is(e, orm.ErrUnavailable) {
		t.Error("ORM still available")
	}
	if _, e := client.Target("http://127.0.0.1:1", rpc.Connect); e == nil {
		t.Error("RPC client still available")
	}
	mu.Lock()
	defer mu.Unlock()
	if enabled {
		for _, span := range []string{releaseProcedure, "nats.receive", "jetstream.process"} {
			if exported[span] == 0 {
				t.Errorf("missing exported span %s", span)
			}
		}
	} else if len(exported) != 0 {
		t.Error("unexpected export without Telemetry")
	}
}

// A late bind failure must roll back all earlier opened dependencies, including
// a telemetry exporter initialized without contacting its collector.
func TestReleaseRollback(t *testing.T) {
	url, dsn := os.Getenv("NATS_TEST_URL"), os.Getenv("TEST_POSTGRES_DSN")
	if url == "" || dsn == "" {
		t.Skip("set both NATS_TEST_URL and TEST_POSTGRES_DSN to isolated test servers")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	nc := broker.DefaultConfig()
	nc.URL = url
	nc.AllowPlaintext = true
	pc := postgres.DefaultConfig()
	pc.DSN = dsn
	pc.AllowPlaintext = true
	sc, mc := rpc.DefaultServerConfig(), management.DefaultConfig()
	sc.Address = listener.Addr().String()
	mc.Address = "127.0.0.1:0"
	tc := telemetry.DefaultConfig()
	tc.Endpoint = "http://127.0.0.1:1/v1/traces"
	tc.AllowPlaintext = true
	var pool *postgres.Pool
	var conn *broker.Connection
	var s *rpc.Server
	var m *management.Server
	var registry *health.Registry
	var runtime *telemetry.Runtime
	var db *gorm.DB
	var client *rpc.Client
	a := core.NewWithConfig(app.Info{Name: "release-rollback"}, config.Defaults(),
		rpc.ServerModule(sc), rpc.ClientModule(rpc.DefaultClientConfig()), management.Module(mc), broker.Module(nc), jetstream.Module(), postgres.Module(pc), orm.Module(), telemetry.Module(tc),
		rpc.ProvideHandler(func() rpc.Handler {
			return rpc.Handler{AllowUnauthenticated: true, Build: func(opts ...connect.HandlerOption) (string, http.Handler) {
				return releasePrefix, connect.NewUnaryHandler(releaseProcedure, func(_ context.Context, r *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error) {
					return connect.NewResponse(r.Msg), nil
				}, opts...)
			}}
		}), fx.Populate(&pool, &conn, &s, &m, &registry, &runtime, &db, &client))
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.Start(ctx); err == nil || !strings.Contains(err.Error(), "bind RPC listener failed") {
		if err == nil {
			stop, done := context.WithTimeout(context.Background(), 5*time.Second)
			defer done()
			_ = a.Stop(stop)
		}
		t.Fatalf("expected RPC bind failure, got %v", err)
	}
	if registry.Ready(ctx) || registry.Live() || s.Address() != "" || m.Address() != "" {
		t.Error("listener or health survived rollback")
	}
	if _, err := pool.Native(); err == nil {
		t.Error("pool survived rollback")
	}
	if _, err := conn.Conn(); err == nil {
		t.Error("NATS survived rollback")
	}
	if _, err := db.DB(); !errors.Is(err, orm.ErrUnavailable) {
		t.Error("ORM survived rollback")
	}
	if _, err := client.Target("http://127.0.0.1:1", rpc.Connect); err == nil {
		t.Error("RPC client survived rollback")
	}
	_, span := runtime.TracerProvider().Tracer("release-rollback").Start(context.Background(), "after rollback")
	defer span.End()
	if span.SpanContext().IsValid() {
		t.Error("trace provider survived rollback")
	}
}
