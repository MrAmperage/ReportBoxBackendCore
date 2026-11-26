package nats

import (
	"testing"
	"time"
)

func TestConfig(t *testing.T) {
	for _, tc := range []struct {
		name                string
		change              func(*Config)
		production, wantErr bool
	}{
		{"default", func(*Config) {}, false, false},
		{"production", func(*Config) {}, true, true},
		{"explicit plaintext", func(c *Config) { c.AllowPlaintext = true }, true, false},
		{"remote plaintext", func(c *Config) { c.URL = "nats://example.com:4222" }, false, true},
		{"TLS", func(c *Config) { c.URL = "tls://example.com:4222" }, true, false},
		{"secret in URL", func(c *Config) { c.URL = "nats://user:secret@localhost:4222" }, false, true},
		{"zero timeout", func(c *Config) { c.ConnectTimeout = 0 * time.Second }, false, true},
		{"two auth methods", func(c *Config) { c.Token = "secret"; c.CredentialsFile = "file" }, false, true},
		{"plaintext token", func(c *Config) { c.Token = "secret" }, false, true},
		{"invalid port", func(c *Config) { c.URL = "nats://localhost:99999" }, false, true},
		{"multiple endpoints", func(c *Config) { c.URL = "nats://localhost:4222,nats://localhost:4223" }, false, true},
		{"incomplete TLS", func(c *Config) { c.CertFile = "file" }, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := DefaultConfig()
			tc.change(&c)
			if got := c.Validate(tc.production) != nil; got != tc.wantErr {
				t.Fatalf("error=%v want=%v", got, tc.wantErr)
			}
		})
	}
}
func TestInvalidEnv(t *testing.T) {
	t.Setenv("NATS_CONNECT_TIMEOUT", "")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected invalid duration")
	}
}
