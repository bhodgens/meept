package resources

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// newTestStore creates a CASStore in a temp dir with the given capacity.
// The store is registered for cleanup via t.Cleanup.
func newTestStore(t *testing.T, capacityBytes int64) *CASStore {
	t.Helper()
	dir := t.TempDir()
	cfg := CASConfig{
		StoreDir:              dir,
		CapacityBytes:         capacityBytes,
		EvictionSweepInterval: 0, // disable background sweep for unit tests
		HashAlgorithm:         AlgoBlake3,
	}
	store, err := NewCASStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() {
		store.Close()
	})
	return store
}

// writeFile creates a temp file with the given content and returns its path.
func writeFile(t *testing.T, content []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "source.bin")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	return path
}

func TestCASStore_AddAndHas(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	src := writeFile(t, []byte("hello world"))
	hash, err := store.Add(ctx, src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}

	// Has should return true.
	if !store.Has(hash) {
		t.Error("Has returned false after Add")
	}

	// Verify data file exists.
	dataPath := HashDataPath(store.StoreDir(), hash)
	if _, err := os.Stat(dataPath); err != nil {
		t.Errorf("data file missing: %v", err)
	}

	// Verify meta.json exists.
	metaPath := HashMetaPath(store.StoreDir(), hash)
	if _, err := os.Stat(metaPath); err != nil {
		t.Errorf("meta.json missing: %v", err)
	}

	// Verify meta record in index.
	rec, ok := store.index.Get(hash)
	if !ok {
		t.Fatal("index has no record")
	}
	if rec.OriginalName != "source.bin" {
		t.Errorf("expected original_name 'source.bin', got %q", rec.OriginalName)
	}
	if rec.Size != int64(len("hello world")) {
		t.Errorf("expected size %d, got %d", len("hello world"), rec.Size)
	}
	if rec.Refcount != 0 {
		t.Errorf("expected refcount 0, got %d", rec.Refcount)
	}
}

func TestCASStore_AddIdempotent(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	src := writeFile(t, []byte("identical content"))
	hash1, err := store.Add(ctx, src)
	if err != nil {
		t.Fatalf("first Add: %v", err)
	}
	hash2, err := store.Add(ctx, src)
	if err != nil {
		t.Fatalf("second Add: %v", err)
	}
	if hash1 != hash2 {
		t.Errorf("same content produced different hashes: %s vs %s", hash1, hash2)
	}
}

func TestCASStore_GetPath(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	src := writeFile(t, []byte("getpath test"))
	hash, err := store.Add(ctx, src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	path, err := store.GetPath(hash)
	if err != nil {
		t.Fatalf("GetPath: %v", err)
	}

	// Read the data back and verify.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "getpath test" {
		t.Errorf("data mismatch: got %q", string(data))
	}
}

func TestCASStore_GetPath_Missing(t *testing.T) {
	store := newTestStore(t, 0)
	_, err := store.GetPath("nonexistenthash")
	if err == nil {
		t.Fatal("expected error for missing hash")
	}
}

