// Package config resolves and validates core settings.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains fully resolved core settings.
type Config struct {
	Environment  string
	LogLevel     string
	LogFormat    string
	LogAddSource bool
	StartTimeout time.Duration
	StopTimeout  time.Duration
}

// Defaults returns the documented defaults.
func Defaults() Config {
	return Config{Environment: "development", LogLevel: "info", LogFormat: "json", StartTimeout: 30 * time.Second, StopTimeout: 30 * time.Second}
}

// Overrides distinguish omitted settings from explicitly provided zero values.
type Overrides struct {
	Environment  *string
	LogLevel     *string
	LogFormat    *string
	LogAddSource *bool
	StartTimeout *time.Duration
	StopTimeout  *time.Duration
}

// Load resolves defaults, environment and explicit overrides (highest priority).
func Load(o Overrides) (Config, error) { return resolve(os.LookupEnv, o) }

func resolve(lookup func(string) (string, bool), o Overrides) (Config, error) {
	c := Defaults()
	str := func(key string, dst *string, override *string) {
		if override != nil {
			*dst = *override
		} else if v, ok := lookup(key); ok {
			*dst = v
		}
	}
	str("SERVICE_ENVIRONMENT", &c.Environment, o.Environment)
	str("LOG_LEVEL", &c.LogLevel, o.LogLevel)
	str("LOG_FORMAT", &c.LogFormat, o.LogFormat)
	if o.LogAddSource != nil {
		c.LogAddSource = *o.LogAddSource
	} else if v, ok := lookup("LOG_ADD_SOURCE"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("LOG_ADD_SOURCE must be a boolean")
		}
		c.LogAddSource = b
	}
	duration := func(key string, dst *time.Duration, override *time.Duration) error {
		if override != nil {
			*dst = *override
			return nil
		}
		if v, ok := lookup(key); ok {
			d, err := time.ParseDuration(v)
			if err != nil {
				return fmt.Errorf("%s must be a duration", key)
			}
			*dst = d
		}
		return nil
	}
	if err := duration("SERVICE_START_TIMEOUT", &c.StartTimeout, o.StartTimeout); err != nil {
		return Config{}, err
	}
	if err := duration("SERVICE_STOP_TIMEOUT", &c.StopTimeout, o.StopTimeout); err != nil {
		return Config{}, err
	}
	return c.Normalize()
}

// Normalize validates a fully resolved configuration and normalizes enum casing.
func (c Config) Normalize() (Config, error) {
	c.Environment = strings.TrimSpace(c.Environment)
	c.LogLevel = strings.ToLower(strings.TrimSpace(c.LogLevel))
	c.LogFormat = strings.ToLower(strings.TrimSpace(c.LogFormat))
	if c.Environment == "" {
		return Config{}, fmt.Errorf("SERVICE_ENVIRONMENT must not be empty")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error")
	}
	switch c.LogFormat {
	case "json", "text":
	default:
		return Config{}, fmt.Errorf("LOG_FORMAT must be json or text")
	}
	if c.StartTimeout <= 0 {
		return Config{}, fmt.Errorf("SERVICE_START_TIMEOUT must be positive")
	}
	if c.StopTimeout <= 0 {
		return Config{}, fmt.Errorf("SERVICE_STOP_TIMEOUT must be positive")
	}
	return c, nil
}
