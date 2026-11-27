package auth

import (
	"context"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Validate checks a compact signed access token, then calls the mandatory state
// provider. No verified Identity is returned on failure. Provider errors are
// mapped to safe sentinels, not wrapped with potentially sensitive details.
func (v *Validator) Validate(ctx context.Context, raw string, mode ValidationMode) (Identity, error) {
	if v == nil || v.keys == nil || v.state == nil || !mode.Valid() {
		return Identity{}, ErrUnavailable
	}
	if e := ctx.Err(); e != nil {
		return Identity{}, e
	}
	now := time.Now()
	i, e := v.preflight(raw, now)
	if e != nil {
		return Identity{}, e
	}
	keyCtx, cancel := context.WithTimeout(ctx, v.cfg.ProviderTimeout)
	defer cancel()
	key, e := v.keys.ResolveKey(keyCtx, i.issuer, i.keyID, i.algorithm)
	timedOut := providerExpired(keyCtx)
	cancel()
	if e2 := ctx.Err(); e2 != nil {
		return Identity{}, e2
	}
	if timedOut {
		return Identity{}, ErrUnavailable
	}
	if e != nil {
		if errors.Is(e, ErrUnknownKey) || errors.Is(e, ErrRevoked) {
			return Identity{}, ErrInvalidToken
		}
		return Identity{}, ErrUnavailable
	}
	if !validKey(key, i.algorithm) {
		return Identity{}, ErrUnavailable
	}
	// The strict preflight checks structure and types; jwt/v5 independently checks
	// signature, algorithm and registered claims with the same clock sample.
	t, e := jwt.Parse(raw, func(*jwt.Token) (any, error) { return key, nil }, jwt.WithValidMethods([]string{v.cfg.Algorithm}), jwt.WithIssuer(v.cfg.Issuer), jwt.WithAudience(v.cfg.Audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithLeeway(v.cfg.ClockSkew), jwt.WithTimeFunc(func() time.Time { return now }), jwt.WithJSONNumber(), jwt.WithStrictDecoding())
	if e != nil || t == nil || !t.Valid {
		return Identity{}, ErrInvalidToken
	}
	if e = v.Revalidate(ctx, i, mode); e != nil {
		return Identity{}, e
	}
	return i, nil
}
