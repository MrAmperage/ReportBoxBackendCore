package auth

import (
	"context"
	"crypto"
	"go.uber.org/fx"
	"testing"
)

func TestModuleRequiresProviders(t *testing.T) {
	c := testConfig()
	if fx.ValidateApp(Module(c), fx.NopLogger) == nil {
		t.Fatal("missing providers accepted")
	}
	if fx.ValidateApp(Module(c, c), fx.NopLogger) == nil {
		t.Fatal("duplicate config accepted")
	}
	var v *Validator
	a := fx.New(Module(c), fx.NopLogger, fx.Provide(func() KeyProvider {
		return keysFunc(func(context.Context, string, string, string) (crypto.PublicKey, error) {
			t.Fatal("constructor I/O")
			return nil, ErrUnknownKey
		})
	}, func() StateValidator {
		return stateFunc(func(context.Context, Identity, ValidationMode) error { t.Fatal("constructor I/O"); return nil })
	}), fx.Populate(&v))
	if a.Err() != nil || v == nil {
		t.Fatal(a.Err())
	}
}
