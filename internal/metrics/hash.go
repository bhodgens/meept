// Salt management for dispatch_log input hashing (classifier-observability
// design S4): a per-install random salt + salt id, generated once, stored
// next to metrics.db with 0600 perms. The salt ID is mixed into every
// input_hash (see HashInput) so cross-install dictionary attacks fail;
// the 32 salt bytes are generated and rotated alongside the id but are
// not currently read by the derivation.
package metrics

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const (
	// classifierSaltFile holds 32 random bytes; classifierSaltIDFile holds
	// the 16-hex-char salt id. Both live in the meept config directory
	// (next to metrics.db) and are created on first use.
	classifierSaltFile   = "classifier_salt"
	classifierSaltIDFile = "classifier_salt_id"

	// classifierSaltLen is the salt byte length (design S4: 32 random
	// bytes). classifierSaltIDHexLen is the salt id length in hex chars
	// (8 random bytes, hex-encoded).
	classifierSaltLen      = 32
	classifierSaltIDHexLen = 16
)

// HashInput derives the dispatch input hash persisted in
// dispatch_log.input_hash (design S4):
//
//	hex(SHA-256(saltID || 0x00 || message))[:16]
//
// saltID is the per-install 16-hex-char id from LoadOrCreateSalt; it is
// the ENTIRE hash key. The raw salt bytes are neither read nor mixed
// here: identical prompts on the same install (or after rotating the
// salt file without changing saltID) deliberately collide, which is
// what cross-install dedup/join relies on. The 32 salt bytes are held
// for a future keyed derivation — rotating them does NOT invalidate
// historical hashes — and the `salt` parameter is accepted only so
// every call site already holds the material a future versioned
// derivation would need. Changing the derivation must version the hash
// prefix (new saltID), never silently re-key existing rows. Storing the
// first 16 hex chars is sufficient for dedup/join at campaign corpus
// sizes.
func HashInput(saltID string, salt []byte, message string) string {
	h := sha256.New()
	h.Write([]byte(saltID))
	h.Write([]byte{0x00})
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))[:classifierSaltIDHexLen]
}

// LoadOrCreateSalt returns the per-install classifier salt id and salt,
// creating both on first use. Files live in configDir:
//
//	classifier_salt     32 random bytes, 0600
//	classifier_salt_id  16 hex chars, 0600
//
// Subsequent calls read the existing pair. A corrupt or wrong-length file
// regenerates the whole pair (a mismatched id/salt combination would
// silently corrupt future hash comparisons); regeneration is logged as a
// warning. A missing pair is the normal first-run path and is silent.
func LoadOrCreateSalt(configDir string) (saltID string, salt []byte, err error) {
	if configDir == "" {
		return "", nil, fmt.Errorf("config dir is empty")
	}
	saltPath := filepath.Join(configDir, classifierSaltFile)
	idPath := filepath.Join(configDir, classifierSaltIDFile)

	saltBytes, saltErr := os.ReadFile(saltPath)
	idBytes, idErr := os.ReadFile(idPath)
	if saltErr == nil && idErr == nil && len(saltBytes) == classifierSaltLen {
		id := strings.TrimSpace(string(idBytes))
		if _, hexErr := hex.DecodeString(id); hexErr == nil && len(id) == classifierSaltIDHexLen {
			return id, saltBytes, nil
		}
		slog.Warn("classifier salt id file invalid; regenerating salt pair", "path", idPath)
	} else {
		if saltErr != nil && !os.IsNotExist(saltErr) {
			slog.Warn("classifier salt file unreadable; regenerating salt pair",
				"path", saltPath, "error", saltErr)
		}
		if saltErr == nil && len(saltBytes) != classifierSaltLen {
			slog.Warn("classifier salt file corrupt or short; regenerating salt pair",
				"path", saltPath, "got_bytes", len(saltBytes), "want_bytes", classifierSaltLen)
		}
		if idErr != nil && !os.IsNotExist(idErr) {
			slog.Warn("classifier salt id file unreadable; regenerating salt pair",
				"path", idPath, "error", idErr)
		}
	}

	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return "", nil, fmt.Errorf("failed to create config dir %s: %w", configDir, err)
	}

	salt = make([]byte, classifierSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", nil, fmt.Errorf("failed to generate classifier salt: %w", err)
	}
	idRaw := make([]byte, classifierSaltIDHexLen/2)
	if _, err := rand.Read(idRaw); err != nil {
		return "", nil, fmt.Errorf("failed to generate classifier salt id: %w", err)
	}
	saltID = hex.EncodeToString(idRaw)

	if err := os.WriteFile(saltPath, salt, 0o600); err != nil {
		return "", nil, fmt.Errorf("failed to write %s: %w", saltPath, err)
	}
	if err := os.WriteFile(idPath, []byte(saltID), 0o600); err != nil {
		return "", nil, fmt.Errorf("failed to write %s: %w", idPath, err)
	}
	return saltID, salt, nil
}
