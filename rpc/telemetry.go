package rpc

import (
	"context"
	"errors"
	"io"

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
	streams  *streamTracker
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
		state := i.streams.start(ctx, finish)
		// If a user interceptor panics during stream construction, do not leak the span.
		returned := false
		defer func() {
			if !returned {
				state.end("internal")
			}
		}()
		conn := next(ctx, spec)
		i.observer.Inject(ctx, conn.RequestHeader())
		returned = true
		return &observedStream{StreamingClientConn: conn, state: state}
	}
}

type observedStream struct {
	connect.StreamingClientConn
	state *streamObservation
}

func (s *observedStream) end(outcome string) { s.state.end(outcome) }

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
