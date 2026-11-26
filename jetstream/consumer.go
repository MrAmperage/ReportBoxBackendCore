package jetstream

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
	nats "github.com/nats-io/nats.go"
	native "github.com/nats-io/nats.go/jetstream"
)

// Delivery exposes data without acknowledgement operations. The worker owns ack.
type Delivery struct {
	Subject  string
	Data     []byte
	Headers  nats.Header
	Metadata *native.MsgMetadata
}

// Consumer binds an existing durable pull consumer. Handle returns nil only
// after the durable business operation has succeeded; errors trigger redelivery.
type Consumer struct {
	Stream, Durable                       string
	Concurrency                           int
	HandleTimeout, AckTimeout, RetryDelay time.Duration
	Handle                                func(context.Context, Delivery) error
}

const pullTimeout = 3 * time.Second

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func (c Consumer) validate() error {
	if !namePattern.MatchString(c.Stream) || !namePattern.MatchString(c.Durable) || c.Concurrency < 1 || c.Concurrency > 128 || c.HandleTimeout <= 0 || c.AckTimeout <= 0 || c.RetryDelay <= 0 || c.Handle == nil {
		return errors.New("invalid JetStream consumer configuration")
	}
	return nil
}
func (c Consumer) validateRemote(r native.ConsumerConfig) error {
	wait := r.AckWait
	if len(r.BackOff) > 0 {
		wait = r.BackOff[0]
		for _, delay := range r.BackOff {
			wait = min(wait, delay)
		}
	}
	if r.Durable != c.Durable || r.DeliverSubject != "" || r.AckPolicy != native.AckExplicitPolicy || r.MaxDeliver <= 0 || r.MaxAckPending < c.Concurrency || (r.MaxWaiting > 0 && r.MaxWaiting < c.Concurrency) || (r.MaxRequestExpires > 0 && r.MaxRequestExpires < pullTimeout) || wait <= c.HandleTimeout || wait-c.HandleTimeout <= c.AckTimeout {
		return errors.New("incompatible durable consumer: require pull, explicit ack, finite MaxDeliver, adequate ack and pull limits")
	}
	return nil
}

type worker struct {
	spec    Consumer
	remote  native.Consumer
	healthy atomic.Bool
}
type consumers struct {
	p                  consumerParams
	workers            []*worker
	pull, work         context.Context
	stopPull, stopWork context.CancelFunc
	wg                 sync.WaitGroup
	done               chan struct{}
	mu                 sync.Mutex
	accepting          bool
}

