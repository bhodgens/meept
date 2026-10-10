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
	// HOME must be pinned too, and for the same reason. skills.AutoDiscoverHermes
	// defaults to true (internal/config/schema.go:2343) and discovery.go:44
	// derives the source dir as filepath.Join(homeDir, ".hermes", "skills") — from
	// HOME, not MEEPT_HOME. So a daemon booted by this package walked the
	// OPERATOR's real ~/.hermes/skills (83 entries on this machine) on every run.
	// Under `go test -p 2` that scan is enough to push TestDaemonStartup past its
	// 10x50ms wait for status "running": measured 1.11s failing under load versus
	// 0.26s passing in isolation, with the only log lines in the window being
	// "Skill file has no frontmatter" from ~/.hermes/skills.
	//
	// This is the same defect class as the MEEPT_HOME pin above: a test that
	// reads operator state instead of fixture state. Only the operator's HOME is
	// redirected, never their real home directory contents, and an explicit HOME
	// still wins so CI can point at a fixture.
	if os.Getenv("MEEPT_TEST_PINNED_HOME") == "" {
		fakeHome := filepath.Join(os.TempDir(), "meept-daemon-testhome")
		if err := os.MkdirAll(filepath.Join(fakeHome, ".hermes"), 0o700); err != nil {
			panic(err)
		}
		if err := os.Setenv("HOME", fakeHome); err != nil {
			panic(err)
		}
		// Go caches its own home lookups; clear them so the new HOME takes effect.
		os.Unsetenv("USERPROFILE") // windows equivalent, ignored on darwin
		_ = os.Setenv("MEEPT_TEST_PINNED_HOME", "1")
	}
	os.Exit(m.Run())
}
