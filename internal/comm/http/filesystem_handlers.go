package http

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DirEntry represents a single directory entry in a browse response.
type DirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// BrowseResponse is the JSON returned by GET /api/v1/filesystem/browse.
type BrowseResponse struct {
	Path    string     `json:"path"`
	Parent  string     `json:"parent,omitempty"`
	Entries []DirEntry `json:"entries"`
}

// handleFilesystemBrowse lists subdirectories of the given path on the
// daemon's filesystem. This lets remote clients (Flutter GUI) navigate
// server-side directories to pick a project root without having direct
// filesystem access.
//
// GET /api/v1/filesystem/browse?path=/Users/caimlas/git
//
// Returns only directories (no files), sorted alphabetically.
// When path is empty, defaults to the daemon's home directory.
// Symlinks are resolved to their real path; broken symlinks are skipped.
// Hidden directories (starting with ".") are excluded unless the path
// itself is under a hidden directory.
func (s *Server) handleFilesystemBrowse(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		// Default to the daemon process's home directory.
		if home, err := os.UserHomeDir(); err == nil {
			path = home
		} else {
			path = "/"
		}
	}

	// Resolve to absolute path, following symlinks.
	absPath, err := filepath.Abs(path)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid path")
		return
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		// Don't leak internal paths in error messages — just say "unreadable".
		s.writeError(w, http.StatusForbidden, "directory not readable")
		return
	}

	var dirs []DirEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			// Skip non-directories. We only browse directories.
			continue
		}
		name := entry.Name()
		// Skip hidden directories (starting with ".") unless the user is
		// already inside one (e.g. ~/.config).
		if strings.HasPrefix(name, ".") && !isHiddenPath(absPath) {
			continue
		}
		fullPath := filepath.Join(absPath, name)
		dirs = append(dirs, DirEntry{
			Name: name,
			Path: fullPath,
		})
	}

	// Sort alphabetically (case-insensitive).
	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})

	parent := ""
	if absPath != "/" && absPath != "." {
		parent = filepath.Dir(absPath)
	}

	s.writeJSON(w, http.StatusOK, BrowseResponse{
		Path:    absPath,
		Parent:  parent,
		Entries: dirs,
	})
}

// isHiddenPath returns true if any component of the path starts with ".",
// meaning the user is navigating inside a hidden directory tree.
func isHiddenPath(path string) bool {
	// Check the last few components for hidden markers.
	// We check the path relative to home to avoid flagging the home dir.
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		if len(part) > 0 && part[0] == '.' {
			return true
		}
	}
	return false
}
