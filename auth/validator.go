package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"errors"
	"reflect"
	"time"
)

// Validator is a passive, concurrency-safe verifier, assuming its injected
// providers are concurrency-safe. It owns no connections or background work.
type Validator struct {
	cfg   Config
	keys  KeyProvider
	state StateValidator
}

// NewValidator requires both providers. There is no implicit signature-only mode.
func NewValidator(cfg Config, keys KeyProvider, state StateValidator) (*Validator, error) {
	if e := cfg.Validate(); e != nil {
		return nil, e
	}
	if nilProvider(keys) || nilProvider(state) {
		return nil, errors.New("auth requires key and state providers")
	}
	return &Validator{cfg: cfg, keys: keys, state: state}, nil
}
func nilProvider(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}

// Revalidate repeats time and authoritative state checks for an identity issued
// by this validator. It does not verify the original signature again; the state
// provider must detect revoked signing keys. Useful for long-running requests.
func (v *Validator) Revalidate(ctx context.Context, i Identity, mode ValidationMode) error {
	if v == nil || v.state == nil || !mode.Valid() {
		return ErrUnavailable
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if i.owner != v || !v.timeValid(i, time.Now()) {
		return ErrInvalidToken
	}
	c, cancel := context.WithTimeout(ctx, v.cfg.ProviderTimeout)
	defer cancel()
	e := v.state.Validate(c, i, mode)
	timedOut := providerExpired(c)
	cancel()
	if ce := ctx.Err(); ce != nil {
		return ce
	}
	if timedOut {
		return ErrUnavailable
	}
	switch {
	case e == nil:
		if !v.timeValid(i, time.Now()) {
			return ErrInvalidToken
		}
		return nil
	case errors.Is(e, ErrRevoked), errors.Is(e, ErrInvalidToken):
		return ErrRevoked
	case errors.Is(e, ErrPermissionDenied):
		return ErrPermissionDenied
	default:
		return ErrUnavailable
	}
}

func validKey(key crypto.PublicKey, alg string) bool {
	switch alg {
	case "EdDSA":
		k, ok := key.(ed25519.PublicKey)
		return ok && len(k) == ed25519.PublicKeySize
	case "RS256":
		k, ok := key.(*rsa.PublicKey)
		return ok && k != nil && k.N != nil && k.N.Sign() > 0 && k.N.BitLen() >= 2048 && k.N.BitLen() <= 8192 && k.E >= 65537 && k.E%2 == 1 && k.E <= 2147483647
	case "ES256":
		k, ok := key.(*ecdsa.PublicKey)
		return ok && k != nil && k.Curve == elliptic.P256() && k.X != nil && k.Y != nil && k.Curve.IsOnCurve(k.X, k.Y)
	default:
		return false
	}
}

// Check the deadline itself as well as context.Err in case its timer callback
// has not been scheduled yet when a provider returns.
func providerExpired(ctx context.Context) bool {
	if ctx.Err() != nil {
		return true
	}
	deadline, ok := ctx.Deadline()
	return ok && !time.Now().Before(deadline)
}
