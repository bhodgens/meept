package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

// unixTime is a test helper building a *time.Time from a unix timestamp.
func unixTime(sec int64) *time.Time {
	t := time.Unix(sec, 0).UTC()
	return &t
}

func TestGenerateRawKeyUnique(t *testing.T) {
	raw1, hash1, err := generateRawKey()
	if err != nil {
		t.Fatalf("generateRawKey: %v", err)
	}
	raw2, hash2, err := generateRawKey()
	if err != nil {
		t.Fatalf("generateRawKey: %v", err)
	}

	if raw1 == raw2 {
		t.Fatal("two generated raw keys are identical")
	}
	if hash1 == hash2 {
		t.Fatal("two generated key hashes are identical")
	}
}

func TestRawKeyShape(t *testing.T) {
	raw, _, err := generateRawKey()
	if err != nil {
		t.Fatalf("generateRawKey: %v", err)
	}
	if len(raw) != 64 { // 32 random bytes hex-encoded
		t.Fatalf("raw key length = %d, want 64", len(raw))
	}
	for _, c := range raw {
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		if !isHex {
			t.Fatalf("raw key %q contains non-lowercase-hex character %q", raw, c)
		}
	}
}

func TestHashMatchesManualSha256(t *testing.T) {
	raw := "a-very-predictable-test-key-value"
	want := sha256.Sum256([]byte(raw))
	wantHex := hex.EncodeToString(want[:])

	if got := hashKey(raw); got != wantHex {
		t.Fatalf("hashKey(%q) = %s, want %s", raw, got, wantHex)
	}
}

func TestCopyUserDeepCopiesKeysAndExpiry(t *testing.T) {
	orig := User{
		ID:   "user-1",
		Name: "alice",
		Keys: []Key{{ID: "key-1", Hash: "h", ExpiresAt: unixTime(1700000000)}},
	}

	cp := copyUser(orig)
	cp.Keys[0].ExpiresAt = nil
	cp.Keys[0].Label = "mutated"

	if orig.Keys[0].ExpiresAt == nil {
		t.Fatal("copying mutated original expiry pointer")
	}
	if orig.Keys[0].ExpiresAt.Unix() != 1700000000 {
		t.Fatalf("original expiry changed: got %d, want 1700000000", orig.Keys[0].ExpiresAt.Unix())
	}
}
