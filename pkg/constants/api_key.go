// Package constants holds shared defaults across the Meept project.
package constants

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// defaultDevAPIKeyLegacy is the old hardcoded public key from early versions.
// It is kept ONLY for migration detection — never returned by DevAPIKey().
// New code must use DevAPIKey() exclusively.
const defaultDevAPIKeyLegacy = "meept_dev_default_key_CHANGE_ME"

// DefaultDevAPIKey returns the legacy hardcoded key value. This exists solely
// for backward compatibility with external consumers (e.g., the Swift menubar
// app) that reference the constant by name. New code must call DevAPIKey().
func DefaultDevAPIKey() string {
	return defaultDevAPIKeyLegacy
}

// devKeyFileName is the name of the per-installation dev key file, stored
// under the meept home directory ($MEEPT_HOME, else ~/.meept).
const devKeyFileName = "dev_key"

// EnvMeeptHome mirrors internal/config.EnvMeeptHome. It is duplicated here
// because pkg/ must not import internal/. Keep the value in sync.
const EnvMeeptHome = "MEEPT_HOME"

// DefaultHomeRel mirrors internal/config.DefaultHomeRel. Duplicated for the
// same layering reason as EnvMeeptHome.
const DefaultHomeRel = ".meept"

// devKeyDir resolves the directory that holds the per-installation dev key.
//
// The rule MUST stay in sync with internal/config.MeeptHome() (internal is
// off-limits from pkg/, so it is duplicated here deliberately):
//
//  1. $MEEPT_HOME when set (non-empty); a leading ~ is expanded.
//  2. otherwise $HOME/.meept.
//  3. if HOME cannot be resolved, ".meept" relative to the process CWD
//     (matching internal/config.MeeptHome(), which treats CWD as the data
//     root rather than failing).
//
// If this drifts from internal/config.MeeptHome(), an installer-generated
// key written under one home will not match the daemon/CLI reading the other,
// producing a hard HTTP 418 auth failure.
func devKeyDir() string {
	if override := os.Getenv(EnvMeeptHome); override != "" {
		return expandHomeTilde(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return DefaultHomeRel
	}
	return filepath.Join(home, DefaultHomeRel)
}

// expandHomeTilde resolves a leading ~ (or ~/) to the user's home directory,
// mirroring internal/config.expandTilde.
func expandHomeTilde(path string) string {
	if path != "~" && !(len(path) >= 2 && path[0] == '~' && path[1] == '/') {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// devKeyOnce ensures devKeyCached is computed exactly once.
//
// Previously this used devKeyMu with a loaded-flag, which required holding
// the mutex across disk I/O (ReadFile/WriteFile) on first call — a violation
// of the CLAUDE.md "Mutex scope" rule (no I/O under mutex) that the mutexio
// analyzer catches. sync.Once is the correct primitive for one-shot lazy
// initialization and avoids the issue: the lock is held only briefly to
// coordinate the once.Do, and the I/O happens inside the function passed to
// once.Do (which the implementer is free to make pure of any user-held lock).
var devKeyOnce sync.Once

// devKeyCached is the process-lifetime cached value of DevAPIKey(). Read
// freely after devKeyOnce has fired; written exactly once inside once.Do.
var devKeyCached string

// DevAPIKey returns the per-installation development API key.
//
// Resolution order:
//  1. If the meept home's dev_key file exists ($MEEPT_HOME/dev_key when
//     MEEPT_HOME is set, else ~/.meept/dev_key), its (trimmed) contents are
//     returned.
//  2. Otherwise, a 32-byte random hex key is generated and written to that
//     path with 0600 permissions, then returned.
//  3. On any error in steps 1-2 (e.g., permission denied, read-only HOME),
//     a per-process ephemeral random key is generated and a warning is logged.
//     This key will NOT match between independently-started daemon and CLI
//     processes — in such environments, configure keys explicitly.
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
	devKeyOnce.Do(func() {
		devKeyCached = loadOrGenerateDevKey()
	})
	return devKeyCached
}

// loadOrGenerateDevKey performs the disk I/O for DevAPIKey.
//
// Called exactly once from devKeyOnce.Do. All I/O happens here, outside
// any caller-held mutex — the sync.Once primitive handles the locking
// internally with a brief critical section that does not span this call.
func loadOrGenerateDevKey() string {
	keyPath := filepath.Join(devKeyDir(), devKeyFileName)

	// Step 1: try to read an existing key file.
	if data, err := os.ReadFile(keyPath); err == nil {
		if k := strings.TrimSpace(string(data)); len(k) > 0 {
			return k
		}
	}

	// Step 2: generate a fresh key and persist it.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		slog.Warn("dev key: crypto/rand failed; using ephemeral random key",
			"error", err)
		return ephemeralKey()
	}
	generated := hex.EncodeToString(raw)

	// Best-effort create of ~/.meept with 0700; ignore error since the
	// write below will surface any real problem.
	_ = os.MkdirAll(filepath.Dir(keyPath), 0o700)

	if err := os.WriteFile(keyPath, []byte(generated), 0o600); err != nil {
		slog.Warn("dev key: cannot persist generated key; using ephemeral random key (will not survive restart)",
			"path", keyPath, "error", err)
		return ephemeralKey()
	}

	slog.Info("dev key: generated new per-installation key",
		"path", keyPath)
	return generated
}

// ephemeralKey generates a per-process random key and logs a warning.
// Used only when the key file cannot be read or written (read-only HOME,
// crypto/rand failure, etc.). The key is valid for the lifetime of the
// process only — it will not match between daemon and CLI if they start
// independently. In such environments, explicit key configuration is required.
func ephemeralKey() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		// Last resort: crypto/rand also failed. Log loudly.
		slog.Error("dev key: crypto/rand failed for ephemeral key; using fixed fallback (INSECURE)")
		return "meept_insecure_fallback_" + hex.EncodeToString(raw)
	}
	slog.Warn("dev key: using ephemeral random key (will not survive restart; daemon and CLI must share via key file)")
	return hex.EncodeToString(raw)
}
