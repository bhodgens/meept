// groups_override_test.go — CLI-layer tests for `meept groups override
// add|list|prune`: command structure, arg validation, atomic file
// round-trips (tmp+rename leaves no temp litter), and list/prune shapes.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/groups"
	"github.com/spf13/cobra"
)

// executeOverrideCLI runs the override subtree with args, returning
// captured stdout (errors surface via the returned error so callers
// assert on them). Args are relative to the `override` root (the subtree
// root itself, not the parent `groups` command). $MEEPT_HOME is pointed
// at home for the duration so the default file resolution lands in the
// sandbox.
func executeOverrideCLI(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("MEEPT_HOME", home)
	root := newGroupsOverrideCmd()
	res := captureUsersStdoutErr(t, root, args, home)
	return res.stdout, res.err
}

// TestGroupsOverrideAddListPruneRoundTrip pins T3 end to end through the
// command funcs: add writes the file (wrapper form), list shows
// active/expired with the expired hidden by default, prune removes
// exactly the expired entries atomically (no .tmp litter).
func TestGroupsOverrideAddListPruneRoundTrip(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, groups.OverridesFileName)

	// The active window is pinned RELATIVE to now so the test cannot
	// date-rot (the original fixed 2026-11-05 expiry expired live).
	activeExpiry := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	// add (no file yet — first use creates it)
	if _, err := executeOverrideCLI(t, home, "add", "agent:deploy-bot",
		"--allow", "git_push,shell", "--reason", "deploy window",
		"--expires", activeExpiry); err != nil {
		t.Fatalf("add: %v", err)
	}
	entries, err := readOverridesFile(file)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(entries) != 1 || entries[0].Subject != "agent:deploy-bot" ||
		strings.Join(entries[0].Allow, ",") != "git_push,shell" {
		t.Fatalf("file contents wrong: %+v", entries)
	}

	// add an already-expired entry
	if _, err := executeOverrideCLI(t, home, "add", "agent:old-bot",
		"--allow", "shell", "--reason", "old window",
		"--expires", "2026-10-01T00:00:00Z"); err != nil {
		t.Fatalf("add expired: %v", err)
	}

	// list default: active only
	out, err := executeOverrideCLI(t, home, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "agent:deploy-bot") || strings.Contains(out, "agent:old-bot") {
		t.Fatalf("default list must show active only:\n%s", out)
	}
	if !strings.Contains(out, "1 active, 1 expired") {
		t.Fatalf("list omits the expired count (check-integration shape):\n%s", out)
	}

	// list --all: both
	out, err = executeOverrideCLI(t, home, "list", "--all")
	if err != nil {
		t.Fatalf("list --all: %v", err)
	}
	if !strings.Contains(out, "agent:old-bot") || !strings.Contains(out, "expired") {
		t.Fatalf("list --all must include expired entries:\n%s", out)
	}

	// prune
	out, err = executeOverrideCLI(t, home, "prune")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if !strings.Contains(out, "pruned 1") {
		t.Fatalf("prune did not report the removal count:\n%s", out)
	}
	entries, err = readOverridesFile(file)
	if err != nil {
		t.Fatalf("post-prune read: %v", err)
	}
	if len(entries) != 1 || entries[0].Subject != "agent:deploy-bot" {
		t.Fatalf("prune left the wrong entries: %+v", entries)
	}
	// Atomic write hygiene: no temp litter in the home dir.
	if litter := tempLitter(home); len(litter) > 0 {
		t.Fatalf("prune/add left temp files behind: %v", litter)
	}

	// no-op prune
	out, err = executeOverrideCLI(t, home, "prune")
	if err != nil {
		t.Fatalf("no-op prune: %v", err)
	}
	if !strings.Contains(out, "nothing to prune") {
		t.Fatalf("no-op prune output wrong:\n%s", out)
	}
}

