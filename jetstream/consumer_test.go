package jetstream

import (
	"context"
	"testing"
	"time"

	native "github.com/nats-io/nats.go/jetstream"
)

func TestRemotePolicy(t *testing.T) {
	s := Consumer{Stream: "EVENTS", Durable: "worker", Concurrency: 2, HandleTimeout: time.Second, AckTimeout: time.Second, RetryDelay: time.Second, Handle: func(context.Context, Delivery) error { return nil }}
	if err := s.validate(); err != nil {
		t.Fatal(err)
	}
	good := native.ConsumerConfig{Durable: "worker", AckPolicy: native.AckExplicitPolicy, AckWait: 10 * time.Second, MaxDeliver: 4, MaxAckPending: 8}
	if err := s.validateRemote(good); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*native.ConsumerConfig){func(c *native.ConsumerConfig) { c.AckPolicy = native.AckNonePolicy }, func(c *native.ConsumerConfig) { c.MaxDeliver = -1 }, func(c *native.ConsumerConfig) { c.MaxAckPending = 1 }, func(c *native.ConsumerConfig) { c.AckWait = time.Second }, func(c *native.ConsumerConfig) { c.DeliverSubject = "push" }, func(c *native.ConsumerConfig) { c.BackOff = []time.Duration{10 * time.Second, time.Second} }, func(c *native.ConsumerConfig) { c.MaxWaiting = 1 }, func(c *native.ConsumerConfig) { c.MaxRequestExpires = time.Second }} {
		c := good
		change(&c)
		if s.validateRemote(c) == nil {
			t.Fatal("invalid consumer accepted")
		}
	}
}
func TestRecoverHandler(t *testing.T) {
	if invoke(context.Background(), func(context.Context, Delivery) error { panic("secret") }, Delivery{}) == nil {
		t.Fatal("expected panic recovery")
	}
}
