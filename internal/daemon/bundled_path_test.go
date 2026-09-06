package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// chdirTemp changes the process CWD to a temp dir for the test duration.
func chdirTemp(t *testing.T) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) }) //nolint:errcheck // best-effort restore
}

func TestResolveBundledPathEmptyAndAbs(t *testing.T) {
	if got := resolveBundledPath(""); got != "" {
		t.Errorf("empty rel: got %q, want empty", got)
	}
	abs := filepath.Join(string(filepath.Separator), "opt", "config", "agents")
	if got := resolveBundledPath(abs); got != abs {
		t.Errorf("abs rel: got %q, want unchanged", got)
	}
}

func TestResolveBundledPathCWDPreferred(t *testing.T) {
	chdirTemp(t)
	rel := "config/agents"
	if err := os.MkdirAll(rel, 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolveBundledPath(rel)
	// CWD branch returns the ORIGINAL relative path (CWD-first means the
	// relative path already works). Verify it resolves inside the temp CWD.
	if got != rel {
		t.Errorf("CWD hit: got %q, want original rel %q", got, rel)
	}
	abs, err := filepath.Abs(got)
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		t.Errorf("resolved %q does not exist as a dir from CWD", abs)
	}
}

func TestResolveBundledPathExeRelativeFallback(t *testing.T) {
	chdirTemp(t) // empty CWD: nothing resolves from it
	rel := "resolve_exe_test_config/agents"
	exe, err := os.Executable()
	if err != nil {
		t.Skip("no executable path")
	}
	planted := filepath.Join(filepath.Dir(exe), rel)
	if err := os.MkdirAll(planted, 0o755); err != nil {
		t.Skipf("cannot plant beside test binary: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(filepath.Join(filepath.Dir(exe), "resolve_exe_test_config")) //nolint:errcheck
	})

	got := resolveBundledPath(rel)
	if got != planted {
		t.Errorf("exe fallback: got %q, want %q", got, planted)
	}
}

func TestResolveBundledPathNoHitReturnsRel(t *testing.T) {
	chdirTemp(t) // empty CWD
	rel := "definitely/not/anywhere/config"
	if got := resolveBundledPath(rel); got != rel {
		t.Errorf("no hit: got %q, want original rel %q", got, rel)
	}
}
