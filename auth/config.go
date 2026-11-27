// Package auth validates signed access tokens and delegates current-state checks
// to a trusted application provider. It does not issue tokens or own sessions.
package auth

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Config defines one trust domain. Issuer and Audience are required. Exactly one
// algorithm is pinned, independently of the untrusted JWT header.
type Config struct {
	Issuer          string
	Audience        string
	Algorithm       string
	TokenType       string
	ClockSkew       time.Duration
	ProviderTimeout time.Duration
	MaxTokenBytes   int
}

// DefaultConfig selects Ed25519 access tokens; supply Issuer and Audience.
func DefaultConfig() Config {
	return Config{Algorithm: "EdDSA", TokenType: "at+jwt", ClockSkew: 30 * time.Second, ProviderTimeout: 2 * time.Second, MaxTokenBytes: 8192}
}

// Validate performs no I/O.
func (c Config) Validate() error {
	if !validText(c.Issuer, 512) || !validText(c.Audience, 512) || !validText(c.TokenType, 64) {
		return errors.New("auth issuer, audience and token type are required")
	}
	switch c.Algorithm {
	case "EdDSA", "RS256", "ES256":
	default:
		return errors.New("unsupported auth algorithm")
	}
	if c.ClockSkew < 0 || c.ClockSkew > time.Minute || c.ProviderTimeout <= 0 || c.ProviderTimeout > time.Minute || c.MaxTokenBytes < 256 || c.MaxTokenBytes > 65536 {
		return errors.New("invalid auth limits")
	}
	return nil
}

// LoadConfig reads AUTH_* variables. Explicit invalid values are rejected.
func LoadConfig() (Config, error) {
	c := DefaultConfig()
	for k, p := range map[string]*string{"AUTH_ISSUER": &c.Issuer, "AUTH_AUDIENCE": &c.Audience, "AUTH_ALGORITHM": &c.Algorithm, "AUTH_TOKEN_TYPE": &c.TokenType} {
		if s, ok := os.LookupEnv(k); ok {
			*p = s
		}
	}
	for k, p := range map[string]*time.Duration{"AUTH_CLOCK_SKEW": &c.ClockSkew, "AUTH_PROVIDER_TIMEOUT": &c.ProviderTimeout} {
		if s, ok := os.LookupEnv(k); ok {
			d, e := time.ParseDuration(s)
			if e != nil {
				return c, errors.New("invalid " + k)
			}
			*p = d
		}
	}
	if s, ok := os.LookupEnv("AUTH_MAX_TOKEN_BYTES"); ok {
		n, e := strconv.Atoi(s)
		if e != nil {
			return c, errors.New("invalid AUTH_MAX_TOKEN_BYTES")
		}
		c.MaxTokenBytes = n
	}
	return c, c.Validate()
}

func validText(s string, limit int) bool {
	if s == "" || len(s) > limit || !utf8.ValidString(s) || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
