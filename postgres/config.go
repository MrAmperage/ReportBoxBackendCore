// Package postgres manages a shared PostgreSQL connection pool.
package postgres

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config owns pool limits and connection policy. DSN is sensitive.
type Config struct {
	DSN                                                                                string
	AllowPlaintext                                                                     bool
	MaxConns, MinConns                                                                 int32
	ConnectTimeout, HealthTimeout, MaxConnLifetime, MaxConnIdleTime, HealthCheckPeriod time.Duration
}

// DefaultConfig returns defaults without reading environment or connecting.
func DefaultConfig() Config {
	return Config{MaxConns: 10, ConnectTimeout: 5 * time.Second, HealthTimeout: 2 * time.Second, MaxConnLifetime: time.Hour, MaxConnIdleTime: 15 * time.Minute, HealthCheckPeriod: time.Minute}
}

// LoadConfig reads module settings from the environment.
func LoadConfig() (Config, error) {
	c := DefaultConfig()
	if v, ok := os.LookupEnv("POSTGRES_DSN"); ok {
		c.DSN = v
	}
	if v, ok := os.LookupEnv("POSTGRES_ALLOW_PLAINTEXT"); ok {
		b, e := strconv.ParseBool(v)
		if e != nil {
			return c, errors.New("invalid POSTGRES_ALLOW_PLAINTEXT")
		}
		c.AllowPlaintext = b
	}
	for k, p := range map[string]*int32{"POSTGRES_MAX_CONNS": &c.MaxConns, "POSTGRES_MIN_CONNS": &c.MinConns} {
		if v, ok := os.LookupEnv(k); ok {
			n, e := strconv.ParseInt(v, 10, 32)
			if e != nil {
				return c, errors.New("invalid " + k)
			}
			*p = int32(n)
		}
	}
	for k, p := range map[string]*time.Duration{"POSTGRES_CONNECT_TIMEOUT": &c.ConnectTimeout, "POSTGRES_HEALTH_TIMEOUT": &c.HealthTimeout, "POSTGRES_MAX_CONN_LIFETIME": &c.MaxConnLifetime, "POSTGRES_MAX_CONN_IDLE_TIME": &c.MaxConnIdleTime, "POSTGRES_HEALTH_CHECK_PERIOD": &c.HealthCheckPeriod} {
		if v, ok := os.LookupEnv(k); ok {
			d, e := time.ParseDuration(v)
			if e != nil {
				return c, errors.New("invalid " + k)
			}
			*p = d
		}
	}
	return c, c.Validate(false)
}

// endpoint validates a single explicit TCP URL, avoiding ambiguous fallbacks.
func (c Config) endpoint() (*url.URL, error) {
	u, e := url.Parse(c.DSN)
	if e != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Opaque != "" || u.Hostname() == "" || strings.ContainsAny(u.Host, ",/\\ \t\r\n") || u.User == nil || u.User.Username() == "" || len(u.Path) < 2 || strings.Contains(u.Path[1:], "/") || u.Fragment != "" {
		return nil, errors.New("POSTGRES_DSN must be a single PostgreSQL TCP URL with user and database")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return nil, errors.New("invalid PostgreSQL port")
		}
	}
	q, e := url.ParseQuery(u.RawQuery)
	if e != nil {
		return nil, errors.New("invalid PostgreSQL URL options")
	}
	for k, vs := range q {
		if len(vs) != 1 || vs[0] == "" {
			return nil, errors.New("invalid PostgreSQL URL options")
		}
		switch k {
		case "sslmode", "sslrootcert", "sslcert", "sslkey":
		default:
			return nil, errors.New("unsupported PostgreSQL URL option")
		}
	}
	if mode := q.Get("sslmode"); mode != "verify-full" && mode != "disable" {
		return nil, errors.New("POSTGRES_DSN requires sslmode=verify-full or sslmode=disable")
	}
	if (q.Get("sslcert") == "") != (q.Get("sslkey") == "") {
		return nil, errors.New("PostgreSQL client certificate and key must be paired")
	}
	if q.Get("sslmode") == "disable" && (q.Get("sslrootcert") != "" || q.Get("sslcert") != "") {
		return nil, errors.New("TLS files cannot be used with disabled TLS")
	}
	return u, nil
}

// Validate checks config without making network connections.
func (c Config) Validate(production bool) error {
	u, e := c.endpoint()
	if e != nil {
		return e
	}
	if c.MaxConns <= 0 || c.MinConns < 0 || c.MinConns > c.MaxConns || c.ConnectTimeout <= 0 || c.HealthTimeout <= 0 || c.MaxConnLifetime <= 0 || c.MaxConnIdleTime <= 0 || c.HealthCheckPeriod <= 0 {
		return errors.New("invalid PostgreSQL pool limits or timeouts")
	}
	ip := net.ParseIP(u.Hostname())
	local := strings.EqualFold(u.Hostname(), "localhost") || (ip != nil && ip.IsLoopback())
	if u.Query().Get("sslmode") == "disable" && !c.AllowPlaintext && (production || !local) {
		return errors.New("PostgreSQL plaintext requires explicit permission")
	}
	return nil
}
