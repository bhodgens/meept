// Package pathutil provides common path manipulation utilities.
package pathutil

import (
	"os"
	"path/filepath"
	"strings"
)

// EnvMeeptHome is the environment variable that relocates the meept home
// directory. It mirrors internal/config.EnvMeeptHome; pathutil cannot import
// internal/config (config imports pathutil), so the name is repeated here
// deliberately. Keep the two in lockstep.
const EnvMeeptHome = "MEEPT_HOME"

// meeptHomeRel is the shipped meept home prefix carried by config-supplied
// paths (the default "~/.meept"). It mirrors internal/config.DefaultHomeRel for
// the same import-cycle reason.
const meeptHomeRel = ".meept"

// ExpandPath expands ~ to the home directory and resolves the path.
//
// It always means the REAL user home: it never consults MEEPT_HOME. Callers
// that genuinely mean the operator's home (a path meept does not own) keep that
// exact meaning. Callers resolving a meept-supplied path (the shipped
// "~/.meept/..." defaults) must use ExpandMeeptPath instead, so a rig started
// with MEEPT_HOME does not read or write the operator's real ~/.meept.
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

// ExpandMeeptPath expands a path that arrives from MEEPT configuration,
// honoring MEEPT_HOME: a leading "~/.meept" prefix (the shipped default for
// every meept path) is redirected under $MEEPT_HOME when it is set. A rig with
// MEEPT_HOME=/x therefore resolves the shipped pid_file
// "~/.meept/run/llama.pid" to "/x/run/llama.pid", so the runtime's pid file,
// its durable spawn record and the daemon's run-dir sweep scan all land in ONE
// directory (before this the pid file/record used the operator's real home
// while the sweep scanned $MEEPT_HOME/run, so the record half of the sweep
// never fired in a rig).
//
// A "~" path WITHOUT the meept prefix expands to the real home, and any other
// path is returned unchanged. When MEEPT_HOME is unset or blank this is
// byte-identical to ExpandPath, so the default install is unaffected.
func ExpandMeeptPath(path string) string {
	if override := os.Getenv(EnvMeeptHome); override != "" {
		override = expandHomeTilde(override)
		meeptDefault := "~/" + meeptHomeRel
		if path == meeptDefault {
			return override
		}
		if strings.HasPrefix(path, meeptDefault+"/") {
			return filepath.Join(override, path[len(meeptDefault)+1:])
		}
	}
	return ExpandPath(path)
}

// expandHomeTilde resolves a leading ~ or ~/ in a MEEPT_HOME override so
// MEEPT_HOME="~/.meept-rig" behaves like the shell would expand it. It never
// consults MEEPT_HOME (that would recurse).
func expandHomeTilde(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
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

// ExpandTildePath is an alias for ExpandPath for backwards compatibility.
var ExpandTildePath = ExpandPath