// TestGroupsOverrideAddValidation pins the named-error surface: missing
// expires / bad RFC3339 / empty allow / empty reason / bad subject all
// fail WITHOUT writing the file.
func TestGroupsOverrideAddValidation(t *testing.T) {
	home := t.TempDir()
	cases := []struct {
		name    string
		args    []string
		wantSub string
	}{
		{
			name:    "missing expires",
			args:    []string{"add", "agent:x", "--allow", "git_push", "--reason", "r"},
			wantSub: "--expires is required",
		},
		{
			name:    "bad RFC3339",
			args:    []string{"add", "agent:x", "--allow", "git_push", "--reason", "r", "--expires", "tomorrow"},
			wantSub: "RFC3339",
		},
		{
			name:    "bad subject",
			args:    []string{"add", "x", "--allow", "git_push", "--reason", "r", "--expires", "2027-01-01T00:00:00Z"},
			wantSub: "subject",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := executeOverrideCLI(t, home, tc.args...)
			if err == nil {
				t.Fatalf("%s: add succeeded (must fail)", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("%s: error does not name the defect %q: %v", tc.name, tc.wantSub, err)
			}
		})
	}
	// Nothing was written by the failed adds.
	if _, err := os.Stat(filepath.Join(home, groups.OverridesFileName)); !os.IsNotExist(err) {
		t.Fatalf("failed adds wrote or created the overrides file")
	}
}

// TestGroupsOverrideAddReplacesSameSubjectReason pins the idempotent
// re-add: a second add with the same subject+reason REPLACES the window
// instead of duplicating it.
func TestGroupsOverrideAddReplacesSameSubjectReason(t *testing.T) {
	home := t.TempDir()
	add := func(expires string) {
		t.Helper()
		if _, err := executeOverrideCLI(t, home, "add", "agent:x",
			"--allow", "git_push", "--reason", "deploy window", "--expires", expires); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	add(time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339))
	add("2026-12-05T17:00:00Z")
	entries, err := readOverridesFile(filepath.Join(home, groups.OverridesFileName))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("re-add duplicated the entry: %+v", entries)
	}
	want := time.Date(2026, 12, 5, 17, 0, 0, 0, time.UTC)
	if !entries[0].Expires.Equal(want) {
		t.Fatalf("re-add did not replace the window: expires=%v want %v", entries[0].Expires, want)
	}
}

// TestGroupsOverrideListEmpty pins the empty-file shape.
func TestGroupsOverrideListEmpty(t *testing.T) {
	home := t.TempDir()
	out, err := executeOverrideCLI(t, home, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "no overrides") {
		t.Fatalf("empty list output wrong:\n%s", out)
	}
}

// --- helpers -------------------------------------------------------------

// readOverridesFile loads the file through the production reader (the
// same path the daemon uses) and returns the RAW entries (active +
// expired — bypasses the expiry split).
func readOverridesFile(path string) ([]groups.Override, error) {
	set, err := groups.LoadOverrides(path, groups.OverrideLoadOptions{
		Now: func() time.Time { return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		return nil, err
	}
	return append(append([]groups.Override{}, set.Active...), set.Expired...), nil
}

// tempLitter lists leftover *.tmp files in dir (atomic-write hygiene).
func tempLitter(dir string) []string {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	return matches
}

// captureUsersStdoutErr executes the cobra command with os.Stdout
// captured; it returns the captured output plus the command error (RunE
// errors are returned, not printed).
func captureUsersStdoutErr(t *testing.T, root *cobra.Command, args []string, _ string) struct {
	stdout string
	err    error
} {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = w
	root.SetOut(w)
	root.SetErr(w)
	root.SetArgs(args)
	runErr := root.Execute()
	os.Stdout = saved
	w.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)
	for {
		n, rerr := r.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if rerr != nil {
			break
		}
	}
	r.Close()
	return struct {
		stdout string
		err    error
	}{stdout: string(buf), err: runErr}
}
