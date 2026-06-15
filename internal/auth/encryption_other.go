//go:build !darwin && !linux

package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// machineKeyPath is the persisted fallback key location for platforms
// without a hardware UUID source. The file is created on first use with
// 0600 permissions and contains 256 bits of randomness.
func machineKeyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".meept", ".machine-key"), nil
}

// platformMachineID returns a fallback hardware identifier for platforms
// without a dedicated hardware UUID source.
//
//   - If MEEPT_ENCRYPTION_KEY is set, its value is used verbatim.
//   - Otherwise a 256-bit random key is generated once and persisted to
//     ~/.meept/.machine-key with mode 0600. Subsequent calls read the
//     persisted key so encrypted tokens remain decryptable across
//     restarts.
func platformMachineID() (string, error) {
	if envKey := os.Getenv("MEEPT_ENCRYPTION_KEY"); envKey != "" {
		return envKey, nil
	}

	path, err := machineKeyPath()
	if err != nil {
		return "", fmt.Errorf("resolve machine key path: %w", err)
	}

	// Try to load an existing key first.
	if existing, err := os.ReadFile(path); err == nil {
		return string(existing), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read machine key: %w", err)
	}

	// Generate a new random key.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate machine key: %w", err)
	}
	key := hex.EncodeToString(raw)

	// Persist with mode 0600. Create the parent directory first.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create machine key dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
		return "", fmt.Errorf("write machine key: %w", err)
	}
	return key, nil
}

// ensure build succeeds on non-darwin/non-linux platforms
var _ = fmt.Sprintf
