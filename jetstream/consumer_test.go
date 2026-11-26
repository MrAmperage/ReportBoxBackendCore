package jetstream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	nats "github.com/nats-io/nats.go"
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

// Embedding the native interface makes unneeded methods fail loudly if called.
type testDelivery struct {
	native.Msg
	ackErr, nakErr error
	acks, naks     int
}

func (m *testDelivery) Metadata() (*native.MsgMetadata, error) { return &native.MsgMetadata{}, nil }
func (m *testDelivery) Subject() string                        { return "test" }
func (m *testDelivery) Data() []byte                           { return []byte("payload") }
func (m *testDelivery) Headers() nats.Header                   { return nil }
func (m *testDelivery) DoubleAck(context.Context) error        { m.acks++; return m.ackErr }
func (m *testDelivery) NakWithDelay(time.Duration) error       { m.naks++; return m.nakErr }

func TestAcknowledgementOutcome(t *testing.T) {
	failure := errors.New("failure")
	for _, tc := range []struct {
		name                       string
		handlerErr, ackErr, nakErr error
		cancelWork                 bool
		healthy                    bool
		acks, naks                 int
	}{
		{name: "success", healthy: true, acks: 1},
		{name: "ack failed", ackErr: failure, acks: 1},
		{name: "handler failed", handlerErr: failure, healthy: true, naks: 1},
		{name: "nak failed", handlerErr: failure, nakErr: failure, naks: 1},
		{name: "forced shutdown", cancelWork: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			work, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := &consumers{work: work, p: consumerParams{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
			w := &worker{spec: Consumer{HandleTimeout: time.Second, AckTimeout: time.Second, RetryDelay: time.Second, Handle: func(context.Context, Delivery) error {
				if tc.cancelWork {
					cancel()
				}
				return tc.handlerErr
			}}}
			m := &testDelivery{ackErr: tc.ackErr, nakErr: tc.nakErr}
			if healthy := c.handle(w, m); healthy != tc.healthy || m.acks != tc.acks || m.naks != tc.naks {
				t.Fatalf("healthy=%v ack=%d nak=%d", healthy, m.acks, m.naks)
			}
		})
	}
}
