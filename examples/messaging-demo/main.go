// messaging-demo publishes and receives an example Core NATS or JetStream message.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"time"

	core "github.com/MrAmperage/ReportBoxBackendCore"
	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/jetstream"
	broker "github.com/MrAmperage/ReportBoxBackendCore/nats"
	native "github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/fx"
)

const coreSubject = "reportbox.demo.notice.v1"
const durableSubject = "reportbox.demo.event.v1"

func main() {
	mode := flag.String("mode", "core-worker", "core-worker, js-worker, core-publish, js-publish")
	message := flag.String("message", "hello", "demo payload (not sensitive)")
	id := flag.String("id", "", "stable message ID for js-publish")
	flag.Parse()
	switch *mode {
	case "core-worker":
		core.Run(app.Info{Name: "core-demo-worker"}, broker.Module(), broker.ProvideSubscription(coreHandler))
	case "js-worker":
		core.Run(app.Info{Name: "js-demo-worker"}, broker.Module(), jetstream.Module(), jetstream.ProvideConsumer(jsHandler))
	case "core-publish", "js-publish":
		if err := publish(*mode, *message, *id); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatal("unknown mode")
	}
}

func coreHandler(logger *slog.Logger) broker.Subscription {
	return broker.Subscription{Subject: coreSubject, Timeout: time.Second, PendingMessages: 32, PendingBytes: 64 * 1024, Handle: func(_ context.Context, m *native.Msg) error {
		logger.Info("demo notice received", "bytes", len(m.Data))
		return nil
	}}
}
func jsHandler(logger *slog.Logger) jetstream.Consumer {
	return jetstream.Consumer{Stream: "REPORTBOX_DEMO", Durable: "demo-worker", Concurrency: 1, HandleTimeout: 2 * time.Second, AckTimeout: 2 * time.Second, RetryDelay: time.Second, Handle: func(_ context.Context, d jetstream.Delivery) error {
		logger.Info("demo event received", "bytes", len(d.Data))
		return nil
	}}
}
func publish(mode, message, id string) (err error) {
	var conn *broker.Connection
	var client *jetstream.Client
	opts := []fx.Option{broker.Module(), fx.Populate(&conn)}
	if mode == "js-publish" {
		opts = append(opts, jetstream.Module(), fx.Populate(&client))
	}
	a := core.New(app.Info{Name: "demo-publisher"}, opts...)
	if err := a.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		return err
	}
	defer func() {
		stop, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if e := a.Stop(stop); err == nil {
			err = e
		}
	}()
	if mode == "core-publish" {
		nc, e := conn.Conn()
		if e != nil {
			return e
		}
		if e = nc.Publish(coreSubject, []byte(message)); e != nil {
			return e
		}
		return nc.FlushWithContext(ctx)
	}
	js, e := client.JetStream()
	if e != nil {
		return e
	}
	var options []jsapi.PublishOpt
	if id != "" {
		options = append(options, jsapi.WithMsgID(id))
	}
	_, e = js.Publish(ctx, durableSubject, []byte(message), options...)
	return e
}
