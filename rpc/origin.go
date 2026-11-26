package rpc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

// originClient pins the URL and HTTP authority and ties every response body to
// the shared client lifetime, including streams after Do has returned.
type originClient struct {
	client       *http.Client
	scheme, host string
	lifetime     context.Context
	active       *atomic.Bool
}

func (c originClient) Do(r *http.Request) (*http.Response, error) {
	if r == nil || r.URL == nil || r.URL.Scheme != c.scheme || !strings.EqualFold(r.URL.Host, c.host) || r.URL.User != nil || r.URL.Opaque != "" || (r.Host != "" && !strings.EqualFold(r.Host, c.host)) {
		return nil, errors.New("RPC target origin mismatch")
	}
	if !c.active.Load() || c.lifetime.Err() != nil {
		return nil, errors.New("RPC client is not active")
	}
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(c.lifetime, cancel)
	release := func() { stop(); cancel() }
	// AfterFunc may run asynchronously when a close races with admission.
	if c.lifetime.Err() != nil {
		release()
		return nil, errors.New("RPC client is not active")
	}
	resp, err := c.client.Do(r.Clone(ctx))
	if err != nil {
		release()
		return resp, err
	}
	resp.Body = &lifetimeBody{ReadCloser: resp.Body, release: release}
	return resp, nil
}

type lifetimeBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *lifetimeBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.once.Do(b.release)
	}
	return n, err
}

func (b *lifetimeBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.release)
	return err
}
