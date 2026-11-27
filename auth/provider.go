package auth

import (
	"context"
	"crypto"
	"errors"
)

var (
	// ErrInvalidToken denotes malformed, expired or cryptographically invalid credentials.
	ErrInvalidToken = errors.New("invalid access token")
	// ErrUnknownKey denotes an authoritative negative key lookup, not a network failure.
	ErrUnknownKey = errors.New("unknown signing key")
	// ErrRevoked denotes a revoked session, principal, key or stale token revision.
	ErrRevoked = errors.New("access token revoked")
	// ErrPermissionDenied denotes a verified identity without the required permission.
	ErrPermissionDenied = errors.New("permission denied")
	// ErrUnavailable denotes a failed or inconclusive verification dependency.
	ErrUnavailable = errors.New("authorization unavailable")
)

// ValidationMode controls freshness requirements, not signature verification.
type ValidationMode uint8

const (
	// LocalPreferred permits trusted fresh state or authoritative remote validation.
	LocalPreferred ValidationMode = iota
	// RemoteRequired requires an authoritative remote check, not a cached approval.
	RemoteRequired
)

// Valid reports whether the mode is recognized.
func (m ValidationMode) Valid() bool { return m == LocalPreferred || m == RemoteRequired }

// KeyProvider resolves only trusted keys for the configured issuer and algorithm.
// Implementations must be concurrency-safe, honor context, bound their cache and
// refresh unknown keys safely. Keys must not be derived from token-supplied URLs.
// ErrUnknownKey is reserved for definitive absence; otherwise return an error.
type KeyProvider interface {
	ResolveKey(ctx context.Context, issuer, keyID, algorithm string) (crypto.PublicKey, error)
}

// StateValidator checks current session/principal/key status and claims freshness.
// It must be concurrency-safe and honor context. A nil error means the requested
// freshness guarantee was satisfied. Unknown/stale state must not return nil.
// Implementations own revision semantics, sync, cache and remote fallback.
type StateValidator interface {
	Validate(ctx context.Context, identity Identity, mode ValidationMode) error
}
