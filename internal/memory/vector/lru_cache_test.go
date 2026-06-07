package vector

import (
	"testing"
)

func TestLRUCache_New(t *testing.T) {
	cache := NewLRUCache(3)
	if cache.Len() != 0 {
		t.Errorf("expected empty cache, got len %d", cache.Len())
	}
	if cache.maxSize != 3 {
		t.Errorf("expected maxSize 3, got %d", cache.maxSize)
	}
}

func TestLRUCache_Access(t *testing.T) {
	cache := NewLRUCache(3)

	// First access - miss
	cache.Access("key1")
	if cache.Len() != 1 {
		t.Errorf("expected len 1, got %d", cache.Len())
	}

	// Second access - miss
	cache.Access("key2")
	cache.Access("key3")
	if cache.Len() != 3 {
		t.Errorf("expected len 3, got %d", cache.Len())
	}

	// Fourth access - miss + eviction
	cache.Access("key4")
	if cache.Len() != 3 {
		t.Errorf("expected len still 3 after eviction, got %d", cache.Len())
	}

	// Verify key1 was evicted (least recently used)
	keys := cache.Keys()
	for _, k := range keys {
		if k == "key1" {
			t.Error("key1 should have been evicted")
		}
	}
}

func TestLRUCache_HitsAndMisses(t *testing.T) {
	cache := NewLRUCache(3)

	// First access - miss
	cache.Access("key1")
	if cache.Misses() != 1 {
		t.Errorf("expected 1 miss, got %d", cache.Misses())
	}

	// Second access to same key - hit
	cache.Access("key1")
	if cache.Hits() != 1 {
		t.Errorf("expected 1 hit, got %d", cache.Hits())
	}

	// Third access to same key - hit
	cache.Access("key1")
	if cache.Hits() != 2 {
		t.Errorf("expected 2 hits, got %d", cache.Hits())
	}
}

func TestLRUCache_Evictions(t *testing.T) {
	cache := NewLRUCache(2)

	cache.Access("key1")
	cache.Access("key2")
	if cache.Evictions() != 0 {
		t.Errorf("expected 0 evictions, got %d", cache.Evictions())
	}

	// This should trigger an eviction
	cache.Access("key3")
	if cache.Evictions() != 1 {
		t.Errorf("expected 1 eviction, got %d", cache.Evictions())
	}
}

func TestLRUCache_AccessOrder(t *testing.T) {
	cache := NewLRUCache(3)

	cache.Access("key1")
	cache.Access("key2")
	cache.Access("key3")

	// Access key1 again to make it most recently used
	cache.Access("key1")

	keys := cache.Keys()
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}

	// Keys should be in LRU order: key2 (least), key3, key1 (most)
	if keys[0] != "key2" {
		t.Errorf("expected first key to be key2 (LRU), got %s", keys[0])
	}
	if keys[1] != "key3" {
		t.Errorf("expected second key to be key3, got %s", keys[1])
	}
	if keys[2] != "key1" {
		t.Errorf("expected third key to be key1 (MRU), got %s", keys[2])
	}
}

func TestLRUCache_MRUNotEvicted(t *testing.T) {
	cache := NewLRUCache(2)

	cache.Access("key1")
	cache.Access("key2")

	// Access key1 to make it MRU
	cache.Access("key1")

	// Access key3 - should evict key2 (LRU), not key1
	cache.Access("key3")

	keys := cache.Keys()
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}

	// key2 should be evicted, key1 and key3 should remain
	found := make(map[string]bool)
	for _, k := range keys {
		found[k] = true
	}

	if found["key2"] {
		t.Error("key2 should have been evicted")
	}
	if !found["key1"] {
		t.Error("key1 should still be in cache")
	}
	if !found["key3"] {
		t.Error("key3 should still be in cache")
	}
}

func TestLRUCache_Keys(t *testing.T) {
	cache := NewLRUCache(5)

	// Empty cache
	keys := cache.Keys()
	if len(keys) != 0 {
		t.Errorf("expected 0 keys for empty cache, got %d", len(keys))
	}

	// Single key
	cache.Access("single")
	keys = cache.Keys()
	if len(keys) != 1 || keys[0] != "single" {
		t.Errorf("expected [single], got %v", keys)
	}
}
