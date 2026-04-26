package llm

import (
	"time"
)

// CacheConfig holds configuration for the token cache.
type CacheConfig struct {
	// L1MaxEntries is the maximum number of entries in the L1 in-memory cache.
	L1MaxEntries int
	// L2Enabled enables the L2 SQLite-backed cache.
	L2Enabled bool
	// L2DBPath is the path to the SQLite database for L2 cache.
	L2DBPath string
	// DefaultTTL is the default time-to-live for cache entries.
	DefaultTTL time.Duration
	// CleanupFreq is the frequency of background cleanup.
	CleanupFreq time.Duration
	// Enabled controls whether caching is active.
	Enabled bool
	// FileAware controls whether file content hashes are included in cache keys.
	FileAware bool
}

// DefaultCacheConfig returns a cache configuration with sensible defaults.
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		L1MaxEntries: 10000,
		L2Enabled:    true,
		DefaultTTL:   30 * time.Minute,
		CleanupFreq:  2 * time.Minute,
		Enabled:      true,
		FileAware:    true,
	}
}
