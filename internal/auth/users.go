// Package auth provides the multi-user authentication model for meept:
// users, API keys with optional expiry, a persistent JSON5-backed store,
// cluster-pooled ("foreign") user merging, and no-op quota/permission stubs.
//
// Multi-user access is opt-in (multiuser.enabled defaults to false); when
// disabled, callers never touch this package and the legacy single-key path
// behaves identically to before.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Sentinel validation outcomes. The HTTP middleware maps these to distinct
// 418 messages; the store wraps them with %w.
var (
	// ErrUnknownKey is returned when the presented key matches no stored key.
	ErrUnknownKey = errors.New("unknown key")
	// ErrExpiredKey is returned when the presented key matches but has passed
	// its ExpiresAt instant.
	ErrExpiredKey = errors.New("key expired")
	// ErrEmptyKey is returned when the presented key is empty.
	ErrEmptyKey = errors.New("empty key")
)

// rawKeyLen is the number of random bytes in a generated API key
// (hex-encoded, yielding 64 characters).
const rawKeyLen = 32

// Key roles ([acp] approval gating, audit M5 Phase 2b). A key's role
// limits what authenticated surfaces it may answer.
const (
	// KeyRoleHuman keys may answer ACP permission requests (the human
	// gate). Empty string on stored keys means the same for backward
	// compatibility: every key minted before roles existed was, and stays,
	// human-capable — fail-open on migration, no operator lockout.
	KeyRoleHuman = "human"
	// KeyRoleAPI keys are for programmatic consumers (automation, other
	// services). They authenticate everywhere ordinary keys do, but are
	// refused on the human permission gate: an issued service key cannot
	// answer a pending ACP permission request even if leaked to an agent.
	KeyRoleAPI = "api"
)

// IsValidKeyRole reports whether role is an accepted key role (including
// the empty legacy value).
func IsValidKeyRole(role string) bool {
	switch role {
	case "", KeyRoleHuman, KeyRoleAPI:
		return true
	}
	return false
}

// KeyRoleIsHuman reports whether a key role may answer the human
// permission gate. Empty (legacy) and "human" do; anything else does not.
func KeyRoleIsHuman(role string) bool {
	return role == "" || role == KeyRoleHuman
}

// Key is a single API key belonging to a user. Only the sha256 hash of the
// raw key material is ever persisted; the raw key is returned once at
// creation time and never stored.
type Key struct {
	ID        string     `json:"id"`   // stable key id
	Hash      string     `json:"hash"` // sha256 hex of raw key
	Label     string     `json:"label,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"` // nil = never
	// Role gates privileged surfaces ("human" | "api"; empty = human,
	// backward compatible — see KeyRoleHuman). Enforced today on
	// acp.permissions.resolve; additive JSON, old stores load unchanged.
	Role string `json:"role,omitempty"`
}

// User is an account that may hold multiple API keys.
type User struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Keys       []Key  `json:"keys"`
	OriginNode string `json:"origin_node,omitempty"` // empty = local
}

// Identity is the authenticated principal attached to a request after a
// presented API key has been validated against the store.
type Identity struct {
	UserID   string
	UserName string
	KeyID    string
	// Role is the presented key's role ("human" | "api" | "" legacy =
	// human). Privileged surfaces gate on it (acp.permissions.resolve);
	// see KeyRoleHuman.
	Role string
}

// hashKey returns the sha256 hex digest of a raw API key string.
func hashKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// generateRawKey produces a new raw API key (32 random bytes, hex-encoded)
// together with its sha256 hex hash. The raw value is surfaced to the user
// exactly once at creation; only the hash is persisted.
func generateRawKey() (raw string, hash string, err error) {
	b := make([]byte, rawKeyLen)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate api key: %w", err)
	}
	raw = hex.EncodeToString(b)
	return raw, hashKey(raw), nil
}

// copyKey returns a deep copy of k, isolating the expiry pointer.
func copyKey(k Key) Key {
	c := k
	if k.ExpiresAt != nil {
		t := *k.ExpiresAt
		c.ExpiresAt = &t
	}
	return c
}

// copyUser returns a deep copy of u, isolating the keys slice and every
// expiry pointer so callers cannot mutate store-owned state.
func copyUser(u User) User {
	c := u
	if len(u.Keys) > 0 {
		c.Keys = make([]Key, len(u.Keys))
		for i, k := range u.Keys {
			c.Keys[i] = copyKey(k)
		}
	}
	return c
}

// copyUsers returns a deep copy of a user slice.
func copyUsers(users []User) []User {
	out := make([]User, len(users))
	for i, u := range users {
		out[i] = copyUser(u)
	}
	return out
}
