package auth

import (
	"errors"
	"go.uber.org/fx"
)

// Module registers a passive Validator. With zero arguments it reads AUTH_*;
// one complete Config overrides ENV. Providers must be explicitly bound in Fx.
func Module(settings ...Config) fx.Option {
	if len(settings) > 1 {
		return fx.Error(errors.New("auth accepts one configuration"))
	}
	load := LoadConfig
	if len(settings) == 1 {
		c := settings[0]
		load = func() (Config, error) { return c, nil }
	}
	// Invoke forces validation even if a consumer forgot to inject Validator.
	return fx.Module("backendcore.auth", fx.Provide(load, NewValidator), fx.Invoke(func(*Validator) {}))
}