func newConsumers(p consumerParams) (*consumers, error) {
	c := &consumers{p: p, done: make(chan struct{})}
	seen := map[string]bool{}
	for _, spec := range p.Specs {
		if err := spec.validate(); err != nil {
			return nil, err
		}
		key := spec.Stream + "/" + spec.Durable
		if seen[key] {
			return nil, errors.New("duplicate JetStream consumer")
		}
		seen[key] = true
		for i := 0; i < spec.Concurrency; i++ {
			c.workers = append(c.workers, &worker{spec: spec})
		}
	}
	if err := p.Health.Register(health.Check{Name: "jetstream.consumers", Required: true, Run: func(context.Context) error {
		for _, w := range c.workers {
			if !w.healthy.Load() {
				return errors.New("JetStream consumer unavailable")
			}
		}
		return nil
	}}); err != nil {
		return nil, err
	}
	return c, nil
}
func (c *consumers) participant() lifecycle.Participant {
	return lifecycle.Participant{Name: "jetstream.consumers", DependsOn: []string{"jetstream.client"}, Activate: c.activate, Quiesce: c.quiesce, Drain: c.drain, Close: c.close}
}
func (c *consumers) activate(ctx context.Context) error {
	js, err := c.p.Client.JetStream()
	if err != nil {
		return err
	}
	for _, w := range c.workers {
		probe, cancel := context.WithTimeout(ctx, 5*time.Second)
		remote, e := js.Consumer(probe, w.spec.Stream, w.spec.Durable)
		if e == nil {
			var info *native.ConsumerInfo
			info, e = remote.Info(probe)
			if e == nil {
				e = w.spec.validateRemote(info.Config)
			}
		}
		cancel()
		if e != nil {
			return errors.New("JetStream consumer lookup or validation failed")
		}
		w.remote = remote
		w.healthy.Store(true)
	}
	c.pull, c.stopPull = context.WithCancel(context.Background())
	c.work, c.stopWork = context.WithCancel(context.Background())
	c.mu.Lock()
	c.accepting = true
	c.mu.Unlock()
	for _, w := range c.workers {
		c.wg.Add(1)
		go c.run(w)
	}
	go func() { c.wg.Wait(); close(c.done) }()
	return nil
}
func (c *consumers) run(w *worker) {
	defer c.wg.Done()
	defer w.healthy.Store(false)
	for c.pull.Err() == nil {
		if !c.p.Health.Running() {
			if !pause(c.pull, 20*time.Millisecond) {
				return
			}
			continue
		}
		if !w.healthy.Load() {
			ctx, cancel := context.WithTimeout(c.pull, 5*time.Second)
			info, e := w.remote.Info(ctx)
			cancel()
			if e != nil || w.spec.validateRemote(info.Config) != nil {
				if !pause(c.pull, time.Second) {
					return
				}
				continue
			}
			w.healthy.Store(true)
		}
		pull, cancel := context.WithTimeout(c.pull, pullTimeout)
		batch, err := w.remote.Fetch(1, native.FetchContext(pull))
		if err == nil {
			for m := range batch.Messages() {
				c.mu.Lock()
				admit := c.accepting && c.p.Health.Running()
				c.mu.Unlock()
				if admit {
					c.handle(w, m)
				}
			}
			err = batch.Error()
		}
		cancel()
		if c.pull.Err() != nil {
			return
		}
		if err != nil {
			w.healthy.Store(false)
			c.p.Logger.Warn("JetStream pull failed")
			if !pause(c.pull, time.Second) {
				return
			}
		}
	}
}
func pause(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func (c *consumers) handle(w *worker, m native.Msg) {
	ctx, cancel := context.WithTimeout(c.work, w.spec.HandleTimeout)
	metadata, err := m.Metadata()
	if err == nil {
		err = invoke(ctx, w.spec.Handle, Delivery{Subject: m.Subject(), Data: m.Data(), Headers: m.Headers(), Metadata: metadata})
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	cancel()
	if c.work.Err() != nil {
		return
	} // uncertain outcome: leave unacked
	if err != nil {
		c.p.Logger.Warn("JetStream handler failed")
		if e := m.NakWithDelay(w.spec.RetryDelay); e != nil {
			c.p.Logger.Warn("JetStream negative acknowledgement failed")
		}
		return
	}
	ack, done := context.WithTimeout(c.work, w.spec.AckTimeout)
	defer done()
	if m.DoubleAck(ack) != nil {
		c.p.Logger.Warn("JetStream acknowledgement uncertain")
	}
}
func invoke(ctx context.Context, fn func(context.Context, Delivery) error, d Delivery) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("JetStream handler panic")
		}
	}()
	return fn(ctx, d)
}
func (c *consumers) quiesce(context.Context) error {
	c.mu.Lock()
	c.accepting = false
	c.mu.Unlock()
	if c.stopPull != nil {
		c.stopPull()
	}
	return nil
}
func (c *consumers) drain(ctx context.Context) error {
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		if c.stopWork != nil {
			c.stopWork()
		}
		return ctx.Err()
	}
}
func (c *consumers) close(ctx context.Context) error {
	_ = c.quiesce(ctx)
	if c.stopWork != nil {
		c.stopWork()
	}
	return nil
}
