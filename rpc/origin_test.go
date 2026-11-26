package rpc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOriginAuthorityAndLifecycle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "start")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	lifetime, cancel := context.WithCancel(context.Background())
	defer cancel()
	var active atomic.Bool
	active.Store(true)
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	c := originClient{client: server.Client(), scheme: u.Scheme, host: u.Host, lifetime: lifetime, active: &active}
	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "other.example"
	if _, err = c.Do(req); err == nil || !strings.Contains(err.Error(), "origin mismatch") {
		t.Fatalf("authority mismatch: %v", err)
	}
	req.Host = u.Host
	ctx, done := context.WithTimeout(context.Background(), 3*time.Second)
	defer done()
	resp, err := c.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 5)
	if _, err = io.ReadFull(resp.Body, buf); err != nil || string(buf) != "start" {
		t.Fatalf("stream start: %q %v", buf, err)
	}
	active.Store(false)
	cancel()
	if _, err = resp.Body.Read(buf); err == nil {
		t.Fatal("stream not canceled")
	}
	if ctx.Err() != nil {
		t.Fatal("stream only ended by caller deadline")
	}
	if _, err = c.Do(req); err == nil {
		t.Fatal("request admitted after close")
	}
}