func TestCASStore_Refcount(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	src := writeFile(t, []byte("refcount test"))
	hash, err := store.Add(ctx, src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Initial refcount should be 0.
	if rc := store.Refcount(hash); rc != 0 {
		t.Fatalf("expected initial refcount 0, got %d", rc)
	}

	// Increment.
	store.IncrementRef(hash)
	if rc := store.Refcount(hash); rc != 1 {
		t.Errorf("expected refcount 1 after increment, got %d", rc)
	}

	store.IncrementRef(hash)
	if rc := store.Refcount(hash); rc != 2 {
		t.Errorf("expected refcount 2 after increment, got %d", rc)
	}

	// Decrement.
	store.DecrementRef(hash)
	if rc := store.Refcount(hash); rc != 1 {
		t.Errorf("expected refcount 1 after decrement, got %d", rc)
	}

	store.DecrementRef(hash)
	if rc := store.Refcount(hash); rc != 0 {
		t.Errorf("expected refcount 0 after decrement, got %d", rc)
	}

	// Decrement below zero should floor at zero.
	store.DecrementRef(hash)
	if rc := store.Refcount(hash); rc != 0 {
		t.Errorf("expected refcount 0 (floored), got %d", rc)
	}
}

func TestCASStore_DecrementRef_Metrics(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	src := writeFile(t, []byte("metrics test"))
	hash, err := store.Add(ctx, src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	mock := &mockMetrics{}
	store.SetMetricsEmitter(mock)

	// Increment then decrement to zero — should fire IncCASRefcountZeroEligible.
	store.IncrementRef(hash)
	store.DecrementRef(hash)

	if mock.refcountZeroEligible != 1 {
		t.Errorf("expected 1 refcountZeroEligible signal, got %d", mock.refcountZeroEligible)
	}

	// Decrement again — already zero, should NOT fire (no change).
	store.DecrementRef(hash)
	if mock.refcountZeroEligible != 1 {
		t.Errorf("expected refcountZeroEligible still 1, got %d", mock.refcountZeroEligible)
	}
}

func TestCASStore_PinUnpin(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	src := writeFile(t, []byte("pin test"))
	hash, err := store.Add(ctx, src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if store.IsPinned(hash) {
		t.Error("expected unpinned after Add")
	}

	store.Pin(hash)
	if !store.IsPinned(hash) {
		t.Error("expected pinned after Pin")
	}

	store.Unpin(hash)
	if store.IsPinned(hash) {
		t.Error("expected unpinned after Unpin")
	}
}

func TestCASStore_PinFromConfig(t *testing.T) {
	dir := t.TempDir()

	// First, add a blob to get its hash.
	cfg1 := CASConfig{StoreDir: dir, HashAlgorithm: AlgoBlake3}
	store1, err := NewCASStore(cfg1, nil)
	if err != nil {
		t.Fatalf("NewCASStore 1: %v", err)
	}
	src := writeFile(t, []byte("config pin test"))
	hash, err := store1.Add(context.Background(), src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	store1.Close()

	// Reopen with the hash pinned via config.
	cfg2 := CASConfig{
		StoreDir:     dir,
		PinnedHashes: []string{hash},
	}
	store2, err := NewCASStore(cfg2, nil)
	if err != nil {
		t.Fatalf("NewCASStore 2: %v", err)
	}
	defer store2.Close()

	if !store2.IsPinned(hash) {
		t.Error("expected pinned from config")
	}
}

func TestCASStore_EvictRemovesZeroRefcount(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	// Add two blobs.
	src1 := writeFile(t, []byte("blob one"))
	hash1, err := store.Add(ctx, src1)
	if err != nil {
		t.Fatalf("Add 1: %v", err)
	}

	src2 := writeFile(t, []byte("blob two"))
	hash2, err := store.Add(ctx, src2)
	if err != nil {
		t.Fatalf("Add 2: %v", err)
	}

	// Both have refcount 0, both eligible.
	n, err := store.Evict(ctx)
	if err != nil {
		t.Fatalf("Evict: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 evicted, got %d", n)
	}
	if store.Has(hash1) {
		t.Error("hash1 should have been evicted")
	}
	if store.Has(hash2) {
		t.Error("hash2 should have been evicted")
	}
}

func TestCASStore_EvictSkipsReferenced(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	src := writeFile(t, []byte("referenced blob"))
	hash, err := store.Add(ctx, src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Increment refcount so it's referenced.
	store.IncrementRef(hash)

	n, err := store.Evict(ctx)
	if err != nil {
		t.Fatalf("Evict: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 evicted, got %d", n)
	}
	if !store.Has(hash) {
		t.Error("referenced blob was evicted")
	}
}

func TestCASStore_EvictSkipsPinned(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	src := writeFile(t, []byte("pinned blob"))
	hash, err := store.Add(ctx, src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	store.Pin(hash)

	n, err := store.Evict(ctx)
	if err != nil {
		t.Fatalf("Evict: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 evicted, got %d", n)
	}
	if !store.Has(hash) {
		t.Error("pinned blob was evicted")
	}
}

func TestCASStore_CapDrivenEviction(t *testing.T) {
	// Small cap: 10 bytes.
	store := newTestStore(t, 10)
	ctx := context.Background()

	// Add first blob (4 bytes). Fits within cap.
	src1 := writeFile(t, []byte("AAAA")) // 4 bytes
	hash1, err := store.Add(ctx, src1)
	if err != nil {
		t.Fatalf("Add 1: %v", err)
	}

	// Add second blob (8 bytes). 4+8=12 > 10 triggers eviction.
	// blob1 has refcount=0 so it gets evicted to make room.
	src2 := writeFile(t, []byte("BBBBBBBB")) // 8 bytes
	hash2, err := store.Add(ctx, src2)
	if err != nil {
		t.Fatalf("Add 2: %v", err)
	}

	// After Add 2, total must be under cap (only blob2, 8 bytes).
	total := store.TotalSize()
	if total > 10 {
		t.Errorf("total %d exceeds cap 10", total)
	}

	// hash1 should have been evicted; hash2 (just added) survives.
	if store.Has(hash1) {
		t.Error("hash1 should have been evicted under cap pressure")
	}
	if !store.Has(hash2) {
		t.Error("hash2 should still be present")
	}
}

func TestCASStore_CapDrivenEvictionOrder(t *testing.T) {
	// Cap: 50 bytes. Add two 20-byte blobs (total 40, under cap), then a
	// third 20-byte blob. 40+20=60 > 50 triggers Evict which reclaims
	// ALL zero-refcount entries (transit cache semantics). After eviction
	// all prior blobs are gone; only the new blob remains.
	store := newTestStore(t, 50)
	ctx := context.Background()

	src1 := writeFile(t, []byte("01234567890123456789")) // 20 bytes
	hash1, err := store.Add(ctx, src1)
	if err != nil {
		t.Fatalf("Add 1: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	src2 := writeFile(t, []byte("abcdefghijabcdefghij")) // 20 bytes
	hash2, err := store.Add(ctx, src2)
	if err != nil {
		t.Fatalf("Add 2: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	// Add third blob — triggers eviction of all zero-refcount entries.
	src3 := writeFile(t, []byte("zyxwvutsrqzyxwvutsrq")) // 20 bytes
	_, err = store.Add(ctx, src3)
	if err != nil {
		t.Fatalf("Add 3: %v", err)
	}

	// Per transit-cache semantics, all zero-refcount entries were evicted.
	if store.Has(hash1) {
		t.Error("hash1 (zero refcount) should have been evicted")
	}
	if store.Has(hash2) {
		t.Error("hash2 (zero refcount) should have been evicted")
	}
}

func TestCASStore_CapDrivenEvictionPreservesReferenced(t *testing.T) {
	// Cap: 50 bytes. Add a referenced blob and an unreferenced blob,
	// then add a third that triggers eviction. The referenced blob must
	// survive; the unreferenced one is evicted.
	store := newTestStore(t, 50)
	ctx := context.Background()

	src1 := writeFile(t, []byte("01234567890123456789")) // 20 bytes
	hash1, err := store.Add(ctx, src1)
	if err != nil {
		t.Fatalf("Add 1: %v", err)
	}
	store.IncrementRef(hash1) // now referenced

	src2 := writeFile(t, []byte("abcdefghijabcdefghij")) // 20 bytes
	hash2, err := store.Add(ctx, src2)
	if err != nil {
		t.Fatalf("Add 2: %v", err)
	}
	// hash2 has refcount=0

	// Add third blob — triggers eviction. hash2 (zero refcount) evicted,
	// hash1 (referenced) preserved. But: 20+20=40 <= 50, so no eviction
	// yet. Total after adding blob3 would be 60 > 50.
	src3 := writeFile(t, []byte("zyxwvutsrqzyxwvutsrq")) // 20 bytes
	_, err = store.Add(ctx, src3)
	if err != nil {
		t.Fatalf("Add 3: %v", err)
	}

	// hash1 should survive (referenced), hash2 evicted (zero refcount).
	if !store.Has(hash1) {
		t.Error("hash1 (referenced) should have been preserved")
	}
	if store.Has(hash2) {
		t.Error("hash2 (zero refcount) should have been evicted")
	}
}

func TestCASStore_CapFullAllReferenced(t *testing.T) {
	// Very small cap with all blobs referenced: Add should fail with CacheFull.
	store := newTestStore(t, 20)
	ctx := context.Background()

	src1 := writeFile(t, []byte("twenty byte blob!!!")) // 20 bytes
	hash1, err := store.Add(ctx, src1)
	if err != nil {
		t.Fatalf("Add 1: %v", err)
	}
	// Pin it so it can't be evicted.
	store.Pin(hash1)

	// Now add a second blob — can't evict the first, so should fail.
	src2 := writeFile(t, []byte("another twenty bytes"))
	_, err = store.Add(ctx, src2)
	if err == nil {
		t.Fatal("expected ErrCacheFull, got nil")
	}
}

func TestCASStore_ConcurrentAdd(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	// All goroutines add the same content — should result in one CAS entry.
	content := []byte("concurrent add content")
	var wg sync.WaitGroup
	const N = 10
	hashes := make([]string, N)
	errs := make([]error, N)

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			src := writeFile(t, content)
			hashes[idx], errs[idx] = store.Add(ctx, src)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: Add: %v", i, err)
		}
	}

	// All hashes should be identical.
	firstHash := hashes[0]
	for i, h := range hashes {
		if h != firstHash {
			t.Errorf("goroutine %d: hash %s != first %s", i, h, firstHash)
		}
	}

	// One entry in index.
	if store.index.EntryCount() != 1 {
		t.Errorf("expected 1 index entry, got %d", store.index.EntryCount())
	}
}

func TestCASStore_VerifyBlob(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	src := writeFile(t, []byte("verify me"))
	hash, err := store.Add(ctx, src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Should verify successfully.
	if err := store.VerifyBlob(hash, AlgoBlake3); err != nil {
		t.Errorf("VerifyBlob: %v", err)
	}
}

func TestCASStore_VerifyBlob_Corruption(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	src := writeFile(t, []byte("original content"))
	hash, err := store.Add(ctx, src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Corrupt the data file.
	dataPath := HashDataPath(store.StoreDir(), hash)
	if err := os.WriteFile(dataPath, []byte("corrupted!"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	err = store.VerifyBlob(hash, AlgoBlake3)
	if err == nil {
		t.Fatal("expected corruption error")
	}
}

func TestCASStore_StoreBlob(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	data := []byte("stored blob data")
	hash, err := HashBytes(data, AlgoBlake3)
	if err != nil {
		t.Fatalf("HashBytes: %v", err)
	}

	mock := &mockMetrics{}
	store.SetMetricsEmitter(mock)

	if err := store.StoreBlob(ctx, hash, data, "peer-test"); err != nil {
		t.Fatalf("StoreBlob: %v", err)
	}

	if !store.Has(hash) {
		t.Error("Has returned false after StoreBlob")
	}

	// Verify telemetry.
	if mock.bytesFetched != int64(len(data)) {
		t.Errorf("expected bytesFetched %d, got %d", len(data), mock.bytesFetched)
	}

	// Verify source in metadata.
	rec, ok := store.index.Get(hash)
	if !ok {
		t.Fatal("index missing record")
	}
	if rec.Source != "peer-test" {
		t.Errorf("expected source 'peer-test', got %q", rec.Source)
	}
}

func TestCASStore_StoreBlobIdempotent(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	data := []byte("idempotent blob")
	hash, _ := HashBytes(data, AlgoBlake3)

	if err := store.StoreBlob(ctx, hash, data, "peer-1"); err != nil {
		t.Fatalf("first StoreBlob: %v", err)
	}
	if err := store.StoreBlob(ctx, hash, data, "peer-2"); err != nil {
		t.Fatalf("second StoreBlob: %v", err)
	}
	// Source should be from the first store.
	rec, _ := store.index.Get(hash)
	if rec.Source != "peer-1" {
		t.Errorf("expected source 'peer-1', got %q", rec.Source)
	}
}

func TestCASStore_EligibleCount(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	src1 := writeFile(t, []byte("eligible one"))
	_, err := store.Add(ctx, src1)
	if err != nil {
		t.Fatalf("Add 1: %v", err)
	}

	src2 := writeFile(t, []byte("referenced"))
	hash2, err := store.Add(ctx, src2)
	if err != nil {
		t.Fatalf("Add 2: %v", err)
	}
	store.IncrementRef(hash2)

	src3 := writeFile(t, []byte("pinned"))
	hash3, err := store.Add(ctx, src3)
	if err != nil {
		t.Fatalf("Add 3: %v", err)
	}
	store.Pin(hash3)

	if n := store.EligibleCount(); n != 1 {
		t.Errorf("expected 1 eligible, got %d", n)
	}

	// Release hash2 -> now eligible.
	store.DecrementRef(hash2)
	if n := store.EligibleCount(); n != 2 {
		t.Errorf("expected 2 eligible after release, got %d", n)
	}
}

func TestCASStore_HashDataPath(t *testing.T) {
	root := "/tmp/resources"
	hash := "abcdef1234567890"

	path := HashDataPath(root, hash)
	expected := "/tmp/resources/ab/cd/abcdef1234567890/data"
	if path != expected {
		t.Errorf("HashDataPath: got %s, want %s", path, expected)
	}

	metaPath := HashMetaPath(root, hash)
	expectedMeta := "/tmp/resources/ab/cd/abcdef1234567890/meta.json"
	if metaPath != expectedMeta {
		t.Errorf("HashMetaPath: got %s, want %s", metaPath, expectedMeta)
	}
}

func TestCASStore_SHA256Algorithm(t *testing.T) {
	dir := t.TempDir()
	cfg := CASConfig{
		StoreDir:      dir,
		HashAlgorithm: AlgoSHA256,
	}
	store, err := NewCASStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	src := writeFile(t, []byte("sha256 mode"))
	hash, err := store.Add(ctx, src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// SHA-256 produces 64 hex chars.
	if len(hash) != 64 {
		t.Errorf("expected 64-char SHA-256 hash, got %d chars", len(hash))
	}

	// Verify with SHA-256.
	if err := store.VerifyBlob(hash, AlgoSHA256); err != nil {
		t.Errorf("VerifyBlob: %v", err)
	}
}

func TestCASStore_ReopenIndex(t *testing.T) {
	dir := t.TempDir()

	cfg := CASConfig{
		StoreDir:      dir,
		HashAlgorithm: AlgoBlake3,
	}
	store1, err := NewCASStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewCASStore 1: %v", err)
	}
	src := writeFile(t, []byte("persist across reopen"))
	hash, err := store1.Add(context.Background(), src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	store1.Close()

	// Reopen — index should be warmed from bbolt.
	store2, err := NewCASStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewCASStore 2: %v", err)
	}
	defer store2.Close()

	if !store2.Has(hash) {
		t.Error("Has returned false after reopen")
	}
}

// --- mock metrics ---

type mockMetrics struct {
	hits                 int
	misses               int
	bytesFetched         int64
	bytesEvicted         int64
	refcountZeroEligible int
}

func (m *mockMetrics) IncCASHits()                 { m.hits++ }
func (m *mockMetrics) IncCASMisses()               { m.misses++ }
func (m *mockMetrics) IncCASBytesFetched(n int64)  { m.bytesFetched += n }
func (m *mockMetrics) IncCASBytesEvicted(n int64)  { m.bytesEvicted += n }
func (m *mockMetrics) IncCASRefcountZeroEligible() { m.refcountZeroEligible++ }
