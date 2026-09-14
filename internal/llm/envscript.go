package llm

// envScriptResolver resolves ${VAR} references during config expansion.
//
// Resolution chain (in order):
//  1. The daemon's own process environment (existing behavior — whoever
//     started the daemon wins, unchanged for shell/launchd starts).
//  2. The executable script named `env` in the meept home ($MEEPT_HOME, else
//     ~/.meept). The daemon invokes it with ONE argument — the variable name —
//     and reads the value from stdout. Exit 0 = found, exit nonzero = not
//     found, >5s = not found with a warning.
//  3. Not found: empty value plus the existing loud diagnostic naming the
//     variable (never the value).
//
// Security properties:
//   - The script is user-owned local code, executed only when the process
//     environment lacks the variable, and only for variables the config
//     actually references. Values are never logged; diagnostics name
//     variables only.
//   - The script is NEVER synced by config sync (see internal/config/merger.go
//     EnvScriptName): a remotely-pushed script executed at daemon boot would
//     be remote code execution.
//
// A per-boot cache memoizes each variable's resolution, so a variable asked
// twice (e.g. by two config loads) runs the script once. Cache is never used
// when the process environment has the variable — env wins immediately.

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EnvScriptName is the literal filename of the env resolver script, directly
// under the meept home. It is an EXECUTABLE script, not a static env file.
const EnvScriptName = "env"

// EnvScriptTimeout bounds a single env-script invocation. A hung script must
// never hang daemon boot: on timeout the variable counts as not-found and a
// warning names the variable (never any value).
const EnvScriptTimeout = 5 * time.Second

// envScriptResolver consults the process env, then the meept-home env script.
type envScriptResolver struct {
	// homeDir is the meept home the script lives in. Empty = no script.
	homeDir string
	// scriptPath is homeDir/env when it exists and is executable.
	scriptPath string

	// per-boot memoization: one script run per variable name max.
	mu    sync.Mutex
	cache map[string]string
}

// newEnvScriptResolver inspects the meept home for an executable `env` script.
// It warns (never errors — a missing script is the normal case) when the
// script exists but is not executable or has permissions wider than 0700.
// The meept home is resolved the same way internal/llm resolves models.json5
// (mirroring config.MeeptHome locally; internal/llm cannot import
// internal/config — cycle via tools/mcp).
func newEnvScriptResolver() *envScriptResolver {
	r := &envScriptResolver{cache: make(map[string]string)}

	home := strings.TrimSpace(os.Getenv("MEEPT_HOME"))
	if strings.HasPrefix(home, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, home[2:])
		}
	}
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return r
		}
		home = filepath.Join(h, ".meept")
	}
	r.homeDir = home

	path := filepath.Join(home, EnvScriptName)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return r
	}
	if info.Mode()&0o111 == 0 {
		slog.Warn("env script in meept home is not executable; ignoring it",
			"path", path,
			"hint", "chmod 700 "+EnvScriptName)
		return r
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		slog.Warn("env script permissions are wider than 0700",
			"path", path,
			"mode", perm.String(),
			"hint", "chmod 700 "+EnvScriptName)
	}
	r.scriptPath = path
	return r
}

// Resolve returns the value for varName: process environment first, then the
// env script, then empty (the caller's existing not-found diagnostics handle
// the naming). The result is memoized per variable name.
func (r *envScriptResolver) Resolve(varName string) (string, bool) {
	if val, ok := os.LookupEnv(varName); ok {
		return val, true
	}
	if r.scriptPath == "" {
		return "", false
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if val, ok := r.cache[varName]; ok {
		return val, val != ""
	}

	val := r.runScript(varName)
	r.cache[varName] = val
	return val, val != ""
}

// runScript executes the env script with the variable name as its single
// argument and returns the stdout value (one trailing newline trimmed).
// Nonzero exit, timeout, or execution failure all count as not-found;
// warnings name the variable only — values are never logged.
func (r *envScriptResolver) runScript(varName string) string {
	ctx, cancel := context.WithTimeout(context.Background(), EnvScriptTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.scriptPath, varName)
	// CommandContext kills the process when the deadline passes, but it kills
	// only the direct child (`/bin/sh script`) — an `sh -c` style script that
	// spawned its own long-running child (sleep, a profile that starts an
	// agent) can keep the stdout/stderr PIPE WRITERS open, and Wait would
	// block on the pipe read until THAT child exits. WaitDelay bounds the
	// post-kill I/O wait so a hung grandchild cannot hang boot.
	cmd.WaitDelay = EnvScriptTimeout
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// The script inherits the daemon's environment; it may also see MEEPT_HOME
	// so it can locate sibling assets. No new privilege surface: it is
	// user-owned code running as the daemon's OS user.
	cmd.Env = os.Environ()

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		slog.Warn("env script timed out resolving variable; treating as not-found",
			"var", varName,
			"timeout", EnvScriptTimeout.String())
		return ""
	}
	if err != nil {
		// Nonzero exit = not found. That is the normal contract (the script
		// exits nonzero for every variable it does not know), so Debug, not
		// Warn — only the timeout is loud. The variable name may be logged;
		// the value may not.
		slog.Debug("env script did not provide variable",
			"var", varName)
		return ""
	}

	return strings.TrimSuffix(stdout.String(), "\n")
}
