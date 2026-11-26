// Package nats integrates one managed NATS connection into BackendCore.
package nats

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains connection settings. Credentials are never accepted in URLs.
type Config struct {
	URL                                               string
	AllowPlaintext                                    bool
	CredentialsFile, Token, CAFile, CertFile, KeyFile string
	ConnectTimeout, DrainTimeout, ReconnectWait       time.Duration
	MaxReconnect                                      int
}

// DefaultConfig returns development defaults, without reading the environment.
func DefaultConfig() Config {
	return Config{URL: "nats://127.0.0.1:4222", ConnectTimeout: 5 * time.Second, DrainTimeout: 5 * time.Second, ReconnectWait: 2 * time.Second, MaxReconnect: 60}
}

// LoadConfig resolves environment settings; explicit empty/invalid values fail validation.
func LoadConfig() (Config, error) {
	c := DefaultConfig()
	for k, v := range map[string]*string{"NATS_URL": &c.URL, "NATS_CREDENTIALS_FILE": &c.CredentialsFile, "NATS_TOKEN": &c.Token, "NATS_CA_FILE": &c.CAFile, "NATS_CERT_FILE": &c.CertFile, "NATS_KEY_FILE": &c.KeyFile} {
		if s, ok := os.LookupEnv(k); ok {
			*v = s
		}
	}
	for k, v := range map[string]*time.Duration{"NATS_CONNECT_TIMEOUT": &c.ConnectTimeout, "NATS_DRAIN_TIMEOUT": &c.DrainTimeout, "NATS_RECONNECT_WAIT": &c.ReconnectWait} {
		if s, ok := os.LookupEnv(k); ok {
			d, e := time.ParseDuration(s)
			if e != nil {
				return c, errors.New("invalid " + k)
			}
			*v = d
		}
	}
	if s, ok := os.LookupEnv("NATS_MAX_RECONNECT"); ok {
		v, e := strconv.Atoi(s)
		if e != nil {
			return c, errors.New("invalid NATS_MAX_RECONNECT")
		}
		c.MaxReconnect = v
	}
	if s, ok := os.LookupEnv("NATS_ALLOW_PLAINTEXT"); ok {
		v, e := strconv.ParseBool(s)
		if e != nil {
			return c, errors.New("invalid NATS_ALLOW_PLAINTEXT")
		}
		c.AllowPlaintext = v
	}
	return c, c.Validate(false)
}

// Validate checks settings without reading credential files or opening sockets.
func (c Config) Validate(production bool) error {
	u, e := url.Parse(c.URL)
	if e != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(c.URL, ", \t\r\n") || (u.Scheme != "nats" && u.Scheme != "tls") {
		return errors.New("NATS_URL must be one nats:// or tls:// endpoint without credentials or path")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return errors.New("invalid NATS port")
		}
	}
	if c.ConnectTimeout <= 0 || c.DrainTimeout <= 0 || c.ReconnectWait <= 0 || c.MaxReconnect < 0 {
		return errors.New("invalid NATS timeout or reconnect limit")
	}
	if (c.CertFile == "") != (c.KeyFile == "") || (c.CredentialsFile != "" && c.Token != "") {
		return errors.New("invalid NATS credentials or key pair")
	}
	if u.Scheme != "tls" {
		ip := net.ParseIP(u.Hostname())
		local := strings.EqualFold(u.Hostname(), "localhost") || (ip != nil && ip.IsLoopback())
		if c.CAFile != "" || c.CertFile != "" {
			return errors.New("NATS TLS files require tls://")
		}
		if !c.AllowPlaintext && (production || !local || c.CredentialsFile != "" || c.Token != "") {
			return errors.New("NATS plaintext requires explicit permission")
		}
	}
	return nil
}
