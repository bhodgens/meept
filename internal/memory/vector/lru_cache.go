package vector

import (
	"container/list"
	"sync"
)

// cacheItem represents an item stored in the LRU cache.
type cacheItem struct {
	key string
}

// LRUCache implements a least-recently-used cache for shard eviction.
// It tracks access order and provides statistics on hits, misses, and evictions.
type LRUCache struct {
	mu        sync.Mutex
	items     map[string]*list.Element
	list      *list.List
	maxSize   int
	hits      int64
	misses    int64
	evictions int64
}

// NewLRUCache creates a new LRU cache with the specified maximum size.
func NewLRUCache(maxSize int) *LRUCache {
	return &LRUCache{
		items:   make(map[string]*list.Element),
		list:    list.New(),
		maxSize: maxSize,
	}
}

// Access marks a key as recently used, moving it to the back of the list.
// If the key is not present, it is added (and may trigger eviction).
func (c *LRUCache) Access(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		// Key exists - move to back (most recently used)
		c.list.MoveToBack(elem)
		c.hits++
	} else {
		// Key not found - insert
		c.misses++
		elem := c.list.PushBack(&cacheItem{key: key})
		c.items[key] = elem

		// Evict if over capacity
		if c.list.Len() > c.maxSize {
			front := c.list.Front()
			if front != nil {
				item := front.Value.(*cacheItem)
				delete(c.items, item.key)
				c.list.Remove(front)
				c.evictions++
			}
		}
	}
}

// Keys returns all keys in access order (least to most recently used).
func (c *LRUCache) Keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	keys := make([]string, 0, c.list.Len())
	for elem := c.list.Front(); elem != nil; elem = elem.Next() {
		item := elem.Value.(*cacheItem)
		keys = append(keys, item.key)
	}
	return keys
}

// Len returns the number of items in the cache.
func (c *LRUCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.list.Len()
}

// Hits returns the number of cache hits.
func (c *LRUCache) Hits() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits
}

// Misses returns the number of cache misses.
func (c *LRUCache) Misses() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.misses
}

// Evictions returns the number of cache evictions.
func (c *LRUCache) Evictions() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evictions
}
