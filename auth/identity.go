package auth

import (
	"context"
	"encoding/json"
	"time"
)

// Identity contains authenticated token claims. It has no exported mutable fields
// and cannot be constructed as a trusted nonzero identity by a consumer.
type Identity struct {
	owner                                                 *Validator
	issuer, subject, keyID, algorithm, tokenType, tokenID string
	audience                                              []string
	expiresAt, issuedAt, notBefore                        time.Time
	claims                                                map[string]json.RawMessage
}

// Issuer returns the verified issuer.
func (i Identity) Issuer() string { return i.issuer }

// Subject returns the verified subject.
func (i Identity) Subject() string { return i.subject }

// Audience returns a copy of the verified audiences.
func (i Identity) Audience() []string { return append([]string(nil), i.audience...) }

// KeyID returns the verified key identifier.
func (i Identity) KeyID() string { return i.keyID }

// Algorithm returns the verified signing algorithm.
func (i Identity) Algorithm() string { return i.algorithm }

// TokenType returns the verified token type.
func (i Identity) TokenType() string { return i.tokenType }

// TokenID returns the optional jti.
func (i Identity) TokenID() string { return i.tokenID }

// ExpiresAt returns the signed expiry (without clock skew).
func (i Identity) ExpiresAt() time.Time { return i.expiresAt }

// IssuedAt returns iat or zero if absent.
func (i Identity) IssuedAt() time.Time { return i.issuedAt }

// NotBefore returns nbf or zero if absent.
func (i Identity) NotBefore() time.Time { return i.notBefore }

// Claim returns an independent copy of one verified JSON claim. Decode numbers
// into a suitable integer type or json.Number to avoid float64 rounding.
func (i Identity) Claim(name string) (json.RawMessage, bool) {
	v, ok := i.claims[name]
	return append(json.RawMessage(nil), v...), ok
}

type identityKey struct{}

// WithIdentity attaches a successfully validated Identity to a derived context.
// Only trusted application code should use this; a zero value clears identity.
func WithIdentity(ctx context.Context, i Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, i)
}

// FromContext returns the identity installed by the authorization boundary.
// It does not repeat expiry or state checks for long-running work.
func FromContext(ctx context.Context) (Identity, bool) {
	i, ok := ctx.Value(identityKey{}).(Identity)
	return i, ok && i.owner != nil
}

// String deliberately avoids emitting token claims into formatted logs.
func (i Identity) String() string { return "auth.Identity(redacted)" }

// GoString also redacts claims in %#v formatting.
func (i Identity) GoString() string { return i.String() }
