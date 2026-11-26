// Package management provides the optional private HTTP monitoring listener.
package management

import (
	"errors"
	"net"
	"os"
	"strconv"
	"time"
)

// Config controls the private HTTP listener. Non-loopback exposure is opt-in.
type Config struct {
	Address                                        string
	AllowRemote                                    bool
	ReadHeaderTimeout, RequestTimeout, IdleTimeout time.Duration
	MaxHeaderBytes, MaxConcurrent                  int
}

// DefaultConfig returns the agreed loopback configuration.
func DefaultConfig() Config {
	return Config{Address: "127.0.0.1:7613", ReadHeaderTimeout: 2 * time.Second, RequestTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 16 << 10, MaxConcurrent: 16}
}

// Validate checks settings without opening sockets.
func (c Config) Validate() error {
	host, port, err := net.SplitHostPort(c.Address)
	n, pe := strconv.Atoi(port)
	ip := net.ParseIP(host)
	if err != nil || pe != nil || n < 0 || n > 65535 || ip == nil {
		return errors.New("MANAGEMENT_ADDRESS must contain an explicit IP and numeric port")
	}
	if !ip.IsLoopback() && !c.AllowRemote {
		return errors.New("non-loopback management requires MANAGEMENT_ALLOW_REMOTE=true")
	}
	if c.ReadHeaderTimeout <= 0 || c.RequestTimeout <= 0 || c.IdleTimeout <= 0 || c.MaxHeaderBytes <= 0 || c.MaxConcurrent <= 0 || c.RequestTimeout > time.Duration(1<<63-1)-c.ReadHeaderTimeout || c.MaxHeaderBytes > int(^uint(0)>>1)-4096 {
		return errors.New("invalid management timeouts or limits")
	}
	return nil
}

// LoadConfig reads module-specific environment settings; invalid values fail.
func LoadConfig() (Config, error) {
	c := DefaultConfig()
	if v, ok := os.LookupEnv("MANAGEMENT_ADDRESS"); ok {
		c.Address = v
	}
	if v, ok := os.LookupEnv("MANAGEMENT_ALLOW_REMOTE"); ok {
		b, e := strconv.ParseBool(v)
		if e != nil {
			return c, errors.New("invalid MANAGEMENT_ALLOW_REMOTE")
		}
		c.AllowRemote = b
	}
	for k, p := range map[string]*time.Duration{"MANAGEMENT_READ_HEADER_TIMEOUT": &c.ReadHeaderTimeout, "MANAGEMENT_REQUEST_TIMEOUT": &c.RequestTimeout, "MANAGEMENT_IDLE_TIMEOUT": &c.IdleTimeout} {
		if v, ok := os.LookupEnv(k); ok {
			d, e := time.ParseDuration(v)
			if e != nil {
				return c, errors.New("invalid " + k)
			}
			*p = d
		}
	}
	for k, p := range map[string]*int{"MANAGEMENT_MAX_HEADER_BYTES": &c.MaxHeaderBytes, "MANAGEMENT_MAX_CONCURRENT": &c.MaxConcurrent} {
		if v, ok := os.LookupEnv(k); ok {
			n, e := strconv.Atoi(v)
			if e != nil {
				return c, errors.New("invalid " + k)
			}
			*p = n
		}
	}
	return c, c.Validate()
}
