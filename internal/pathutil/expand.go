// Package pathutil provides common path manipulation utilities.
package pathutil

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandPath expands ~ to the home directory and resolves the path.
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if path == "~" {
			return homeDir
		}
		if strings.HasPrefix(path, "~/") {
			return filepath.Join(homeDir, path[2:])
		}
	}
	return filepath.Clean(path)
}

// ExpandTildePath is an alias for ExpandPath for backwards compatibility.
var ExpandTildePath = ExpandPath
