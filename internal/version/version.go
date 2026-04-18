// Package version holds build-time version information for meept.
// The variables below are populated at build time via ldflags.
package version

var (
	// Version is the git-derived version string (e.g., "v20260228" or "v1.2.3")
	Version = "dev"

	// Commit is the short git commit hash
	Commit = "unknown"

	// BuildTime is the UTC build timestamp
	BuildTime = "unknown"
)

// String returns the version string for display.
func String() string {
	if Version == "dev" {
		return Version
	}
	return Version
}
