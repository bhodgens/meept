// Package config: central home-directory resolution for meept.
//
//meept:section paths
//meept:desc The meept home directory: ~/.meept by default, MEEPT_HOME env override. All config, state, skills, and agents resolve through this one helper.
package config

import (
	"os"
	"path/filepath"
	"strings"
)

// EnvMeeptHome is the environment variable that overrides the meept home
// directory. When set (non-empty after expansion), every meept component —
// daemon, CLI, TUI, config loaders — resolves the home directory to it.
// When unset, the default is $HOME/.meept.
const EnvMeeptHome = "MEEPT_HOME"

// DefaultHomeRel is the home-relative default directory.
const DefaultHomeRel = ".meept"

// MeeptHome returns the meept home directory: $MEEPT_HOME when set,
// else $HOME/.meept. This is THE single resolution point — consumers must
// call this instead of joining ".meept" (or "~/.meept") themselves, so an
// override applies consistently across daemon, CLI, TUI, and tooling.
//
// Falls back to ".meept" relative to CWD only if HOME is unset (a
// pathological environment; meept treats CWD as the data root rather than
// failing).
func MeeptHome() string {
	if override := os.Getenv(EnvMeeptHome); override != "" {
		return expandTilde(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return DefaultHomeRel
	}
	return filepath.Join(home, DefaultHomeRel)
}

// MeeptPath joins elem onto MeeptHome().
func MeeptPath(elem ...string) string {
	return filepath.Join(append([]string{MeeptHome()}, elem...)...)
}

// ExpandMeeptPath resolves a config-supplied path while honoring MEEPT_HOME:
// a leading "~/.meept" prefix (the shipped default for every meept path) is
// redirected under MeeptHome(), so MEEPT_HOME="/x" turns
// "~/.meept/repomap_cache" into "/x/repomap_cache". Any other "~" path
// expands to the user's home directory, and a non-tilde path is returned
// unchanged.
//
// Use this for paths that arrive from configuration. Use MeeptPath() when
// building a meept path from scratch.
func ExpandMeeptPath(path string) string {
	meeptDefault := "~/" + DefaultHomeRel
	if override := os.Getenv(EnvMeeptHome); override != "" {
		override = expandTilde(override)
		if path == meeptDefault {
			return override
		}
		if strings.HasPrefix(path, meeptDefault+"/") {
			return filepath.Join(override, path[len(meeptDefault)+1:])
		}
	}
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if hasTildePrefix(path) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// expandTilde resolves a leading ~ (or ~/) to the user's home directory so
// MEEPT_HOME="~/.meept-test" behaves like the shell would expand it.
func expandTilde(path string) string {
	if path != "~" && !hasTildePrefix(path) {
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

func hasTildePrefix(path string) bool {
	return len(path) >= 2 && path[0] == '~' && path[1] == '/'
}
