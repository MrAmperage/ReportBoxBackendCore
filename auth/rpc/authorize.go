// Package authrpc adapts auth.Validator to BackendCore rpc.AuthorizeFunc.
package authrpc

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"connectrpc.com/connect"
	"github.com/MrAmperage/ReportBoxBackendCore/auth"
)

// Policy explicitly permits one procedure. A zero Policy requires authentication
// and LocalPreferred state validation; Check can enforce additional permissions.
// Public procedures must not also specify a Check or RemoteRequired mode.
type Policy struct {
	AllowUnauthenticated bool
	Mode                 auth.ValidationMode
	Check                func(context.Context, auth.Identity) error
}

var procedurePattern = regexp.MustCompile(`^/(?:[A-Za-z_][A-Za-z0-9_]*\.)*[A-Za-z_][A-Za-z0-9_]*/[A-Za-z_][A-Za-z0-9_]*$`)

// NewAuthorize copies the policy map and returns a function directly assignable
// to rpc.Handler.Authorize. Unlisted procedures are denied, including new ones.
// Resource-specific authorization remains in the application handler.
func NewAuthorize(v *auth.Validator, policies map[string]Policy) (func(context.Context, string, http.Header) (context.Context, error), error) {
	if v == nil || len(policies) == 0 {
		return nil, errors.New("auth RPC requires validator and explicit procedure policies")
	}
	rules := make(map[string]Policy, len(policies))
	for p, r := range policies {
		if !procedurePattern.MatchString(p) || !r.Mode.Valid() || r.AllowUnauthenticated && (r.Check != nil || r.Mode != auth.LocalPreferred) {
			return nil, errors.New("invalid auth RPC policy")
		}
		rules[p] = r
	}
	return func(ctx context.Context, p string, h http.Header) (context.Context, error) {
		if e := ctx.Err(); e != nil {
			return nil, connectError(e)
		}
		policy, ok := rules[p]
		if !ok {
			return nil, connectError(auth.ErrPermissionDenied)
		}
		if policy.AllowUnauthenticated {
			return auth.WithIdentity(ctx, auth.Identity{}), nil
		}
		// Count case-insensitive header names as well as repeated values.
		var values []string
		for k, vs := range h {
			if strings.EqualFold(k, "Authorization") {
				values = append(values, vs...)
			}
		}
		if len(values) != 1 {
			return nil, connectError(auth.ErrInvalidToken)
		}
		scheme, raw, ok := strings.Cut(values[0], " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || raw == "" || strings.ContainsAny(raw, " \t\r\n,") {
			return nil, connectError(auth.ErrInvalidToken)
		}
		i, e := v.Validate(ctx, raw, policy.Mode)
		if e != nil {
			return nil, connectError(e)
		}
		ctx = auth.WithIdentity(ctx, i)
		if policy.Check != nil {
			if e = policy.Check(ctx, i); e != nil {
				return nil, connectError(e)
			}
		}
		if e = ctx.Err(); e != nil {
			return nil, connectError(e)
		}
		return ctx, nil
	}, nil
}
func connectError(e error) error {
	code := connect.CodeUnavailable
	switch {
	case errors.Is(e, context.Canceled):
		code = connect.CodeCanceled
	case errors.Is(e, context.DeadlineExceeded):
		code = connect.CodeDeadlineExceeded
	case errors.Is(e, auth.ErrInvalidToken), errors.Is(e, auth.ErrRevoked), errors.Is(e, auth.ErrUnknownKey):
		code = connect.CodeUnauthenticated
	case errors.Is(e, auth.ErrPermissionDenied):
		code = connect.CodePermissionDenied
	}
	return connect.NewError(code, errors.New(strings.ReplaceAll(code.String(), "_", " ")))
}
