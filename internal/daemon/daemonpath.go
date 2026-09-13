package daemon

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/config"
)

// meeptPrefixPathDirs returns the meept-managed dependency bin directories,
// in precedence order. DaemonPath PREPENDS these to PATH so a tool meept
// installed into its own dependency prefix always beats a same-named build
// from Homebrew or /usr/local.
//
// $MEEPT_HOME/deps is the meept dependency prefix (Makefile MEEPT_DEPS) and
// llama.cpp lives at $MEEPT_HOME/deps/llama.cpp. Both layouts that prefix can
// hold are searched, best first:
//
//	bin/        installed layout (scripts/install-llama-cpp.sh: prebuilt
//	            release tarball or `cmake --install`)
//	build/bin/  in-tree CMake build layout (LLAMA_CPP_METHOD=source, manual builds)
//
// The build floor for that binary is enforced by `make deps-llama-check` (the
// LFM2.5 tool-call parser is missing from older builds, which then answer in
// prose instead of calling tools). If the directories do not exist, PATH
// resolution simply falls through to the next entry -- no filesystem check is
// performed here.
func meeptPrefixPathDirs() []string {
	prefix := config.MeeptPath("deps", "llama.cpp")
	return []string{
		filepath.Join(prefix, "bin"),
		filepath.Join(prefix, "build", "bin"),
	}
}

// guaranteedPathDirs is the ordered list of directories the daemon
// guarantees on PATH for itself and its subprocesses, after any inherited
// PATH. The system dirs come last so user-writable tool locations win.
// $HOME is expanded per call so the result reflects the current process
// environment (and is testable via t.Setenv).
func guaranteedPathDirs() []string {
	home := os.Getenv("HOME")
	return []string{
		"/opt/homebrew/bin",
		"/usr/local/bin",
		home + "/.local/bin",
		home + "/go/bin",
		home + "/.cargo/bin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	}
}

// DaemonPath returns the PATH value the daemon guarantees for itself and
// its subprocesses (launchd plists, MCP server launches). Order:
//
//  1. the meept dependency prefix bin dirs ($MEEPT_HOME/deps/llama.cpp/bin,
//     then $MEEPT_HOME/deps/llama.cpp/build/bin), so a meept-managed
//     llama-server shadows a Homebrew one -- a Homebrew build predating
//     llama.cpp's June-2026 LFM2.5 tool-call parser makes the agent narrate
//     prose instead of calling tools;
//  2. the inherited PATH (preserves shell-launched setups);
//  3. the guaranteed dirs appended if absent: /opt/homebrew/bin,
//     /usr/local/bin, $HOME/.local/bin, $HOME/go/bin, $HOME/.cargo/bin,
//     /usr/bin:/bin:/usr/sbin:/sbin.
//
// Duplicates are dropped; the first occurrence wins.
//
// Pure string assembly over os.Getenv (config.MeeptHome reads only the
// MEEPT_HOME/HOME env vars) -- no filesystem I/O, no exec.LookPath.
// Contract C in docs/plans/20260905-dependency-visibility/master.md;
// closes issue #32. Mirrored by the macOS menubar's daemonPATH()
// (menubar/MeeptMenuBar/Services/DaemonController.swift) -- keep in sync.
func DaemonPath() string {
	seen := make(map[string]bool)
	var dirs []string
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}

	for _, dir := range meeptPrefixPathDirs() {
		add(dir)
	}
	for _, dir := range strings.Split(os.Getenv("PATH"), ":") {
		add(dir)
	}
	for _, dir := range guaranteedPathDirs() {
		add(dir)
	}
	return strings.Join(dirs, ":")
}
