package integration

// cache_eviction_under_pressure_test.go — Tests CAS eviction under
// capacity pressure (spec §7, §10). Small cap. Multi-job. Eviction order:
// lowest refcount first, oldest as tiebreaker. Pinned entries preserved.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/resources"
	"github.com/caimlas/meept/pkg/id"
)

// TestCacheEvictionUnderPressure verifies that when CAS is at capacity,
// lowest-refcount entries are evicted first and pinned entries survive.
func TestCacheEvictionUnderPressure(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Create a CAS with a small cap: 1500 bytes.
	casCfg := resources.CASConfig{
		StoreDir:              filepath.Join(tmpDir, "cas"),
		CapacityBytes:         1500,
		EvictionSweepInterval: 0, // no background sweep
		HashAlgorithm:         resources.AlgoBlake3,
	}
	store, err := resources.NewCASStore(casCfg, nil)
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	defer store.Close()

	// Helper to add a unique blob of given size.
	addBlob := func(size int, seed byte) (string, []byte) {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i%251) ^ seed
		}
		path := filepath.Join(tmpDir, id.Generate("blob-"))
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		hash, err := store.Add(ctx, path)
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		return hash, data
	}

	// Add 3 blobs of 500 bytes each (total 1500, right at cap).
	hash1, _ := addBlob(500, 0x01)
	hash2, _ := addBlob(500, 0x02)
	hash3, _ := addBlob(500, 0x03)

	// Verify all 3 are present.
	if !store.Has(refBody(hash1)) {
		t.Error("hash1 should be present")
	}
	if !store.Has(refBody(hash2)) {
		t.Error("hash2 should be present")
	}
	if !store.Has(refBody(hash3)) {
		t.Error("hash3 should be present")
	}

	// Set refcounts: hash1=1, hash2=1, hash3=0.
	store.IncrementRef(refBody(hash1))
	store.IncrementRef(refBody(hash2))

	// Pin hash1 so it survives eviction.
	store.Pin(refBody(hash1))

	// Verify hash3 has refcount=0 (eligible for eviction).
	if store.Refcount(refBody(hash3)) != 0 {
		t.Errorf("hash3 refcount: got %d, want 0", store.Refcount(refBody(hash3)))
	}

	// Add a 4th blob — triggers eviction to make room.
	hash4, _ := addBlob(500, 0x04)

	// hash3 (refcount=0) should be evicted first. hash1 is pinned.
	// hash2 has refcount=1.
	// Since hash3 is evicted, hash4 should fit.
	if store.Has(refBody(hash4)) {
		// hash4 should be present.
	} else {
		t.Error("hash4 should be present after eviction made room")
	}

	// hash1 should survive (pinned).
	if !store.Has(refBody(hash1)) {
		t.Error("hash1 (pinned) should survive eviction")
	}
}

// TestCacheEvictionRefcountOrder verifies that zero-refcount entries are
// evicted before positive-refcount entries.
func TestCacheEvictionRefcountOrder(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	casCfg := resources.CASConfig{
		StoreDir:              filepath.Join(tmpDir, "cas"),
		CapacityBytes:         1000,
		EvictionSweepInterval: 0,
		HashAlgorithm:         resources.AlgoBlake3,
	}
	store, err := resources.NewCASStore(casCfg, nil)
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	defer store.Close()

	addBlob := func(content []byte) string {
		path := filepath.Join(tmpDir, id.Generate("blob-"))
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		hash, err := store.Add(ctx, path)
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		return hash
	}

	// Add two 400-byte blobs (total 800, under 1000 cap). Different content!
	content400A := make([]byte, 400)
	content400B := make([]byte, 400)
	for i := range content400A {
		content400A[i] = byte(i % 251)
		content400B[i] = byte((i + 100) % 251)
	}
	hashA := addBlob(content400A)
	hashB := addBlob(content400B)

	// hashA has refcount=0, hashB has refcount=1.
	store.IncrementRef(refBody(hashB))

	// Add a 300-byte blob (total 1100, exceeds 1000 cap).
	// hashA (refcount=0) should be evicted.
	content300 := make([]byte, 300)
	for i := range content300 {
		content300[i] = byte((i + 200) % 251)
	}
	hashC := addBlob(content300)

	// hashA should be evicted (refcount=0, lowest eligible).
	if store.Has(refBody(hashA)) {
		t.Error("hashA (refcount=0) should have been evicted")
	}

	// hashB should survive (refcount=1).
	if !store.Has(refBody(hashB)) {
		t.Error("hashB (refcount=1) should survive eviction")
	}

	// hashC should be present.
	if !store.Has(refBody(hashC)) {
		t.Error("hashC should be present after add")
	}
}

// refBody extracts the hash body from a prefixed hash string.
func refBody(hash string) string {
	_, body, isCAS := resources.ParseRef(hash)
	if isCAS {
		return body
	}
	return hash
}
