// Package rpc provides optional ConnectRPC server and client infrastructure.
package rpc

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"
)

// ServerConfig is a fully resolved server configuration. Plaintext is allowed
// on loopback in development; other deployments must explicitly opt in or use TLS.
type ServerConfig struct {
	Address                                        string
	AllowPlaintext                                 bool
	TLSCertFile, TLSKeyFile, ClientCAFile          string
	ReadHeaderTimeout, IdleTimeout, CallTimeout    time.Duration
	MaxHeaderBytes, MaxMessageBytes, MaxConcurrent int
}

// DefaultServerConfig returns the loopback development defaults.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{Address: "127.0.0.1:9090", ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, CallTimeout: 30 * time.Second, MaxHeaderBytes: 1 << 20, MaxMessageBytes: 4 << 20, MaxConcurrent: 128}
}

// ClientConfig controls the shared client transports. Timeout covers entire calls,
// including streams. Override it explicitly for services using longer streams.
type ClientConfig struct {
	AllowPlaintext                                 bool
	CAFile, TLSCertFile, TLSKeyFile                string
	DialTimeout, CallTimeout, IdleTimeout          time.Duration
	MaxIdleConns, MaxConnsPerHost, MaxMessageBytes int
}

// DefaultClientConfig returns the client defaults.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{DialTimeout: 5 * time.Second, CallTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second, MaxIdleConns: 100, MaxConnsPerHost: 32, MaxMessageBytes: 4 << 20}
}

func loopback(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

// Validate checks the server settings without opening files or sockets.
func (c ServerConfig) Validate(production bool) error {
	host, port, err := net.SplitHostPort(c.Address)
	n, parseErr := strconv.Atoi(port)
	if err != nil || parseErr != nil || n < 0 || n > 65535 {
		return errors.New("RPC_ADDRESS must be a host:port with numeric port")
	}
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return errors.New("RPC server requires both TLS certificate and key")
	}
	if c.ClientCAFile != "" && c.TLSCertFile == "" {
		return errors.New("RPC client CA requires server TLS")
	}
	if c.TLSCertFile == "" && !c.AllowPlaintext && (production || !loopback(host)) {
		return errors.New("RPC plaintext requires explicit permission outside loopback development")
	}
	if c.ReadHeaderTimeout <= 0 || c.IdleTimeout <= 0 || c.CallTimeout <= 0 || c.MaxHeaderBytes <= 0 || c.MaxHeaderBytes > int(^uint(0)>>1)-4096 || c.MaxMessageBytes <= 0 || c.MaxConcurrent <= 0 {
		return errors.New("invalid RPC server timeouts or limits")
	}
	return nil
}

// Validate checks the client settings without opening files or sockets.
func (c ClientConfig) Validate() error {
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return errors.New("RPC client requires both TLS certificate and key")
	}
	if c.DialTimeout <= 0 || c.CallTimeout <= 0 || c.IdleTimeout <= 0 || c.MaxIdleConns <= 0 || c.MaxConnsPerHost <= 0 || c.MaxMessageBytes <= 0 {
		return errors.New("RPC client timeouts and limits must be positive")
	}
	return nil
}

type envReader struct{ err error }

func (r *envReader) str(key string, dst *string) {
	if v, ok := os.LookupEnv(key); ok {
		*dst = v
	}
}
func (r *envReader) parse(key string, parse func(string) error) {
	if r.err != nil {
		return
	}
	if v, ok := os.LookupEnv(key); ok {
		if err := parse(v); err != nil {
			r.err = fmt.Errorf("invalid %s", key)
		}
	}
}
func (r *envReader) boolean(key string, dst *bool) {
	r.parse(key, func(v string) error { x, e := strconv.ParseBool(v); *dst = x; return e })
}
func (r *envReader) duration(key string, dst *time.Duration) {
	r.parse(key, func(v string) error { x, e := time.ParseDuration(v); *dst = x; return e })
}
func (r *envReader) integer(key string, dst *int) {
	r.parse(key, func(v string) error { x, e := strconv.Atoi(v); *dst = x; return e })
}

// LoadServerConfig reads RPC_* settings. Validation occurs in the module.
func LoadServerConfig() (ServerConfig, error) {
	c := DefaultServerConfig()
	var r envReader
	r.str("RPC_ADDRESS", &c.Address)
	r.boolean("RPC_ALLOW_PLAINTEXT", &c.AllowPlaintext)
	r.str("RPC_TLS_CERT_FILE", &c.TLSCertFile)
	r.str("RPC_TLS_KEY_FILE", &c.TLSKeyFile)
	r.str("RPC_CLIENT_CA_FILE", &c.ClientCAFile)
	r.duration("RPC_READ_HEADER_TIMEOUT", &c.ReadHeaderTimeout)
	r.duration("RPC_IDLE_TIMEOUT", &c.IdleTimeout)
	r.duration("RPC_CALL_TIMEOUT", &c.CallTimeout)
	r.integer("RPC_MAX_HEADER_BYTES", &c.MaxHeaderBytes)
	r.integer("RPC_MAX_MESSAGE_BYTES", &c.MaxMessageBytes)
	r.integer("RPC_MAX_CONCURRENT", &c.MaxConcurrent)
	return c, r.err
}

// LoadClientConfig reads RPC_CLIENT_* settings.
func LoadClientConfig() (ClientConfig, error) {
	c := DefaultClientConfig()
	var r envReader
	r.boolean("RPC_CLIENT_ALLOW_PLAINTEXT", &c.AllowPlaintext)
	r.str("RPC_CLIENT_TRUST_CA_FILE", &c.CAFile)
	r.str("RPC_CLIENT_TLS_CERT_FILE", &c.TLSCertFile)
	r.str("RPC_CLIENT_TLS_KEY_FILE", &c.TLSKeyFile)
	r.duration("RPC_CLIENT_DIAL_TIMEOUT", &c.DialTimeout)
	r.duration("RPC_CLIENT_CALL_TIMEOUT", &c.CallTimeout)
	r.duration("RPC_CLIENT_IDLE_TIMEOUT", &c.IdleTimeout)
	r.integer("RPC_CLIENT_MAX_IDLE_CONNS", &c.MaxIdleConns)
	r.integer("RPC_CLIENT_MAX_CONNS_PER_HOST", &c.MaxConnsPerHost)
	r.integer("RPC_CLIENT_MAX_MESSAGE_BYTES", &c.MaxMessageBytes)
	return c, r.err
}
