package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestMain isolates the package from the operator's live ~/.meept, then chdirs
// to the module root: NewComponents resolves config/models.json5 relative to the
// working directory, and `go test` runs with the package directory as CWD. Fresh
// machines without a live ~/.meept (CI) then fail with "models.json5 not found".
//
// The MEEPT_HOME pin is not cosmetic. internal/daemon/daemon_test.go builds a
// Config with StateDir set but leaves the nested Daemon.DataDir EMPTY, so
// components.go (queueDB / tasksDB := filepath.Join(cfg.Daemon.DataDir, ...))
// resolves those paths against the loaded operator config — i.e. the real
// ~/.meept/tasks.db. With a large live task DB, ResetStaleClaimsAtStartup then
// sweeps millions of rows before Run() reaches models.StatusRunning
// (daemon.go:2031), so TestDaemonStartup observed status "starting" and gave up
// after its 10x50ms wait. Measured on 2026-10-09 with a 513MB tasks.db: the test
// took 19s and FAILED; with an isolated MEEPT_HOME it takes 0.30s and PASSES.
// The defect is that the test read operator state, not that it was slow.
func TestMain(m *testing.M) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	if err := os.Chdir(root); err != nil {
		panic(err)
	}
	// Pin MEEPT_HOME for every test in this package unless the caller already
	// chose one (CI may point at a fixture home deliberately). t.TempDir() is not
	// usable here (no *testing.T), so use the OS temp root with a stable name.
	if os.Getenv("MEEPT_HOME") == "" {
		if err := os.Setenv("MEEPT_HOME", filepath.Join(os.TempDir(), "meept-daemon-testhome")); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}
