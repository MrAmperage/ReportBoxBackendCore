package rpc

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"connectrpc.com/connect"
	"github.com/MrAmperage/ReportBoxBackendCore/internal/observe"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
)

// Protocol chooses the protocol used by a generated ConnectRPC client.
type Protocol uint8

const (
	GRPC Protocol = iota
	Connect
	GRPCWeb
)

// Target holds reusable transport settings for a generated typed client.
type Target struct {
	BaseURL    string
	HTTPClient connect.HTTPClient
	Options    []connect.ClientOption
}

// Client owns the shared TLS, HTTP/1.1 and h2c client transports.
type Client struct {
	cfg                               ClientConfig
	observer                          *observe.Instruments
	streams                           streamTracker
	production                        bool
	active                            atomic.Bool
	lifetime                          context.Context
	cancel                            context.CancelFunc
	tls, plain, h2c                   *http.Transport
	tlsClient, plainClient, h2cClient *http.Client
}

func newClient(cfg ClientConfig, production bool) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Client{cfg: cfg, production: production}, nil
}
func (c *Client) participant() lifecycle.Participant {
	return lifecycle.Participant{Name: "rpc.client", DependsOn: c.observer.Dependencies(), Open: c.open, Close: c.close}
}
func (c *Client) open(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tlsConfig, err := clientTLS(c.cfg)
	if err != nil {
		return err
	}
	transport := func(http1, http2, h2c bool) *http.Transport {
		p := new(http.Protocols)
		p.SetHTTP1(http1)
		p.SetHTTP2(http2)
		p.SetUnencryptedHTTP2(h2c)
		return &http.Transport{Protocols: p, DialContext: (&net.Dialer{Timeout: c.cfg.DialTimeout, KeepAlive: c.cfg.IdleTimeout}).DialContext, TLSClientConfig: tlsConfig.Clone(), TLSHandshakeTimeout: c.cfg.DialTimeout, IdleConnTimeout: c.cfg.IdleTimeout, MaxIdleConns: c.cfg.MaxIdleConns, MaxIdleConnsPerHost: c.cfg.MaxConnsPerHost, MaxConnsPerHost: c.cfg.MaxConnsPerHost}
	}
	c.tls = transport(true, true, false)
	c.plain = transport(true, false, false)
	c.h2c = transport(false, false, true)
	client := func(t *http.Transport) *http.Client {
		return &http.Client{Transport: t, Timeout: c.cfg.CallTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	c.tlsClient = client(c.tls)
	c.plainClient = client(c.plain)
	c.h2cClient = client(c.h2c)
	c.lifetime, c.cancel = context.WithCancel(context.Background())
	c.active.Store(true)
	return nil
}
func (c *Client) close(context.Context) error {
	c.active.Store(false)
	if c.cancel != nil {
		c.cancel()
	}
	c.streams.close()
	for _, t := range []*http.Transport{c.tls, c.plain, c.h2c} {
		if t != nil {
			t.CloseIdleConnections()
		}
	}
	return nil
}

// Target validates a service origin and returns settings for a generated client.
// Call after Open (including dependent participants' Activate). No socket is
// opened until the generated client actually executes an RPC.
func (c *Client) Target(endpoint string, protocol Protocol, extra ...connect.ClientOption) (Target, error) {
	if !c.active.Load() {
		return Target{}, errors.New("RPC client is not active")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "http" && u.Scheme != "https") {
		return Target{}, errors.New("RPC endpoint must be an HTTP(S) origin without credentials or path")
	}
	if u.Scheme == "http" && (!c.cfg.AllowPlaintext && (c.production || !loopback(u.Hostname())) || c.cfg.TLSCertFile != "") {
		return Target{}, errors.New("RPC plaintext endpoint not permitted")
	}
	var hc *http.Client
	opts := append([]connect.ClientOption(nil), extra...)
	if c.observer != nil {
		opts = append(opts, connect.WithInterceptors(clientObserver{observer: c.observer, streams: &c.streams}))
	}
	opts = append(opts, connect.WithReadMaxBytes(c.cfg.MaxMessageBytes), connect.WithSendMaxBytes(c.cfg.MaxMessageBytes))
	switch protocol {
	case GRPC:
		opts = append(opts, connect.WithGRPC())
	case Connect:
	case GRPCWeb:
		opts = append(opts, connect.WithGRPCWeb())
	default:
		return Target{}, errors.New("unsupported RPC protocol")
	}
	if u.Scheme == "https" {
		hc = c.tlsClient
	} else if protocol == GRPC {
		hc = c.h2cClient
	} else {
		hc = c.plainClient
	}
	return Target{BaseURL: strings.TrimSuffix(u.String(), "/"), HTTPClient: originClient{client: hc, scheme: u.Scheme, host: u.Host, lifetime: c.lifetime, active: &c.active}, Options: opts}, nil
}
