package compress

import (
	"context"
	"time"
)

// CCRStore is the interface for the Compress-Cache-Retrieve store.
//
// The CCR store provides reversible compression by:
// 1. Storing original content when compressing
// 2. Returning a hash key for retrieval
// 3. Allowing on-demand retrieval of originals
//
// Implementations must be concurrency-safe and support TTL-based expiry.
type CCRStore interface {
	// Store saves a compressed content entry and returns its hash.
	// The hash is derived from the original content (content-addressed).
	// TTL controls when the entry expires (0 = use default TTL).
	Store(ctx context.Context, entry CCREntry) (string, error)

	// Retrieve fetches the full original content by hash.
	// Returns nil if the entry doesn't exist or has expired.
	// Increments the retrieval count for metrics.
	Retrieve(ctx context.Context, hash string) (*CCREntry, error)

	// Search finds content within a compressed entry by query.
	// Used for SmartCrusher results where only某些 items are needed.
	// Returns nil if the entry doesn't exist.
	Search(ctx context.Context, hash, query string) ([]CCRSearchResult, error)

	// Exists checks if an entry exists without loading full content.
	// Returns false if the entry doesn't exist or has expired.
	Exists(ctx context.Context, hash string) bool

	// Delete removes an entry by hash.
	// Returns true if an entry was deleted, false if not found.
	Delete(ctx context.Context, hash string) (bool, error)

	// Stats returns current store statistics.
	// Includes entry count, token totals, and expiry info.
	Stats() CCRStats

	// Close releases resources (database connections, etc.).
	Close() error
}

// CCRStoreConfig configures the CCR store.
type CCRStoreConfig struct {
	// DatabasePath is the SQLite database path.
	// Use "~/.meept/ccr.db" for persistent storage.
	DatabasePath string

	// DefaultTTL is the default time-to-live for entries.
	// Entries are soft-deleted after this duration.
	// Default: 1 hour
	DefaultTTL Duration

	// MaxEntries is the maximum number of entries to retain.
	// When exceeded, oldest entries are evicted.
	// Default: 10000
	MaxEntries int
}

// Duration is a time.Duration with JSON/TOML marshaling.
type Duration struct {
	time.Duration
}

// DefaultCCRStoreConfig returns a CCRStoreConfig with sensible defaults.
func DefaultCCRStoreConfig() CCRStoreConfig {
	return CCRStoreConfig{
		DatabasePath: "~/.meept/ccr.db",
		DefaultTTL:   Duration{Duration: time.Hour},
		MaxEntries:   10000,
	}
}
