package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRotateIfNeeded(t *testing.T) {
	writeFile := func(t *testing.T, path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name       string
		setup      func(t *testing.T, dir string) (path string, maxBytes int64)
		wantRotate bool
		wantErr    bool
	}{
		{
			name: "rotates when over cap",
			setup: func(t *testing.T, dir string) (string, int64) {
				path := filepath.Join(dir, "log.jsonl")
				writeFile(t, path, "old\n")
				return path, 3 // "old\n" is 4 bytes
			},
			wantRotate: true,
		},
		{
			name: "no rotation at or under cap",
			setup: func(t *testing.T, dir string) (string, int64) {
				path := filepath.Join(dir, "log.jsonl")
				writeFile(t, path, "old\n")
				return path, 4 // exactly cap size → no rotation
			},
			wantRotate: false,
		},
		{
			name: "missing file is not an error",
			setup: func(t *testing.T, dir string) (string, int64) {
				return filepath.Join(dir, "absent.jsonl"), 10
			},
			wantRotate: false,
		},
		{
			name: "non-positive cap never rotates",
			setup: func(t *testing.T, dir string) (string, int64) {
				path := filepath.Join(dir, "log.jsonl")
				writeFile(t, path, "big\n")
				return path, 0
			},
			wantRotate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path, maxBytes := tt.setup(t, dir)

			err := RotateIfNeeded(path, maxBytes)
			if (err != nil) != tt.wantErr {
				t.Fatalf("RotateIfNeeded() error = %v, wantErr %v", err, tt.wantErr)
			}

			_, statErr := os.Stat(path + ".1")
			rotated := statErr == nil
			if rotated != tt.wantRotate {
				t.Fatalf("rotated = %v, want %v (stat err %v)", rotated, tt.wantRotate, statErr)
			}
		})
	}
}

func TestRotateIfNeededOverwritesPreviousGeneration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")

	// Generation 1: over the cap → rotates, .1 holds gen-1 content.
	if err := os.WriteFile(path, []byte("gen-one-old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RotateIfNeeded(path, 4); err != nil {
		t.Fatal(err)
	}

	// The live file is gone (rotated); the next append recreates it with
	// newer, smaller content, which then over-rotates and must clobber the
	// previous .1 (exactly one generation retained).
	if err := os.WriteFile(path, []byte("gen-two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RotateIfNeeded(path, 4); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "gen-two\n" {
		t.Fatalf(".1 = %q, want %q (old generation must be overwritten)", got, "gen-two\n")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("live file should be gone after rotation, stat err = %v", err)
	}
}

func TestRotateIfNeededIsAtomicRename(t *testing.T) {
	// Rename(2) on the same filesystem is atomic: verify the helper uses a
	// rename by asserting that mid-rotation there is never a copy window —
	// i.e. after RotateIfNeeded returns, either the file moved whole or not
	// at all. We approximate by checking content integrity of the rotated
	// file across a large payload.
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")

	payload := make([]byte, 64*1024)
	for i := range payload {
		payload[i] = byte('a' + i%26)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RotateIfNeeded(path, 1024); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatal("rotated content does not match original byte-for-byte")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("original path should not remain after rename, stat err = %v", err)
	}
}
