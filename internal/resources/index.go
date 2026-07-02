package resources

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

// bucketResources is the bbolt bucket name for CAS metadata records.
var bucketResources = []byte("ResourcesIndex")

// Index is the persistent (bbolt-backed) and hot (in-memory) index for the
// CAS store. It tracks hash to metadata mappings, refcounts, and insertion
// order for eviction sweeps.
//
// The in-memory cache is a read-through optimization: writes go to both
// bbolt and memory; reads hit memory first. On restart, bbolt is the source
// of truth and the cache is lazily rebuilt.
type Index struct {
	mu     sync.RWMutex
	db     *bolt.DB
	cache  map[string]*metaRecord // hash → record (hot path)
	closed bool
}

// metaRecord is the serialised form stored in bbolt and mirrored in
// meta.json on disk. It is the canonical human-readable record.
type metaRecord struct {
	Hash         string    `json:"hash"`
	OriginalName string    `json:"original_name"`
	Size         int64     `json:"size"`
	AddedAt      time.Time `json:"added_at"`
	Refcount     int       `json:"refcount"`
	Pinned       bool      `json:"pinned"`
	Source       string    `json:"source"` // "local" or "peer-<nodeID>"
}

// NewIndex opens (or creates) a bbolt database at dbPath. The parent
// directory must already exist.
func NewIndex(dbPath string) (*Index, error) {
	db, err := bolt.Open(dbPath, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("resources: open index db: %w", err)
	}

	// Ensure the bucket exists.
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketResources)
		return err
	}); err != nil {
		db.Close()
		return nil, fmt.Errorf("resources: create index bucket: %w", err)
	}

	idx := &Index{
		db:    db,
		cache: make(map[string]*metaRecord, 64),
	}

	// Warm the cache from bbolt for all entries. This is O(N) at startup
	// but N is bounded by the CAS cap (10 GB default) / average blob size.
	if err := idx.warm(); err != nil {
		db.Close()
		return nil, fmt.Errorf("resources: warm index cache: %w", err)
	}

	return idx, nil
}

// warm loads all records from bbolt into the in-memory cache.
func (i *Index) warm() error {
	return i.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketResources)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var rec metaRecord
			if err := json.Unmarshal(v, &rec); err != nil {
				// Corrupt record: skip rather than fail startup.
				return nil
			}
			key := string(k)
			i.cache[key] = &rec
			return nil
		})
	})
}

// Close persists any pending state and closes the bbolt handle.
func (i *Index) Close() error {
	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()
		return nil
	}
	i.closed = true
	db := i.db
	i.mu.Unlock()
	return db.Close()
}

// Get returns the metadata record for a hash. Returns nil, false if absent.
func (i *Index) Get(hash string) (*metaRecord, bool) {
	i.mu.RLock()
	rec := i.cache[hash]
	i.mu.RUnlock()
	if rec != nil {
		// Return a copy to prevent callers mutating the cached record.
		cp := *rec
		return &cp, true
	}
	return nil, false
}

// Put writes a metadata record to both bbolt and cache. Existing records
// with the same hash are overwritten.
func (i *Index) Put(rec *metaRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("resources: marshal meta record: %w", err)
	}

	key := []byte(rec.Hash)
	if err := i.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketResources)
		if b == nil {
			return errors.New("resources: index bucket missing")
		}
		return b.Put(key, data)
	}); err != nil {
		return fmt.Errorf("resources: write index: %w", err)
	}

	// Update cache with a copy.
	cp := *rec
	i.mu.Lock()
	i.cache[rec.Hash] = &cp
	i.mu.Unlock()

	return nil
}

// Delete removes a record from both bbolt and cache.
func (i *Index) Delete(hash string) error {
	key := []byte(hash)
	if err := i.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketResources)
		if b == nil {
			return errors.New("resources: index bucket missing")
		}
		return b.Delete(key)
	}); err != nil {
		return fmt.Errorf("resources: delete index: %w", err)
	}

	i.mu.Lock()
	delete(i.cache, hash)
	i.mu.Unlock()

	return nil
}

// All returns a snapshot of all metadata records. The returned slice is
// safe for caller mutation; the records are copies.
func (i *Index) All() []*metaRecord {
	i.mu.RLock()
	defer i.mu.RUnlock()

	out := make([]*metaRecord, 0, len(i.cache))
	for _, rec := range i.cache {
		cp := *rec
		out = append(out, &cp)
	}
	return out
}

// TotalSize returns the sum of blob sizes across all indexed entries.
func (i *Index) TotalSize() int64 {
	i.mu.RLock()
	defer i.mu.RUnlock()

	var total int64
	for _, rec := range i.cache {
		total += rec.Size
	}
	return total
}

// EntryCount returns the number of entries in the index.
func (i *Index) EntryCount() int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.cache)
}

// HashEntryPath returns the canonical on-disk path for a CAS entry,
// following the spec's sharded layout: <root>/<first2hex>/<next2hex>/<fullhash>/.
// The root directory must be the CAS store root.
func HashEntryPath(rootDir, hash string) string {
	if len(hash) < 4 {
		return filepath.Join(rootDir, "00", "00", hash)
	}
	return filepath.Join(rootDir, hash[:2], hash[2:4], hash)
}

// HashDataPath returns the path to the data file for a CAS entry.
func HashDataPath(rootDir, hash string) string {
	return filepath.Join(HashEntryPath(rootDir, hash), "data")
}

// HashMetaPath returns the path to the meta.json file for a CAS entry.
func HashMetaPath(rootDir, hash string) string {
	return filepath.Join(HashEntryPath(rootDir, hash), "meta.json")
}

// writeMetaToDisk writes the meta.json sidecar for a CAS entry atomically.
// Uses a unique temp file per call to avoid races when multiple goroutines
// write the same hash concurrently. If another goroutine already wrote the
// target (rename fails with ENOENT on the temp because it was already
// moved, or the target already exists), the write is considered successful
// since the content is identical for the same hash.
func writeMetaToDisk(metaPath string, rec *metaRecord) error {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("resources: marshal meta.json: %w", err)
	}

	dir := filepath.Dir(metaPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("resources: mkdir for meta.json: %w", err)
	}

	// Atomic write: unique temp file in same dir, then rename.
	tmp := metaPath + ".tmp." + randomSuffix()
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("resources: write meta.json temp: %w", err)
	}
	if err := os.Rename(tmp, metaPath); err != nil {
		os.Remove(tmp) // best-effort cleanup
		// If target already exists, a concurrent writer won. Since the
		// content is identical (same hash = same metadata), treat as
		// success.
		if _, statErr := os.Stat(metaPath); statErr == nil {
			return nil
		}
		return fmt.Errorf("resources: rename meta.json: %w", err)
	}
	return nil
}
