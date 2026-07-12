package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNextAdapterVersion(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	domain := "code"
	model := "lfm2.5-8b"

	// Empty dir → v1
	ver, err := nextAdapterVersion(tmp, domain, model)
	if err != nil {
		t.Fatalf("nextAdapterVersion empty: %v", err)
	}
	if ver != 1 {
		t.Errorf("empty dir version = %d, want 1", ver)
	}

	// Seed v1 and v3 (gap) → next is 4
	domainDir := filepath.Join(tmp, domain)
	if err := os.MkdirAll(filepath.Join(domainDir, "lfm2.5-8b-v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(domainDir, "lfm2.5-8b-v3"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Unrelated model should not affect versioning.
	if err := os.MkdirAll(filepath.Join(domainDir, "lfm2.5-1.2b-v9"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Non-numeric suffix ignored.
	if err := os.MkdirAll(filepath.Join(domainDir, "lfm2.5-8b-vfoo"), 0o755); err != nil {
		t.Fatal(err)
	}

	ver, err = nextAdapterVersion(tmp, domain, model)
	if err != nil {
		t.Fatalf("nextAdapterVersion with existing: %v", err)
	}
	if ver != 4 {
		t.Errorf("version = %d, want 4", ver)
	}
}
