package nats_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	core "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	broker "github.com/MrAmperage/ReportBoxBackendCore/nats"
	native "github.com/nats-io/nats.go"
	"go.uber.org/fx"
)

func TestCoreSubscription(t *testing.T) {
	url := os.Getenv("NATS_TEST_URL")
	if url == "" {
		t.Skip("set NATS_TEST_URL to dedicated test broker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg := broker.DefaultConfig()
	cfg.URL = url
	cfg.AllowPlaintext = true
	subject := fmt.Sprintf("test.core.%d", time.Now().UnixNano())
	received := make(chan string, 1)
	var owner *broker.Connection
	a := core.New(app.Info{Name: "test-core-nats"}, broker.Module(cfg), fx.Populate(&owner), broker.ProvideSubscription(func() broker.Subscription {
		return broker.Subscription{Subject: subject, Timeout: time.Second, PendingMessages: 16, PendingBytes: 65536, Handle: func(_ context.Context, m *native.Msg) error {
			select {
			case received <- string(m.Data):
			default:
			}
			return nil
		}}
	}))
	if a.Err() != nil {
		t.Fatal(a.Err())
	}
	if _, err := owner.Conn(); err == nil {
		t.Fatal("connection opened early")
	}
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if err := a.Stop(stop); err != nil {
			t.Error(err)
		}
	}()
	nc, err := owner.Conn()
	if err != nil {
		t.Fatal(err)
	}
	if err = nc.Publish(subject, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err = nc.FlushWithContext(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-received:
		if got != "hello" {
			t.Fatal(got)
		}
	case <-ctx.Done():
		t.Fatal("no message")
	}
}
