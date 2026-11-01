package gatecapture

import (
	"os"
	"path/filepath"
)

// DefaultDBRelPath is the capture DB's path relative to the meept home: it
// lands BESIDE the shadow store (the shadow data dir is ~/.meept/shadow with
// training.db inside; the capture DB is a sibling directory file, not inside
// the shadow data dir, so shadow enablement is not a dependency).
const DefaultDBRelPath = "gatecapture.db"

// DefaultDBPath resolves the capture DB path under the meept home, honoring
// MEEPT_HOME (the same resolution every meept-owned config path uses - a rig
// with MEEPT_HOME set never touches the operator's real ~/.meept).
func DefaultDBPath() string {
	return filepath.Join(meeptHome(), DefaultDBRelPath)
}

// meeptHome resolves the meept home the same way the spill service does:
// $MEEPT_HOME when set, else ~/.meept.
func meeptHome() string {
	if h := os.Getenv("MEEPT_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".meept"
	}
	return filepath.Join(home, ".meept")
}
