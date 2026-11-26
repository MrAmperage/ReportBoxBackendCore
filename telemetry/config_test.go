package telemetry

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func TestConfigValidation(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"https://collector.example/v1/traces", "https://[::1]:4318/v1/traces"} {
		c := DefaultConfig()
		c.Endpoint = endpoint
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, endpoint := range []string{"http://127.0.0.1:4318/v1/traces", "https://alice:private@collector/v1/traces", "https://collector/v1/traces?secret=private", "https://collector:0/v1/traces", "https://collector:99999/v1/traces", "https://collector", "grpc://collector:4317/v1/traces", "https://collector/v1/%0a", "https://collector/v1/%2ftraces"} {
		c := DefaultConfig()
		c.Endpoint = endpoint
		if err := c.Validate(); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatalf("invalid endpoint accepted or leaked: %v", err)
		}
	}
	c := DefaultConfig()
	c.Endpoint = "http://127.0.0.1:4318/v1/traces"
	c.AllowPlaintext = true
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, ratio := range []float64{-1, 1.1, math.NaN(), math.Inf(1)} {
		c.SampleRatio = ratio
		if c.Validate() == nil {
			t.Fatal("bad ratio")
		}
	}
	c.SampleRatio = 1
	c.ExportTimeout = 0
	if c.Validate() == nil {
		t.Fatal("bad timeout")
	}
}
func TestConfigEnvironment(t *testing.T) {
	keys := []string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_PROTOCOL", "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "OTEL_TRACES_SAMPLER", "OTEL_TRACES_SAMPLER_ARG", "TELEMETRY_ALLOW_PLAINTEXT", "TELEMETRY_EXPORT_TIMEOUT"}
	for _, key := range keys {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://collector:4318/base/")
	t.Setenv("OTEL_TRACES_SAMPLER", "parentbased_traceidratio")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")
	t.Setenv("TELEMETRY_EXPORT_TIMEOUT", "3s")
	c, err := LoadConfig()
	if err != nil || c.Endpoint != "https://collector:4318/base/v1/traces" || c.SampleRatio != 0.25 || c.ExportTimeout != 3*time.Second {
		t.Fatalf("unexpected config: %v", err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "https://other:4318/custom")
	c, err = LoadConfig()
	if err != nil || c.Endpoint != "https://other:4318/custom" {
		t.Fatal("traces endpoint precedence")
	}
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	c, err = LoadConfig()
	if err != nil || c.Endpoint != "" {
		t.Fatal("explicit metrics-only")
	}
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	if _, err = LoadConfig(); err == nil {
		t.Fatal("unsupported protocol accepted")
	}
}
