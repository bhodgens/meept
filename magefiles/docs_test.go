//go:build mage

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestInsideSeparateCheckout covers the guard that keeps package discovery out
// of nested checkouts such as .claude/worktrees/<agent>/.
func TestInsideSeparateCheckout(t *testing.T) {
	root := t.TempDir()

	mkdir := func(parts ...string) string {
		t.Helper()
		dir := filepath.Join(append([]string{root}, parts...)...)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	touch := func(parts ...string) string {
		t.Helper()
		path := filepath.Join(append([]string{root}, parts...)...)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	// The module root itself always carries go.mod; that is not a nested
	// checkout.
	touch("go.mod")
	plain := mkdir("internal", "config")
	touch("internal", "config", "schema.go")

	// A Claude Code worktree: own .git + go.mod below the module root.
	worktree := mkdir(".claude", "worktrees", "agent-ad0fa834", "internal", "config")
	touch(".claude", "worktrees", "agent-ad0fa834", ".git")
	touch(".claude", "worktrees", "agent-ad0fa834", "go.mod")

	// A second real Go module in the repo.
	secondModule := mkdir("sdk", "go")
	touch("sdk", "go", "go.mod")

	tests := []struct {
		name string
		dir  string
		want bool
	}{
		{"module root", root, false},
		{"real package", plain, false},
		{"claude worktree", worktree, true},
		{"nested go module", secondModule, true},
		{"outside module", t.TempDir(), true},
	}
	for _, tt := range tests {
		if got := insideSeparateCheckout(root, tt.dir); got != tt.want {
			t.Errorf("%s: insideSeparateCheckout(%q) = %v, want %v", tt.name, tt.dir, got, tt.want)
		}
	}
}

func TestPackageDir(t *testing.T) {
	root := "/repo"
	got := packageDir(root, "github.com/caimlas/meept/internal/config")
	if want := filepath.Join(root, "internal", "config"); got != want {
		t.Errorf("packageDir = %q, want %q", got, want)
	}

	// Import paths outside the module keep their own path, which the checkout
	// guard then rejects.
	got = packageDir(root, "github.com/other/mod")
	if want := filepath.Join(root, "github.com", "other", "mod"); got != want {
		t.Errorf("packageDir = %q, want %q", got, want)
	}
}

// TestPruneGeneratedOrphans covers removal of stale generator output (e.g. the
// accidentally committed test.md) while leaving hand-written docs in place.
func TestPruneGeneratedOrphans(t *testing.T) {
	dir := t.TempDir()

	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	write("config.md", gomarkdocHeader+"\n\n# config\n")
	write("index.md", "# Generated Package Documentation\n")
	write("test.md", gomarkdocHeader+"\n\n# config\n")
	// Hand-written doc with no gomarkdoc header: must never be touched.
	write("daemon.md", "# daemon\n\nHand written.\n")
	// Not markdown: must never be touched.
	write("bus-topology.json", "{}")

	orphans, err := generatedOrphans(dir, []string{"config.md", "index.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 || orphans[0] != "test.md" {
		t.Fatalf("generatedOrphans = %v, want [test.md]", orphans)
	}

	removed, err := pruneGeneratedOrphans(dir, []string{"config.md", "index.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "test.md" {
		t.Fatalf("pruneGeneratedOrphans = %v, want [test.md]", removed)
	}

	for _, name := range []string{"config.md", "index.md", "daemon.md", "bus-topology.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s should have been kept: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "test.md")); !os.IsNotExist(err) {
		t.Errorf("test.md should have been pruned, stat err = %v", err)
	}
}
