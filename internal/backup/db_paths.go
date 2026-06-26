package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"log/slog"
)

// GetLocalDBPaths returns paths to SQLite databases that should be backed up
// from the given data directory.
//
// Migration handling:
// - If sessions.db exists (legacy name), it is returned instead of local.db
// - memory.db is included if it exists
//
// The function logs a warning when it finds a legacy sessions.db.
func GetLocalDBPaths(dataDir string) ([]string, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("backup: data directory is empty")
	}

	// Ensure data directory exists
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("backup: data directory %s does not exist", dataDir)
	}

	var paths []string

	// Check for local.db (preferred) or sessions.db (legacy)
	localDB := filepath.Join(dataDir, "local.db")
	if _, err := os.Stat(localDB); err == nil {
		paths = append(paths, localDB)
	} else {
		// Look for legacy sessions.db
		sessionsDB := filepath.Join(dataDir, "sessions.db")
		if _, err := os.Stat(sessionsDB); err == nil {
			slog.Warn("backup: using legacy sessions.db (consider migrating to local.db)",
				"path", sessionsDB)
			paths = append(paths, sessionsDB)
		} else {
			slog.Debug("backup: no local.db or sessions.db found in data dir",
				"data_dir", dataDir)
		}
	}

	// Check for memory.db (if stored separately)
	memoryDB := filepath.Join(dataDir, "memory.db")
	if _, err := os.Stat(memoryDB); err == nil {
		paths = append(paths, memoryDB)
	}

	if len(paths) == 0 {
		return nil, ErrNoDatabases
	}

	return paths, nil
}

// GetLocalDBPath returns a single database path for backup, preferring local.db
// over sessions.db (legacy), with no memory.db (the primary store).
func GetLocalDBPath(dataDir string) (string, []string, error) {
	paths, err := GetLocalDBPaths(dataDir)
	if err != nil {
		return "", nil, err
	}

	// If only one path, return it directly
	if len(paths) == 1 {
		return paths[0], nil, nil
	}

	// Return the first (primary) path and the rest as additional
	return paths[0], paths[1:], nil
}
