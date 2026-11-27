// Package static provides an opt-in HTTP server for trusted frontend builds.
package static

import (
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config controls a dedicated static listener. Root must be a trusted build directory.
// SPA enables HTML navigation fallback; non-loopback HTTP is explicitly opted in.
type Config struct {
	Address                                      string
	Root                                         string
	AllowRemote                                  bool
	SPA                                          bool
	ReadHeaderTimeout, WriteTimeout, IdleTimeout time.Duration
	MaxHeaderBytes, MaxConcurrent                int
}

// DefaultConfig returns loopback-only defaults; the build root must be supplied.
func DefaultConfig() Config {
	return Config{Address: "127.0.0.1:8080", ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10, MaxConcurrent: 64}
}

// Validate checks configuration without opening files or sockets.
func (c Config) Validate() error {
	host, port, err := net.SplitHostPort(c.Address)
	n, pe := strconv.Atoi(port)
	ip := net.ParseIP(host)
	if err != nil || pe != nil || n < 0 || n > 65535 || ip == nil {
		return errors.New("STATIC_ADDRESS must contain an explicit IP and numeric port")
	}
	if !ip.IsLoopback() && !c.AllowRemote {
		return errors.New("non-loopback static listener requires STATIC_ALLOW_REMOTE=true")
	}
	if strings.TrimSpace(c.Root) == "" || strings.ContainsRune(c.Root, 0) {
		return errors.New("STATIC_ROOT is required")
	}
	if c.ReadHeaderTimeout <= 0 || c.WriteTimeout <= 0 || c.IdleTimeout <= 0 || c.MaxHeaderBytes <= 0 || c.MaxHeaderBytes > int(^uint(0)>>1)-4096 || c.MaxConcurrent <= 0 {
		return errors.New("invalid static timeouts or limits")
	}
	return nil
}

// LoadConfig reads STATIC_* settings; invalid explicit values are errors.
func LoadConfig() (Config, error) {
	c := DefaultConfig()
	for k, p := range map[string]*string{"STATIC_ADDRESS": &c.Address, "STATIC_ROOT": &c.Root} {
		if v, ok := os.LookupEnv(k); ok {
			*p = v
		}
	}
	for k, p := range map[string]*bool{"STATIC_ALLOW_REMOTE": &c.AllowRemote, "STATIC_SPA": &c.SPA} {
		if v, ok := os.LookupEnv(k); ok {
			b, e := strconv.ParseBool(v)
			if e != nil {
				return c, errors.New("invalid " + k)
			}
			*p = b
		}
	}
	for k, p := range map[string]*time.Duration{"STATIC_READ_HEADER_TIMEOUT": &c.ReadHeaderTimeout, "STATIC_WRITE_TIMEOUT": &c.WriteTimeout, "STATIC_IDLE_TIMEOUT": &c.IdleTimeout} {
		if v, ok := os.LookupEnv(k); ok {
			d, e := time.ParseDuration(v)
			if e != nil {
				return c, errors.New("invalid " + k)
			}
			*p = d
		}
	}
	for k, p := range map[string]*int{"STATIC_MAX_HEADER_BYTES": &c.MaxHeaderBytes, "STATIC_MAX_CONCURRENT": &c.MaxConcurrent} {
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
