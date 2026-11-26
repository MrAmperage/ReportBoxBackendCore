// Package jetstream integrates durable messaging using the shared NATS connection.
package jetstream

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/internal/observe"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
	broker "github.com/MrAmperage/ReportBoxBackendCore/nats"
	native "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/fx"
)

// Client provides the native JetStream context after Open.
type Client struct {
	owner *broker.Connection
	mu    sync.RWMutex
	js    native.JetStream
}

func newClient(owner *broker.Connection, registry *health.Registry) (*Client, error) {
	c := &Client{owner: owner}
	err := registry.Register(health.Check{Name: "jetstream.account", Required: true, Run: func(ctx context.Context) error {
		js, err := c.JetStream()
		if err != nil {
			return err
		}
		if _, err = js.AccountInfo(ctx); err != nil {
			return errors.New("JetStream unavailable")
		}
		return nil
	}})
	return c, err
}

// JetStream returns the native client; do not close its shared connection.
func (c *Client) JetStream() (native.JetStream, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.js == nil {
		return nil, errors.New("JetStream unavailable")
	}
	return c.js, nil
}
func (c *Client) open(ctx context.Context) error {
	nc, err := c.owner.Conn()
	if err != nil {
		return err
	}
	js, err := native.New(nc, native.WithDefaultTimeout(5*time.Second))
	if err != nil {
		return errors.New("JetStream init failed")
	}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err = js.AccountInfo(probe); err != nil {
		return errors.New("JetStream account unavailable")
	}
	c.mu.Lock()
	c.js = js
	c.mu.Unlock()
	return nil
}
func (c *Client) close(context.Context) error { c.mu.Lock(); c.js = nil; c.mu.Unlock(); return nil }
func (c *Client) participant() lifecycle.Participant {
	return lifecycle.Participant{Name: "jetstream.client", DependsOn: []string{"nats.connection"}, Open: c.open, Close: c.close}
}

// Module installs JetStream. nats.Module must be installed separately.
func Module() fx.Option {
	return fx.Module("backendcore.jetstream", fx.Provide(newClient, newConsumers, fx.Annotate(func(c *Client) lifecycle.Participant { return c.participant() }, fx.ResultTags(`group:"backendcore.lifecycle"`)), fx.Annotate(func(c *consumers) lifecycle.Participant { return c.participant() }, fx.ResultTags(`group:"backendcore.lifecycle"`))))
}

// ProvideConsumer registers a constructor returning one Consumer.
func ProvideConsumer(constructor any) fx.Option {
	return fx.Provide(fx.Annotate(constructor, fx.ResultTags(`group:"backendcore.jetstream.consumers"`)))
}

type consumerParams struct {
	fx.In
	Client   *Client
	Health   *health.Registry
	Logger   *slog.Logger
	Observer *observe.Instruments `optional:"true"`
	Specs    []Consumer           `group:"backendcore.jetstream.consumers"`
}
