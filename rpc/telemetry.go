package rpc

import (
	"context"
	"errors"
	"io"
	"sync"

	"connectrpc.com/connect"
	"github.com/MrAmperage/ReportBoxBackendCore/internal/observe"
	"go.opentelemetry.io/otel/trace"
)

func rpcOutcome(err error) string {
	if err == nil {
		return "ok"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	return connect.CodeOf(err).String()
}

type clientObserver struct {
	observer *observe.Instruments
	lifetime context.Context
}

func (i clientObserver) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (resp connect.AnyResponse, err error) {
		ctx, finish := i.observer.Start(ctx, "rpc.client", req.Spec().Procedure, trace.SpanKindClient)
		outcome := "internal"
		defer func() { finish(outcome) }()
		i.observer.Inject(ctx, req.Header())
		resp, err = next(ctx, req)
		outcome = rpcOutcome(err)
		return
	}
}
func (i clientObserver) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}
func (i clientObserver) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		ctx, finish := i.observer.Start(ctx, "rpc.client", spec.Procedure, trace.SpanKindClient)
		conn := next(ctx, spec)
		i.observer.Inject(ctx, conn.RequestHeader())
		// Both caller cancellation and shared transport shutdown terminate the span.
		s := &observedStream{StreamingClientConn: conn, finish: finish}
		// These callbacks do not need to touch the underlying stream; only end telemetry.
		s.install(ctx, i.lifetime)
		return s
	}
}

type observedStream struct {
	connect.StreamingClientConn
	finish func(string)
	mu     sync.Mutex
	done   bool
	stop   []func() bool
}

func (s *observedStream) install(contexts ...context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ctx := range contexts {
		if ctx != nil {
			s.stop = append(s.stop, context.AfterFunc(ctx, func() { s.end(rpcOutcome(ctx.Err())) }))
		}
	}
}
func (s *observedStream) end(outcome string) {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.done = true
	for _, stop := range s.stop {
		stop()
	}
	s.mu.Unlock()
	s.finish(outcome)
}
func (s *observedStream) Receive(v any) error {
	err := s.StreamingClientConn.Receive(v)
	if err != nil {
		if errors.Is(err, io.EOF) {
			s.end("ok")
		} else {
			s.end(rpcOutcome(err))
		}
	}
	return err
}
func (s *observedStream) CloseResponse() error {
	err := s.StreamingClientConn.CloseResponse()
	if err != nil {
		s.end(rpcOutcome(err))
	} else {
		s.end("canceled")
	}
	return err
}

// Send EOF means the server may have returned a status; wait for Receive.
func (s *observedStream) Send(v any) error {
	err := s.StreamingClientConn.Send(v)
	if err != nil && !errors.Is(err, io.EOF) {
		s.end(rpcOutcome(err))
	}
	return err
}
func (s *observedStream) CloseRequest() error {
	err := s.StreamingClientConn.CloseRequest()
	if err != nil && !errors.Is(err, io.EOF) {
		s.end(rpcOutcome(err))
	}
	return err
}
