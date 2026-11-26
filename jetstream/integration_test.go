package jetstream_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	core "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/jetstream"
	broker "github.com/MrAmperage/ReportBoxBackendCore/nats"
	nats "github.com/nats-io/nats.go"
	native "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/fx"
)

func TestDelivery(t *testing.T) {
	url := os.Getenv("NATS_TEST_URL")
	if url == "" {
		t.Skip("set NATS_TEST_URL to dedicated test broker with JetStream")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, err := nats.Connect(url, nats.Timeout(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	js, err := native.New(admin)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("TEST_%d", time.Now().UnixNano())
	subject := "test." + name
	_, err = js.CreateStream(ctx, native.StreamConfig{Name: name, Subjects: []string{subject}, Storage: native.MemoryStorage, MaxAge: time.Minute, MaxBytes: 1024 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		if err := js.DeleteStream(cleanup, name); err != nil {
			t.Error(err)
		}
	}()
	remote, err := js.CreateConsumer(ctx, name, native.ConsumerConfig{Durable: "worker", AckPolicy: native.AckExplicitPolicy, AckWait: 8 * time.Second, MaxDeliver: 3, MaxAckPending: 4})
	if err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int32
	received := make(chan struct{}, 1)
	drainStarted := make(chan struct{})
	releaseDrain := make(chan struct{})
	defer closeIfOpen(releaseDrain)
	cfg := broker.DefaultConfig()
	cfg.URL = url
	cfg.AllowPlaintext = true
	var client *jetstream.Client
	var registry *health.Registry
	a := core.New(app.Info{Name: "test-messaging"}, broker.Module(cfg), jetstream.Module(), fx.Populate(&client, &registry), jetstream.ProvideConsumer(func() jetstream.Consumer {
		return jetstream.Consumer{Stream: name, Durable: "worker", Concurrency: 1, HandleTimeout: 2 * time.Second, AckTimeout: 2 * time.Second, RetryDelay: 100 * time.Millisecond, Handle: func(ctx context.Context, d jetstream.Delivery) error {
			if string(d.Data) == "drain" {
				close(drainStarted)
				select {
				case <-releaseDrain:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			if string(d.Data) != "payload" {
				return errors.New("unexpected payload")
			}
			if attempts.Add(1) == 1 {
				return errors.New("retry")
			}
			select {
			case received <- struct{}{}:
			default:
			}
			return nil
		}}
	}))
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	var stopStarted bool
	stopDone := make(chan struct{})
	var stopErr error
	stopApp := func() {
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		stopErr = a.Stop(stop)
		close(stopDone)
	}
	defer func() {
		closeIfOpen(releaseDrain)
		if !stopStarted {
			stopApp()
		}
		<-stopDone
		if stopErr != nil {
			t.Error(stopErr)
		}
	}()
	if !registry.Ready(ctx) {
		t.Fatal("not ready")
	}
	own, err := client.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = own.Publish(ctx, subject, []byte("payload"), native.WithMsgID(name)); err != nil {
		t.Fatal(err)
	}
	dup, err := own.Publish(ctx, subject, []byte("payload"), native.WithMsgID(name))
	if err != nil || !dup.Duplicate {
		t.Fatalf("expected duplicate publish ack: %v", err)
	}
	select {
	case <-received:
	case <-ctx.Done():
		t.Fatal("no redelivery")
	}
	for {
		info, err := remote.Info(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if info.NumAckPending == 0 && info.AckFloor.Stream >= 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("no confirmed ack")
		case <-time.After(50 * time.Millisecond):
		}
	}
	if attempts.Load() != 2 {
		t.Fatalf("expected exactly two attempts, got %d", attempts.Load())
	}
	// Start shutdown while the next message is being handled. Its ack must
	// reach the broker before the shared connection closes.
	if _, err := own.Publish(ctx, subject, []byte("drain")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-drainStarted:
	case <-ctx.Done():
		t.Fatal("drain handler did not start")
	}
	stopStarted = true
	go stopApp()
	for registry.Running() {
		select {
		case <-ctx.Done():
			t.Fatal("shutdown did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	closeIfOpen(releaseDrain)
	select {
	case <-stopDone:
		if stopErr != nil {
			t.Fatal(stopErr)
		}
	case <-ctx.Done():
		t.Fatal("shutdown timed out")
	}
	info, err := remote.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.NumAckPending != 0 || info.AckFloor.Stream != 2 {
		t.Fatal("message not acknowledged before shutdown")
	}
}

func closeIfOpen(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

// Dependency validation must work without any network resource.
func TestModuleRequiresNATS(t *testing.T) {
	a := core.New(app.Info{Name: "test-missing-nats"}, jetstream.Module())
	if a.Err() == nil {
		t.Fatal("expected missing NATS dependency")
	}
}
