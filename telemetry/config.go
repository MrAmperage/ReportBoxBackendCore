// Package telemetry installs per-application tracing and Prometheus metrics.
package telemetry

import (
	"errors"
	"math"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config defines an optional OTLP/HTTP protobuf trace exporter.
// An empty endpoint enables metrics only. The endpoint is the full traces URL.
type Config struct {
	Endpoint       string
	AllowPlaintext bool
	SampleRatio    float64
	ExportTimeout  time.Duration
}

// DefaultConfig enables metrics and disables trace export until an endpoint is configured.
func DefaultConfig() Config { return Config{SampleRatio: 0.1, ExportTimeout: 5 * time.Second} }

// Validate checks settings without opening network resources.
func (c Config) Validate() error {
	if math.IsNaN(c.SampleRatio) || math.IsInf(c.SampleRatio, 0) || c.SampleRatio < 0 || c.SampleRatio > 1 || c.ExportTimeout <= 0 {
		return errors.New("invalid telemetry sampling or timeout")
	}
	if c.Endpoint == "" {
		return nil
	}
	u, e := url.Parse(c.Endpoint)
	if e != nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery || u.Path == "" || u.RawPath != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("invalid OTLP traces endpoint")
	}
	if strings.ContainsAny(u.Host, " ,\\/\t\r\n") || strings.ContainsAny(u.Path, "\r\n\x00") {
		return errors.New("invalid OTLP traces endpoint")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return errors.New("invalid OTLP port")
		}
	}
	if strings.Contains(u.Hostname(), ":") && net.ParseIP(u.Hostname()) == nil {
		return errors.New("invalid OTLP host")
	}
	if u.Scheme == "http" && !c.AllowPlaintext {
		return errors.New("OTLP HTTP plaintext requires TELEMETRY_ALLOW_PLAINTEXT=true")
	}
	return nil
}

// LoadConfig reads the supported environment settings.
func LoadConfig() (Config, error) {
	c := DefaultConfig()
	if v, ok := os.LookupEnv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); ok {
		c.Endpoint = v
	} else if v, ok := os.LookupEnv("OTEL_EXPORTER_OTLP_ENDPOINT"); ok && v != "" {
		u, err := url.Parse(v)
		if err != nil || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery || u.Opaque != "" {
			return c, errors.New("invalid OTLP base endpoint")
		}
		c.Endpoint = strings.TrimRight(v, "/") + "/v1/traces"
	}
	if v, ok := os.LookupEnv("TELEMETRY_ALLOW_PLAINTEXT"); ok {
		b, e := strconv.ParseBool(v)
		if e != nil {
			return c, errors.New("invalid TELEMETRY_ALLOW_PLAINTEXT")
		}
		c.AllowPlaintext = b
	}
	if v, ok := os.LookupEnv("OTEL_TRACES_SAMPLER_ARG"); ok {
		n, e := strconv.ParseFloat(v, 64)
		if e != nil {
			return c, errors.New("invalid OTEL_TRACES_SAMPLER_ARG")
		}
		c.SampleRatio = n
	}
	if v, ok := os.LookupEnv("OTEL_TRACES_SAMPLER"); ok && v != "parentbased_traceidratio" {
		return c, errors.New("only parentbased_traceidratio sampler supported")
	}
	for _, k := range []string{"OTEL_EXPORTER_OTLP_PROTOCOL", "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"} {
		if v, ok := os.LookupEnv(k); ok && v != "http/protobuf" {
			return c, errors.New("only OTLP http/protobuf supported")
		}
	}
	if v, ok := os.LookupEnv("TELEMETRY_EXPORT_TIMEOUT"); ok {
		d, e := time.ParseDuration(v)
		if e != nil {
			return c, errors.New("invalid TELEMETRY_EXPORT_TIMEOUT")
		}
		c.ExportTimeout = d
	}
	return c, c.Validate()
}
