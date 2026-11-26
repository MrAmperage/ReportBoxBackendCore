package rpc

import (
	"errors"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	"github.com/MrAmperage/ReportBoxBackendCore/config"
	"github.com/MrAmperage/ReportBoxBackendCore/health"
	"github.com/MrAmperage/ReportBoxBackendCore/internal/observe"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
	"go.uber.org/fx"
)

type serverParams struct {
	fx.In
	Config       ServerConfig
	Core         config.Config
	Logger       *slog.Logger
	Health       *health.Registry
	Report       lifecycle.ReportFailure
	Signal       *ShutdownSignal
	Observer     *observe.Instruments  `optional:"true"`
	Handlers     []Handler             `group:"backendcore.rpc.handlers"`
	Interceptors []connect.Interceptor `group:"backendcore.rpc.interceptors"`
}

// InterceptorGroup is an unordered group of additional server interceptors.
// For an order-sensitive chain, provide a single composed interceptor.
const InterceptorGroup = "backendcore.rpc.interceptors"

// ServerModule installs an optional RPC listener. Pass no config to use ENV,
// or one complete config to ignore ENV for this module.
func ServerModule(settings ...ServerConfig) fx.Option {
	if len(settings) > 1 {
		return fx.Error(errors.New("RPC server accepts one configuration"))
	}
	var load func() (ServerConfig, error) = LoadServerConfig
	if len(settings) == 1 {
		cfg := settings[0]
		load = func() (ServerConfig, error) { return cfg, nil }
	}
	return fx.Module("backendcore.rpc.server", fx.Provide(load, newShutdownSignal, func(p serverParams) (*Server, error) {
		return newServer(p.Config, strings.EqualFold(p.Core.Environment, "production"), p.Logger, p.Health, p.Report, p.Handlers, p.Interceptors, p.Signal, p.Observer)
	}, fx.Annotate(func(s *Server) lifecycle.Participant { return s.participant() }, fx.ResultTags(`group:"backendcore.lifecycle"`))))
}

// ClientModule installs the optional shared client transports, without a listener.
// Pass no config to use ENV, or one complete config to ignore module ENV.
func ClientModule(settings ...ClientConfig) fx.Option {
	if len(settings) > 1 {
		return fx.Error(errors.New("RPC client accepts one configuration"))
	}
	var load func() (ClientConfig, error) = LoadClientConfig
	if len(settings) == 1 {
		cfg := settings[0]
		load = func() (ClientConfig, error) { return cfg, nil }
	}
	return fx.Module("backendcore.rpc.client", fx.Provide(load, func(p clientParams) (*Client, error) {
		c, err := newClient(p.Config, strings.EqualFold(p.Core.Environment, "production"))
		if err == nil {
			c.observer = p.Observer
		}
		return c, err
	}, fx.Annotate(func(c *Client) lifecycle.Participant { return c.participant() }, fx.ResultTags(`group:"backendcore.lifecycle"`))))
}

// ProvideHandler registers a constructor returning one Handler in Fx's value group.
func ProvideHandler(constructor any) fx.Option {
	return fx.Provide(fx.Annotate(constructor, fx.ResultTags(`group:"backendcore.rpc.handlers"`)))
}

type clientParams struct {
	fx.In
	Config   ClientConfig
	Core     config.Config
	Observer *observe.Instruments `optional:"true"`
}
