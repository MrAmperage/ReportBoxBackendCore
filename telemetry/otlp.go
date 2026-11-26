package telemetry

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// Use the official OTLP span transformation/exporter with an explicit, small
// HTTP transport. No implicit environment, redirects, collector error text,
// unbounded response body, or global OTel error-handler modification.
type otlpClient struct {
	endpoint  string
	client    *http.Client
	transport *http.Transport
	lifetime  context.Context
	cancel    context.CancelFunc
}

var _ otlptrace.Client = (*otlpClient)(nil)

func newOTLPClient(c Config) *otlpClient {
	t := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: c.ExportTimeout, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 2, MaxConnsPerHost: 2}
	life, cancel := context.WithCancel(context.Background())
	return &otlpClient{lifetime: life, cancel: cancel, endpoint: c.Endpoint, transport: t, client: &http.Client{Transport: t, Timeout: c.ExportTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *otlpClient) Start(ctx context.Context) error { return ctx.Err() }
func (c *otlpClient) Stop(ctx context.Context) error {
	c.cancel()
	c.transport.CloseIdleConnections()
	return ctx.Err()
}
func (c *otlpClient) UploadTraces(ctx context.Context, spans []*tracepb.ResourceSpans) error {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.lifetime, cancel)
	defer func() { stop(); cancel() }()
	if c.lifetime.Err() != nil {
		return errors.New("OTLP client stopped")
	}
	data, err := proto.Marshal(&collector.ExportTraceServiceRequest{ResourceSpans: spans})
	if err != nil {
		return errors.New("OTLP encoding failed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(data))
	if err != nil {
		return errors.New("OTLP request creation failed")
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Accept", "application/x-protobuf")
	resp, err := c.client.Do(req)
	if err != nil {
		return errors.New("OTLP request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("OTLP collector rejected request")
	}
	const maxResponse = 64 << 10
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(body) > maxResponse {
		return errors.New("invalid OTLP response size")
	}
	var result collector.ExportTraceServiceResponse
	if err := proto.Unmarshal(body, &result); err != nil {
		return errors.New("invalid OTLP protobuf response")
	}
	if partial := result.GetPartialSuccess(); partial != nil && (partial.GetRejectedSpans() != 0 || partial.GetErrorMessage() != "") {
		return errors.New("OTLP collector reported partial success")
	}
	return nil
}
