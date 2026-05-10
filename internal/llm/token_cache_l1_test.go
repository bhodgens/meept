package llm

import (
	"fmt"
	"testing"
	"time"
)

func TestL1Cache_LRU_Eviction(t *testing.T) {
	cache := NewL1Cache(L1CacheConfig{MaxEntries: 3})

	// Insert 3 entries
	for i := 0; i < 3; i++ {
		key := CacheKey{ModelID: "model", PromptHash: fmt.Sprintf("hash%d", i)}
		entry := &CacheEntry{
			Response:       &Response{Content: fmt.Sprintf("response%d", i)},
			CreatedAt:      time.Now(),
			LastAccessedAt: time.Now(),
			ExpiresAt:      time.Now().Add(time.Hour),
		}
		cache.Put(key, entry)
	}

	// Access entry 0 to make it recently used
	key0 := CacheKey{ModelID: "model", PromptHash: "hash0"}
	cache.Get(key0)

	// Insert a 4th entry -- should evict the LRU (entry 1, not entry 0)
	key3 := CacheKey{ModelID: "model", PromptHash: "hash3"}
	entry3 := &CacheEntry{
		Response:       &Response{Content: "response3"},
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
		ExpiresAt:      time.Now().Add(time.Hour),
	}
	cache.Put(key3, entry3)

	// Entry 0 should still be present (recently accessed)
	if _, found := cache.Get(key0); !found {
		t.Error("recently accessed entry 0 should not have been evicted")
	}

	// Entry 1 should have been evicted (least recently used)
	key1 := CacheKey{ModelID: "model", PromptHash: "hash1"}
	if _, found := cache.Get(key1); found {
		t.Error("entry 1 should have been evicted as LRU")
	}
}

func TestL1Cache_LRU_Eviction_Zero_LastAccessedAt(t *testing.T) {
	// Test that entries without LastAccessedAt set fall back to CreatedAt
	cache := NewL1Cache(L1CacheConfig{MaxEntries: 2})

	now := time.Now()
	key0 := CacheKey{ModelID: "model", PromptHash: "hash0"}
	cache.Put(key0, &CacheEntry{
		Response:  &Response{Content: "response0"},
		CreatedAt: now,
		// LastAccessedAt intentionally left as zero value
		ExpiresAt: now.Add(time.Hour),
	})

	// Small delay so CreatedAt differs
	time.Sleep(1 * time.Millisecond)

	key1 := CacheKey{ModelID: "model", PromptHash: "hash1"}
	cache.Put(key1, &CacheEntry{
		Response:  &Response{Content: "response1"},
		CreatedAt: time.Now(),
		// LastAccessedAt intentionally left as zero value
		ExpiresAt: time.Now().Add(time.Hour),
	})

	// Insert a 3rd entry -- should evict key0 (older CreatedAt, used as fallback)
	key2 := CacheKey{ModelID: "model", PromptHash: "hash2"}
	cache.Put(key2, &CacheEntry{
		Response:       &Response{Content: "response2"},
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
		ExpiresAt:      time.Now().Add(time.Hour),
	})

	if _, found := cache.Get(key0); found {
		t.Error("entry 0 should have been evicted (older CreatedAt fallback)")
	}
	if _, found := cache.Get(key1); !found {
		t.Error("entry 1 should still be present")
	}
}
