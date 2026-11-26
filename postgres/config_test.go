package postgres

import (
	"strings"
	"testing"
	"time"
)

func TestConfigPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, dsn                string
		production, allow, valid bool
	}{
		{"loopback", "postgres://u:p@127.0.0.1/db?sslmode=disable", false, false, true},
		{"production plaintext denied", "postgres://u:p@127.0.0.1/db?sslmode=disable", true, false, false},
		{"explicit protected network", "postgres://u:p@db/db?sslmode=disable", true, true, true},
		{"remote denied", "postgres://u:p@db/db?sslmode=disable", false, false, false},
		{"verified", "postgresql://u:p@db:5432/db?sslmode=verify-full", true, false, true},
		{"weak TLS", "postgres://u:secret@db/db?sslmode=require", true, true, false},
		{"implicit TLS", "postgres://u:secret@db/db", false, false, false},
		{"fallback", "postgres://u:secret@db,db2/db?sslmode=verify-full", false, false, false},
		{"override", "postgres://u:secret@db/db?sslmode=verify-full&host=other", false, false, false},
		{"duplicate", "postgres://u:secret@db/db?sslmode=disable&sslmode=verify-full", false, false, false},
		{"unix", "postgres://u:secret@%2Ftmp/db?sslmode=disable", false, false, false},
		{"bad port", "postgres://u:secret@db:65536/db?sslmode=verify-full", false, false, false},
		{"tls pair", "postgres://u:secret@db/db?sslmode=verify-full&sslcert=x", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := DefaultConfig()
			c.DSN = tc.dsn
			c.AllowPlaintext = tc.allow
			e := c.Validate(tc.production)
			if (e == nil) != tc.valid {
				t.Fatalf("validation=%v want valid=%v", e, tc.valid)
			}
			if e != nil && strings.Contains(e.Error(), "secret") {
				t.Fatal("credential leaked")
			}
		})
	}
}
func TestLimits(t *testing.T) {
	c := DefaultConfig()
	c.DSN = "postgres://u@localhost/db?sslmode=disable"
	c.MinConns = c.MaxConns + 1
	if c.Validate(false) == nil {
		t.Fatal("accepted invalid pool limits")
	}
	c.MinConns = 0
	c.HealthTimeout = 0
	if c.Validate(false) == nil {
		t.Fatal("accepted zero timeout")
	}
}
func TestLoadConfig(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://u@localhost/db?sslmode=disable")
	t.Setenv("POSTGRES_MAX_CONNS", "4")
	t.Setenv("POSTGRES_CONNECT_TIMEOUT", "3s")
	c, e := LoadConfig()
	if e != nil || c.MaxConns != 4 || c.ConnectTimeout != 3*time.Second {
		t.Fatalf("configuration not loaded: %v", e)
	}
	t.Setenv("POSTGRES_MAX_CONNS", "")
	if _, e = LoadConfig(); e == nil {
		t.Fatal("accepted empty integer")
	}
}
