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
	// The CWD branch is preferred AND resolved to an absolute path: the
	// relative form would be re-resolved against the CWD at read time (F83).
	if !filepath.IsAbs(got) {
		t.Errorf("CWD hit: got %q, want an absolute path", got)
	}
	wantAbs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantAbs {
		t.Errorf("CWD hit: got %q, want %q", got, wantAbs)
	}
	if fi, err := os.Stat(got); err != nil || !fi.IsDir() {
		t.Errorf("resolved %q does not exist as a dir", got)
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

// TestResolveBundledPathNoHitIsAbsolute: when neither candidate exists the
// resolver must still return an ABSOLUTE path. Returning the bare relative
// string let the consumer re-resolve it against the CWD at READ time, which
// is exactly the process-CWD dependence the bundled tier documents away
// (F83, bughunt 2026-09-12 wave).
func TestResolveBundledPathNoHitIsAbsolute(t *testing.T) {
	chdirTemp(t) // empty CWD
	rel := "definitely/not/anywhere/config"
	got := resolveBundledPath(rel)
	if !filepath.IsAbs(got) {
		t.Errorf("no hit: got %q, want an absolute path", got)
	}
	wantAbs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantAbs {
		t.Errorf("no hit: got %q, want the CWD candidate %q", got, wantAbs)
	}
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Errorf("fixture assumption broken: %q unexpectedly exists", got)
	}
}

// TestResolveBundledPathIsStableAcrossChdir pins the real invariant behind
// F83: the tier is resolved ONCE and the result must keep pointing at the same
// directory after the process changes its working directory. A relative result
// (or a CWD-relative read) would silently retarget the tier.
func TestResolveBundledPathIsStableAcrossChdir(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) }) //nolint:errcheck // best-effort restore

	first := t.TempDir()
	if err := os.Chdir(first); err != nil {
		t.Fatal(err)
	}
	rel := "config/prompts"
	if err := os.MkdirAll(rel, 0o755); err != nil {
		t.Fatal(err)
	}
	got := resolveBundledPath(rel)

	// Move somewhere that has no such directory: the resolved tier must not
	// move with it.
	second := t.TempDir()
	if err := os.Chdir(second); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(second, rel)); !os.IsNotExist(err) {
		t.Fatalf("fixture assumption broken: %q exists under the second CWD", rel)
	}
	fi, err := os.Stat(got)
	if err != nil || !fi.IsDir() {
		t.Fatalf("resolved %q no longer resolves to a directory after chdir: %v", got, err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("resolved %q is not absolute; the tier would follow the process CWD", got)
	}
}
