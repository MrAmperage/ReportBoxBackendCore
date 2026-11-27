package rpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/MrAmperage/ReportBoxBackendCore/internal/observe"
	"go.opentelemetry.io/otel/trace"
)

// HandlerGroup is the Fx value group for generated service handler factories.
const HandlerGroup = "backendcore.rpc.handlers"

// AuthorizeFunc validates credentials and permissions for a specific procedure.
// Return a context carrying verified identity, or a Connect error. The context
// must derive from ctx so deadlines and cancellation are preserved.
type AuthorizeFunc func(ctx context.Context, procedure string, headers http.Header) (context.Context, error)

// Handler registers a generated Protobuf service. Exactly one of Authorize and
// AllowUnauthenticated must be selected. Public routing is configured outside the RPC module;
// AllowUnauthenticated is not an instruction to publish the service externally.
type Handler struct {
	Build                func(...connect.HandlerOption) (string, http.Handler)
	Authorize            AuthorizeFunc
	AllowUnauthenticated bool
}

var servicePath = regexp.MustCompile(`^/(?:[A-Za-z_][A-Za-z0-9_]*\.)*[A-Za-z_][A-Za-z0-9_]*/$`)
var methodName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func buildHandlers(c ServerConfig, logger *slog.Logger, registrations []Handler, extra []connect.Interceptor, observers ...*observe.Instruments) (map[string]http.Handler, error) {
	if len(registrations) == 0 {
		return nil, errors.New("RPC server requires at least one handler")
	}
	var observer *observe.Instruments
	if len(observers) > 0 {
		observer = observers[0]
	}
	handlers := make(map[string]http.Handler, len(registrations))
	for _, r := range registrations {
		if r.Build == nil || (r.Authorize != nil) == r.AllowUnauthenticated {
			return nil, errors.New("RPC handler requires a factory and exactly one authorization policy")
		}
		chain := []connect.Interceptor{serverInterceptor{logger: logger, authorize: r.Authorize, observer: observer}}
		chain = append(chain, extra...)
		prefix, h := r.Build(connect.WithReadMaxBytes(c.MaxMessageBytes), connect.WithSendMaxBytes(c.MaxMessageBytes), connect.WithInterceptors(chain...))
		if !servicePath.MatchString(prefix) || h == nil {
			return nil, errors.New("invalid RPC service handler")
		}
		if _, exists := handlers[prefix]; exists {
			return nil, fmt.Errorf("duplicate RPC service %s", prefix)
		}
		handlers[prefix] = h
	}
	return handlers, nil
}

func lookupHandler(handlers map[string]http.Handler, path string) http.Handler {
	i := strings.LastIndexByte(path, '/')
	if i < 1 || !methodName.MatchString(path[i+1:]) {
		return nil
	}
	return handlers[path[:i+1]]
}

type requestIDKey struct{}

// RequestID returns the server-generated identifier for the current RPC.
func RequestID(ctx context.Context) string { id, _ := ctx.Value(requestIDKey{}).(string); return id }

type serverInterceptor struct {
	logger    *slog.Logger
	authorize AuthorizeFunc
	observer  *observe.Instruments
}

func (i serverInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (resp connect.AnyResponse, err error) {
		start := time.Now()
		ctx, finish := i.observer.Start(i.observer.Extract(ctx, req.Header()), "rpc.server", req.Spec().Procedure, trace.SpanKindServer)
		logCtx := ctx
		defer func() {
			if recover() != nil {
				resp = nil
				err = connect.NewError(connect.CodeInternal, errors.New("internal error"))
			}
			err = safeError(err)
			finish(rpcOutcome(err))
			i.log(logCtx, req.Spec().Procedure, start, err)
		}()
		if i.authorize != nil {
			ctx, err = i.authorize(ctx, req.Spec().Procedure, req.Header())
			if err != nil {
				return nil, err
			}
			if ctx == nil {
				return nil, errors.New("invalid authorization context")
			}
		}
		return next(ctx, req)
	}
}
func (i serverInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) (err error) {
		start := time.Now()
		ctx, finish := i.observer.Start(i.observer.Extract(ctx, conn.RequestHeader()), "rpc.server", conn.Spec().Procedure, trace.SpanKindServer)
		logCtx := ctx
		defer func() {
			if recover() != nil {
				err = connect.NewError(connect.CodeInternal, errors.New("internal error"))
			}
			err = safeError(err)
			finish(rpcOutcome(err))
			i.log(logCtx, conn.Spec().Procedure, start, err)
		}()
		if i.authorize != nil {
			ctx, err = i.authorize(ctx, conn.Spec().Procedure, conn.RequestHeader())
			if err != nil {
				return err
			}
			if ctx == nil {
				return errors.New("invalid authorization context")
			}
		}
		return next(ctx, conn)
	}
}
func (i serverInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}
func (i serverInterceptor) log(ctx context.Context, procedure string, start time.Time, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	code := "ok"
	level := slog.LevelInfo
	if err != nil {
		code = connect.CodeOf(err).String()
		if connect.CodeOf(err) == connect.CodeInternal || connect.CodeOf(err) == connect.CodeUnknown {
			level = slog.LevelError
		}
	}
	i.logger.Log(ctx, level, "rpc completed", "procedure", procedure, "request_id", RequestID(ctx), "code", code, "duration", time.Since(start))
}

// safeError preserves the protocol code, never arbitrary error text, metadata
// or details. Application-level error messages can be added by a later explicit policy.
func safeError(err error) error {
	if err == nil {
		return nil
	}
	code := connect.CodeOf(err)
	if errors.Is(err, context.Canceled) {
		code = connect.CodeCanceled
	} else if errors.Is(err, context.DeadlineExceeded) {
		code = connect.CodeDeadlineExceeded
	}
	if code == connect.CodeUnknown {
		code = connect.CodeInternal
	}
	return connect.NewError(code, errors.New(strings.ReplaceAll(code.String(), "_", " ")))
}
