package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestOTLPTransport(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    []byte
		wantErr bool
	}{
		{"success", 200, nil, false},
		{"http failure", 503, []byte("private-token"), true},
		{"redirect", 302, nil, true},
		{"malformed", 200, []byte{0xff}, true},
		{"oversized", 200, make([]byte, (64<<10)+1), true},
		{"partial", 200, mustMarshal(t, &collector.ExportTraceServiceResponse{PartialSuccess: &collector.ExportTracePartialSuccess{RejectedSpans: 1, ErrorMessage: "private-token"}}), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-protobuf" || r.URL.Path != "/v1/traces" {
					t.Error("unexpected OTLP request")
				}
				b, e := io.ReadAll(r.Body)
				var request collector.ExportTraceServiceRequest
				if e != nil || proto.Unmarshal(b, &request) != nil || len(request.ResourceSpans) != 1 {
					t.Error("invalid OTLP body")
				}
				w.Header().Set("Location", "/redirect-target")
				w.WriteHeader(tc.status)
				_, _ = w.Write(tc.body)
			}))
			defer srv.Close()
			cfg := DefaultConfig()
			cfg.Endpoint = srv.URL + "/v1/traces"
			cfg.AllowPlaintext = true
			cfg.ExportTimeout = time.Second
			c := newOTLPClient(cfg)
			defer c.Stop(context.Background())
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := c.UploadTraces(ctx, []*tracepb.ResourceSpans{{}})
			if (err != nil) != tc.wantErr || err != nil && strings.Contains(err.Error(), "private-token") {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
}
func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, e := proto.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
