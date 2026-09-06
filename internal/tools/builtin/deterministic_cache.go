package builtin

import (
	"crypto/sha1" //nolint:gosec // non-security use: cache address derived from URL per the meept-bench capture format
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrCacheMiss is the sentinel returned when deterministic (cached-fetch)
// mode is enabled and the requested URL has no captured fixture. The
// wrapping tool converts it to an error message of the form
// "cache-miss: <url> not captured" and never touches the network.
// It is deliberately a plain error: errcls.IsRetryable must classify it
// as non-retryable so the agent's backoff does not spin on a missing
// fixture.
var ErrCacheMiss = errors.New("cache-miss")

// CacheEntry is the on-disk fixture format shared with meept-bench's
// `capture` subcommand: <cache-dir>/<sha1(url)>.json containing
// {"url","fetched_at","body"}. The URL field is verified on lookup so a
// hash collision (or a hand-edited file) cannot serve the wrong page.
type CacheEntry struct {
	URL       string    `json:"url"`
	FetchedAt time.Time `json:"fetched_at"`
	Body      string    `json:"body"`
}

// DeterministicCacheDir returns the directory cached fetch fixtures are
// served from: $MEEPT_TOOL_CACHE_DIR when set, else ~/.meept/tool-cache.
// The directory is not created here; Lookup simply reports a miss for a
// missing directory.
func DeterministicCacheDir() string {
	if dir := os.Getenv("MEEPT_TOOL_CACHE_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "meept", "tool-cache")
	}
	return filepath.Join(home, ".meept", "tool-cache")
}

// DeterministicToolsEnvEnabled reports whether the process-level override
// MEEPT_DETERMINISTIC_TOOLS is truthy. It ORs with the config gate so a
// benchmark run can flip the mode via the daemon environment without a
// config-file edit.
func DeterministicToolsEnvEnabled() bool {
	switch os.Getenv("MEEPT_DETERMINISTIC_TOOLS") {
	case "1", "true", "TRUE", "True", "yes", "on":
		return true
	default:
		return false
	}
}

// CacheHashURL returns the hex sha1 of url — the fixture file stem shared
// by this package's Lookup and meept-bench's capture/fixtures tooling.
func CacheHashURL(url string) string {
	sum := sha1.Sum([]byte(url)) //nolint:gosec // see ErrCacheMiss comment: cache addressing, not security
	return hex.EncodeToString(sum[:])
}

// CacheEntryPath returns the fixture path for url under dir.
func CacheEntryPath(dir, url string) string {
	return filepath.Join(dir, CacheHashURL(url)+".json")
}

// LookupCacheEntry reads the fixture for url from dir. found is false
// when no fixture exists (the caller must fail with ErrCacheMiss, without
// any network access). A present-but-unreadable or corrupt fixture is an
// error, also surfaced without network. The stored URL is verified so a
// sha1 collision or mis-placed file cannot serve a different page.
func LookupCacheEntry(dir, url string) (CacheEntry, bool, error) {
	data, err := os.ReadFile(CacheEntryPath(dir, url))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CacheEntry{}, false, nil
		}
		return CacheEntry{}, false, fmt.Errorf("cache-read: %w", err)
	}
	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return CacheEntry{}, false, fmt.Errorf("cache-corrupt %s: %w", dir, err)
	}
	if entry.URL != url {
		return CacheEntry{}, false, nil
	}
	return entry, true, nil
}

// SaveCacheEntry writes a fixture for url under dir, creating dir as
// needed. Used by tests and available to capture-style tooling; the
// canonical capture implementation lives in meept-bench.
func SaveCacheEntry(dir, url, body string, fetchedAt time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(CacheEntry{URL: url, FetchedAt: fetchedAt.UTC(), Body: body}, "", "  ")
	if err != nil {
		return "", err
	}
	path := CacheEntryPath(dir, url)
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
