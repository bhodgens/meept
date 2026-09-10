package metrics

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHashInput_Deterministic16Hex verifies the S4 contract:
// hex(SHA-256(saltID || 0x00 || message))[:16] - deterministic for the same
// (saltID, salt, message), 16 lowercase hex chars, and salted (different
// salts or messages never collide).
func TestHashInput_Deterministic16Hex(t *testing.T) {
	salt := []byte("0123456789abcdef0123456789abcdef")

	h1 := HashInput("deadbeefdeadbeef", salt, "commit this")
	h2 := HashInput("deadbeefdeadbeef", salt, "commit this")
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %q != %q", h1, h2)
	}
	if len(h1) != 16 {
		t.Fatalf("hash length = %d, want 16", len(h1))
	}
	for _, c := range h1 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("hash %q contains non-lowercase-hex char %q", h1, c)
		}
	}

	// Different salt id -> different hash (the salt id IS the hash key per
	// the contract; the raw salt bytes are held for a future keyed scheme).
	if HashInput("cafebabecafebabe", salt, "commit this") == h1 {
		t.Error("different salt id produced identical hash")
	}
	// Different message -> different hash.
	if HashInput("deadbeefdeadbeef", salt, "run the tests") == h1 {
		t.Error("different message produced identical hash")
	}
}

// TestLoadOrCreateSalt_CreatesAndReloads covers the salt lifecycle: first
// call creates both files with 0600; second call reads back the identical
// pair (no regeneration).
func TestLoadOrCreateSalt_CreatesAndReloads(t *testing.T) {
	dir := t.TempDir()

	id1, salt1, err := LoadOrCreateSalt(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateSalt(first): %v", err)
	}
	if len(salt1) != 32 {
		t.Fatalf("salt length = %d, want 32", len(salt1))
	}
	if len(id1) != 16 {
		t.Fatalf("salt id length = %d, want 16 hex chars", len(id1))
	}
	for _, c := range id1 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("salt id %q is not hex", id1)
		}
	}

	// Files exist with 0600.
	for _, name := range []string{"classifier_salt", "classifier_salt_id"} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s perms = %o, want 600", name, perm)
		}
	}
	saltBytes, err := os.ReadFile(filepath.Join(dir, "classifier_salt"))
	if err != nil {
		t.Fatalf("read salt file: %v", err)
	}
	if string(saltBytes) != string(salt1) {
		t.Error("salt file contents differ from returned salt")
	}

	// Second call reads the same pair back.
	id2, salt2, err := LoadOrCreateSalt(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateSalt(second): %v", err)
	}
	if id2 != id1 {
		t.Errorf("salt id regenerated: %q != %q", id2, id1)
	}
	if string(salt2) != string(salt1) {
		t.Error("salt regenerated on second call")
	}
}

// TestLoadOrCreateSalt_CorruptRegenerates verifies that a short/corrupt
// salt file (or a bad salt id) regenerates a fresh valid pair instead of
// failing or returning garbage.
func TestLoadOrCreateSalt_CorruptRegenerates(t *testing.T) {
	// Short salt file.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "classifier_salt"), []byte("short"), 0o600); err != nil {
		t.Fatalf("seed short salt: %v", err)
	}
	id, salt, err := LoadOrCreateSalt(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateSalt(short salt): %v", err)
	}
	if len(salt) != 32 || len(id) != 16 {
		t.Fatalf("regenerated pair invalid: salt=%d bytes id=%d chars", len(salt), len(id))
	}

	// Non-hex salt id.
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "classifier_salt_id"), []byte("zzzz"), 0o600); err != nil {
		t.Fatalf("seed bad id: %v", err)
	}
	id, salt, err = LoadOrCreateSalt(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateSalt(bad id): %v", err)
	}
	if len(salt) != 32 || len(id) != 16 {
		t.Fatalf("regenerated pair invalid: salt=%d bytes id=%d chars", len(salt), len(id))
	}
}

// TestLoadOrCreateSalt_EmptyDirErrors verifies the guard on an empty
// config dir.
func TestLoadOrCreateSalt_EmptyDirErrors(t *testing.T) {
	if _, _, err := LoadOrCreateSalt(""); err == nil {
		t.Fatal("LoadOrCreateSalt(\"\") should error")
	}
}
