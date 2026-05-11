package llm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestL1Cache_Basic(t *testing.T) {
	config := L1CacheConfig{
		MaxEntries:  100,
		DefaultTTL:  5 * time.Minute,
		CleanupFreq: 1 * time.Minute,
	}
	cache := NewL1Cache(config)
	defer cache.Stop()

	key := CacheKey{
		PromptHash: "abc123",
		ModelID:    "test-model",
		FileHashes: nil,
	}
	entry := &CacheEntry{
		Response:  &Response{Content: "test response"},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	// Put and get
	cache.Put(key, entry)
	got, found := cache.Get(key)
	if !found {
		t.Fatal("expected entry to be found")
	}
	if got.Response.Content != "test response" {
		t.Fatalf("got %q, want %q", got.Response.Content, "test response")
	}
}

func TestL1Cache_Expiration(t *testing.T) {
	config := L1CacheConfig{
		MaxEntries:  100,
		DefaultTTL:  50 * time.Millisecond,
		CleanupFreq: 1 * time.Hour,
	}
	cache := NewL1Cache(config)
	defer cache.Stop()

	key := CacheKey{
		PromptHash: "abc123",
		ModelID:    "test-model",
	}

	// Verify entry is not expired at creation
	entry := &CacheEntry{
		Response:  &Response{Content: "test response"},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	if entry.IsExpired() {
		t.Fatal("entry should not be expired at creation")
	}

	// Put with an already-expired entry
	expiredEntry := &CacheEntry{
		Response:  &Response{Content: "expired response"},
		CreatedAt: time.Now().Add(-1 * time.Hour),
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	cache.Put(key, expiredEntry)

	// Get should detect expiration and return not-found
	_, found := cache.Get(key)
	if found {
		t.Fatal("expected entry to be expired")
	}
}

func TestL1Cache_InvalidateByFile(t *testing.T) {
	config := L1CacheConfig{
		MaxEntries:  100,
		DefaultTTL:  5 * time.Minute,
		CleanupFreq: 1 * time.Hour,
	}
	cache := NewL1Cache(config)
	defer cache.Stop()

	key1 := CacheKey{
		PromptHash: "abc123",
		ModelID:    "test-model",
		FileHashes: map[string]string{"/path/to/file.go": "hash1"},
	}
	key2 := CacheKey{
		PromptHash: "def456",
		ModelID:    "test-model",
		FileHashes: map[string]string{"/path/to/other.go": "hash2"},
	}

	now := time.Now()
	cache.Put(key1, &CacheEntry{
		Response:   &Response{Content: "response1"},
		ExpiresAt:  now.Add(5 * time.Minute),
		FileHashes: key1.FileHashes,
	})
	cache.Put(key2, &CacheEntry{
		Response:   &Response{Content: "response2"},
		ExpiresAt:  now.Add(5 * time.Minute),
		FileHashes: key2.FileHashes,
	})

	// Verify both are cached before invalidation
	_, found1Before := cache.Get(key1)
	_, found2Before := cache.Get(key2)
	if !found1Before || !found2Before {
		t.Fatalf("expected both keys to be cached before invalidation")
	}

	// Invalidate by file
	cache.InvalidateByFile("/path/to/file.go")

	// key1 should be invalidated, key2 should remain
	_, found1After := cache.Get(key1)
	_, found2After := cache.Get(key2)

	if found1After {
		t.Fatal("expected key1 to be invalidated")
	}
	if !found2After {
		t.Fatalf("expected key2 to still exist, but it was not found")
	}
}

func TestL2Cache_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "token_cache_test.db")

	config := L2CacheConfig{
		DBPath:      dbPath,
		DefaultTTL:  5 * time.Minute,
		CleanupFreq: 1 * time.Minute,
	}
	cache, err := NewL2Cache(config)
	if err != nil {
		t.Fatalf("NewL2Cache failed: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()
	key := CacheKey{
		PromptHash: "abc123",
		ModelID:    "test-model",
		FileHashes: nil,
	}
	entry := &CacheEntry{
		Response:   &Response{Content: "test response", Model: "test-model"},
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(5 * time.Minute),
		FileHashes: key.FileHashes,
	}

	cache.Put(ctx, key, entry)
	got, found := cache.Get(ctx, key)
	if !found {
		t.Fatal("expected entry to be found")
	}
	if got.Response.Content != "test response" {
		t.Fatalf("got %q, want %q", got.Response.Content, "test response")
	}
}

func TestL2Cache_FileAware(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "token_cache_test.db")

	tmpFile := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(tmpFile, []byte("package test"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	config := L2CacheConfig{
		DBPath:      dbPath,
		DefaultTTL:  5 * time.Minute,
		CleanupFreq: 1 * time.Minute,
	}
	cache, err := NewL2Cache(config)
	if err != nil {
		t.Fatalf("NewL2Cache failed: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()

	key1 := CacheKey{
		PromptHash: "abc123",
		ModelID:    "test-model",
		FileHashes: map[string]string{tmpFile: "original_hash"},
	}
	entry1 := &CacheEntry{
		Response:   &Response{Content: "original response", Model: "test-model"},
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(5 * time.Minute),
		FileHashes: map[string]string{tmpFile: "original_hash"},
	}
	cache.Put(ctx, key1, entry1)

	key2 := CacheKey{
		PromptHash: "abc123",
		ModelID:    "test-model",
		FileHashes: map[string]string{tmpFile: "new_hash"},
	}
	_, found := cache.Get(ctx, key2)
	if found {
		t.Fatal("expected cache miss due to file hash mismatch")
	}
}

func TestL2Cache_InvalidateByFile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "token_cache_test.db")

	config := L2CacheConfig{
		DBPath:      dbPath,
		DefaultTTL:  5 * time.Minute,
		CleanupFreq: 1 * time.Minute,
	}
	cache, err := NewL2Cache(config)
	if err != nil {
		t.Fatalf("NewL2Cache failed: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()

	key1 := CacheKey{
		PromptHash: "abc123",
		ModelID:    "test-model",
		FileHashes: map[string]string{"/path/to/file.go": "hash1"},
	}
	key2 := CacheKey{
		PromptHash: "def456",
		ModelID:    "test-model",
		FileHashes: map[string]string{"/path/to/other.go": "hash2"},
	}

	now := time.Now()
	cache.Put(ctx, key1, &CacheEntry{
		Response:   &Response{Content: "response1", Model: "test-model"},
		ExpiresAt:  now.Add(5 * time.Minute),
		FileHashes: key1.FileHashes,
	})
	cache.Put(ctx, key2, &CacheEntry{
		Response:   &Response{Content: "response2", Model: "test-model"},
		ExpiresAt:  now.Add(5 * time.Minute),
		FileHashes: key2.FileHashes,
	})

	// Verify both are cached before invalidation
	_, found1Before := cache.Get(ctx, key1)
	_, found2Before := cache.Get(ctx, key2)
	if !found1Before || !found2Before {
		t.Fatalf("expected both keys to be cached before invalidation")
	}

	cache.InvalidateByFile(ctx, "/path/to/file.go")

	_, found1After := cache.Get(ctx, key1)
	_, found2After := cache.Get(ctx, key2)

	if found1After {
		t.Fatal("expected key1 to be invalidated")
	}
	if !found2After {
		t.Fatal("expected key2 to still exist")
	}
}

func TestTokenCacheCoordinator_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "token_cache_test.db")

	config := CacheConfig{
		L1MaxEntries: 100,
		L2Enabled:    true,
		L2DBPath:     dbPath,
		DefaultTTL:   5 * time.Minute,
		CleanupFreq:  1 * time.Minute,
		Enabled:      true,
		FileAware:    true,
	}

	coordinator, err := NewTokenCacheCoordinator(config)
	if err != nil {
		t.Fatalf("NewTokenCacheCoordinator failed: %v", err)
	}
	defer coordinator.Close()

	ctx := context.Background()
	key := CacheKey{
		PromptHash: "abc123",
		ModelID:    "test-model",
	}
	response := &Response{Content: "test response", Model: "test-model"}

	coordinator.Put(ctx, key, response)

	got, found := coordinator.Get(ctx, key)
	if !found {
		t.Fatal("expected entry to be found")
	}
	if got.Response.Content != "test response" {
		t.Fatalf("got %q, want %q", got.Response.Content, "test response")
	}

	stats := coordinator.Stats()
	if stats.L1Hits != 1 {
		t.Fatalf("expected 1 L1 hit, got %d", stats.L1Hits)
	}
}

func TestTokenCacheCoordinator_L2Fallback(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "token_cache_test.db")

	config := CacheConfig{
		L1MaxEntries: 10,
		L2Enabled:    true,
		L2DBPath:     dbPath,
		DefaultTTL:   5 * time.Minute,
		CleanupFreq:  1 * time.Minute,
		Enabled:      true,
	}

	coordinator, err := NewTokenCacheCoordinator(config)
	if err != nil {
		t.Fatalf("NewTokenCacheCoordinator failed: %v", err)
	}
	defer coordinator.Close()

	ctx := context.Background()

	for i := range 15 {
		key := CacheKey{
			PromptHash: string(rune('a' + i)),
			ModelID:    "test-model",
		}
		coordinator.Put(ctx, key, &Response{Content: "response", Model: "test-model"})
	}

	key := CacheKey{
		PromptHash: "a",
		ModelID:    "test-model",
	}
	_, found := coordinator.Get(ctx, key)
	if !found {
		t.Fatal("expected to find entry in L2 even if evicted from L1")
	}

	// Verify the entry was retrieved from L2 (not L1)
	stats := coordinator.Stats()
	if stats.L2Hits == 0 {
		t.Fatal("expected L2Hits > 0 when entry is evicted from L1 and found in L2")
	}
}

func TestCacheKey_String(t *testing.T) {
	key := CacheKey{
		PromptHash: "abc123def456",
		ModelID:    "test-model",
	}
	s := key.String()
	if s == "" {
		t.Fatal("expected non-empty string")
	}

	keyWithFiles := CacheKey{
		PromptHash: "abc123def456",
		ModelID:    "test-model",
		FileHashes: map[string]string{"/file.go": "hash"},
	}
	s2 := keyWithFiles.String()
	if s2 == "" {
		t.Fatal("expected non-empty string for key with files")
	}
}

func TestCacheEntry_IsExpired(t *testing.T) {
	entry := &CacheEntry{
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	if !entry.IsExpired() {
		t.Fatal("expected entry to be expired")
	}

	entry2 := &CacheEntry{
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if entry2.IsExpired() {
		t.Fatal("expected entry to not be expired")
	}
}
