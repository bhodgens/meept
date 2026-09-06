package daemon

import (
	"os"
	"strings"
)

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
// its subprocesses (launchd plists, MCP server launches). Order: the
// inherited PATH first (preserves shell-launched setups), then the
// guaranteed dirs appended if absent: /opt/homebrew/bin, /usr/local/bin,
// $HOME/.local/bin, $HOME/go/bin, $HOME/.cargo/bin, /usr/bin:/bin:/usr/sbin:/sbin.
//
// Pure string assembly over os.Getenv — no filesystem I/O, no exec.LookPath.
// Contract C in docs/plans/20260905-dependency-visibility/master.md;
// closes issue #32.
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

	for _, dir := range strings.Split(os.Getenv("PATH"), ":") {
		add(dir)
	}
	for _, dir := range guaranteedPathDirs() {
		add(dir)
	}
	return strings.Join(dirs, ":")
}
