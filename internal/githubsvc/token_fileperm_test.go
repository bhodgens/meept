package githubsvc

// bughunt 2026-10-30 M1: FileTokenSource must verify the mode on the SAME
// fd it reads (open+fstat+read) and refuse group/other access bits at read
// time, not only at wiring time.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileTokenSource_RefusesGroupReadableAtReadTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("ghp_real\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	_, err := FileTokenSource{Path: path}.Token(context.Background())
	if !errors.Is(err, ErrNoToken) {
		t.Fatalf("group-readable token file: want ErrNoToken, got %v", err)
	}
}

func TestFileTokenSource_OwnerOnlyResolves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("  ghp_real  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tok, err := FileTokenSource{Path: path}.Token(context.Background())
	if err != nil {
		t.Fatalf("owner-only token file: %v", err)
	}
	if tok != "ghp_real" {
		t.Fatalf("token = %q, want trimmed ghp_real", tok)
	}
}
