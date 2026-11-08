//go:build e2e

package clusterreclaim

import (
	"os"
	"path/filepath"
)

// writeFile creates parent dirs and writes content - the config-hook
// helper (WithConfigHook runs after dirs exist; cluster/ does not).
func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}
