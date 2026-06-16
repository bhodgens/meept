// Package constants holds shared defaults across the Meept project.
package constants

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// DefaultDevAPIKey is the default development API key used when no API keys
// are configured. Both the daemon (server) and the CLI (client) use this value
// so that HTTP transport works out of the box for local development.
//
// In production, always replace this with a generated key via:
//
//	meept token generate --save
//
// This constant is kept as a last-resort fallback for backward compatibility
// and for environments where the per-installation key file cannot be created
// (e.g., read-only HOME). Prefer DevAPIKey() which returns a unique
// per-installation key when available.
const DefaultDevAPIKey = "meept_dev_default_key_CHANGE_ME"

// devKeyFileName is the name of the per-installation dev key file, stored
// under the user's ~/.meept directory.
const devKeyFileName = "dev_key"

// devKeyMu guards devKeyCached during lazy initialization.
var (
	devKeyMu     sync.Mutex
	devKeyCached string
	devKeyLoaded bool
)

// DevAPIKey returns the per-installation development API key.
//
// Resolution order:
//  1. If ~/.meept/dev_key exists, its (trimmed) contents are returned.
//  2. Otherwise, a 32-byte random hex key is generated and written to
//     ~/.meept/dev_key with 0600 permissions, then returned.
//  3. On any error in steps 1-2 (e.g., permission denied, read-only HOME),
//     the legacy DefaultDevAPIKey constant is returned as a fallback and a
//     warning is logged.
//
// Both the daemon (server) and the CLI (client) call this function, so for
// local development — where client and server run on the same machine under
// the same user — both sides resolve to the SAME key by reading the same
// file. This assumption is documented here so it is not accidentally broken
// by changing one call site without the other.
//
// The result is cached for the lifetime of the process; the underlying file
// is only read/written on the first call.
func DevAPIKey() string {
	devKeyMu.Lock()
	defer devKeyMu.Unlock()
	if devKeyLoaded {
		return devKeyCached
	}
	devKeyLoaded = true

	homeDir, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("dev key: cannot resolve home directory; using default constant",
			"error", err)
		devKeyCached = DefaultDevAPIKey
		return devKeyCached
	}

	keyPath := filepath.Join(homeDir, ".meept", devKeyFileName)

	// Step 1: try to read an existing key file.
	if data, err := os.ReadFile(keyPath); err == nil {
		if k := string(data); len(k) > 0 {
			devKeyCached = k
			return devKeyCached
		}
	}

	// Step 2: generate a fresh key and persist it.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		slog.Warn("dev key: crypto/rand failed; using default constant",
			"error", err)
		devKeyCached = DefaultDevAPIKey
		return devKeyCached
	}
	generated := hex.EncodeToString(raw)

	// Best-effort create of ~/.meept with 0700; ignore error since the
	// write below will surface any real problem.
	_ = os.MkdirAll(filepath.Dir(keyPath), 0o700)

	if err := os.WriteFile(keyPath, []byte(generated), 0o600); err != nil {
		slog.Warn("dev key: cannot persist generated key; using default constant",
			"path", keyPath, "error", err)
		devKeyCached = DefaultDevAPIKey
		return devKeyCached
	}

	slog.Info("dev key: generated new per-installation key",
		"path", keyPath)
	devKeyCached = generated
	return devKeyCached
}
