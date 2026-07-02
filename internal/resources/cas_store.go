package resources

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CASConfig holds runtime configuration for the CAS store.
type CASConfig struct {
	// StoreDir is the root directory for CAS blobs
	// (default: ~/.meept/resources).
	StoreDir string

	// CapacityBytes is the maximum total size of all blobs in the store.
	// Zero means unlimited (no cap-driven eviction).
	CapacityBytes int64

	// EvictionSweepInterval controls how often the background eviction
	// sweep runs. Zero disables the background sweep (caller can still
	// call Evict manually).
	EvictionSweepInterval time.Duration

	// PinnedHashes is the set of operator-pinned blob hashes that are
	// exempt from eviction.
	PinnedHashes []string

	// HashAlgorithm is the default algorithm for new Adds
	// ("blake3" or "sha256"). Default "blake3".
	HashAlgorithm string
}

// DefaultCASConfig returns the spec-default configuration.
func DefaultCASConfig() CASConfig {
	home := os.Getenv("HOME")
	if home == "" {
		home = "/tmp"
	}
	return CASConfig{
		StoreDir:              filepath.Join(home, ".meept", "resources"),
		CapacityBytes:         10 * 1024 * 1024 * 1024, // 10 GB
		EvictionSweepInterval: 5 * time.Minute,
		PinnedHashes:          nil,
		HashAlgorithm:         AlgoBlake3,
	}
}

// CASStore implements the content-addressable storage layer. It is safe
// for concurrent use. All disk I/O is performed outside of locks to comply
// with the project's mutex-scope rule.
type CASStore struct {
	cfg     CASConfig
	index   *Index
	logger  *slog.Logger
	metrics MetricsEmitter

	// pinnedSet is a lookup-optimised copy of cfg.PinnedHashes.
	pinnedSet map[string]bool

	// sweepCancel stops the background eviction sweep.
	sweepCancel context.CancelFunc
	sweepWg     sync.WaitGroup
}

// NewCASStore opens or creates a CAS store at the configured location.
// The parent directory of StoreDir must exist or be creatable.
func NewCASStore(cfg CASConfig, logger *slog.Logger) (*CASStore, error) {
	if logger == nil {
		logger = slog.Default()
	}

	if cfg.StoreDir == "" {
		return nil, errors.New("resources: store dir is empty")
	}
	if cfg.HashAlgorithm == "" {
		cfg.HashAlgorithm = AlgoBlake3
	}

	// Ensure store directory exists.
	if err := os.MkdirAll(cfg.StoreDir, 0o700); err != nil {
		return nil, fmt.Errorf("resources: create store dir: %w", err)
	}

	// Open the bbolt index in the store directory.
	indexPath := filepath.Join(cfg.StoreDir, "index.db")
	index, err := NewIndex(indexPath)
	if err != nil {
		return nil, fmt.Errorf("resources: open index: %w", err)
	}

	pinnedSet := make(map[string]bool, len(cfg.PinnedHashes))
	for _, h := range cfg.PinnedHashes {
		pinnedSet[h] = true
	}

	store := &CASStore{
		cfg:       cfg,
		index:     index,
		logger:    logger,
		metrics:   nopMetricsEmitter{},
		pinnedSet: pinnedSet,
	}

	// Apply pinned-from-config to existing entries.
	store.applyConfigPins()

	return store, nil
}

// SetMetricsEmitter sets the telemetry sink. Nil-safe via nopMetricsEmitter.
func (s *CASStore) SetMetricsEmitter(em MetricsEmitter) {
	if em != nil {
		s.metrics = em
	}
}

// applyConfigPins marks any existing entries whose hashes are in the
// configured pinned set as pinned in the index.
func (s *CASStore) applyConfigPins() {
	for hash := range s.pinnedSet {
		if rec, ok := s.index.Get(hash); ok {
			if !rec.Pinned {
				rec.Pinned = true
				_ = s.index.Put(rec)
			}
		}
	}
}

// Close stops the background sweep (if running) and closes the index.
func (s *CASStore) Close() error {
	if s.sweepCancel != nil {
		s.sweepCancel()
		s.sweepWg.Wait()
	}
	return s.index.Close()
}

// StartSweep launches a background goroutine that periodically runs eviction.
// Calling more than once is a no-op.
func (s *CASStore) StartSweep(parentCtx context.Context) {
	if s.cfg.EvictionSweepInterval <= 0 || s.sweepCancel != nil {
		return
	}

	ctx, cancel := context.WithCancel(parentCtx)
	s.sweepCancel = cancel

	s.sweepWg.Add(1)
	go func() {
		defer s.sweepWg.Done()
		ticker := time.NewTicker(s.cfg.EvictionSweepInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n, err := s.Evict(ctx)
				if err != nil {
					s.logger.Warn("resources: eviction sweep error", "err", err)
				}
				if n > 0 {
					s.logger.Debug("resources: eviction sweep reclaimed entries", "count", n)
				}
			}
		}
	}()
}

// Add registers a local file in the CAS store. It computes the hash,
// copies the file into the sharded layout, and writes the metadata record.
// If the blob already exists (same hash), it is not re-copied.
//
// Returns the hex hash body (without algorithm prefix).
func (s *CASStore) Add(ctx context.Context, srcPath string) (string, error) {
	// 1. Hash the source file (disk I/O, no lock held).
	algo := s.cfg.HashAlgorithm
	if algo == "" {
		algo = AlgoBlake3
	}
	hashHex, err := HashFile(srcPath, algo)
	if err != nil {
		return "", fmt.Errorf("resources: hash source: %w", err)
	}

	// Check if already present in index.
	if rec, ok := s.index.Get(hashHex); ok {
		// Already stored. Verify data file exists on disk.
		dataPath := HashDataPath(s.cfg.StoreDir, hashHex)
		if _, err := os.Stat(dataPath); err == nil {
			s.logger.Debug("resources: add cache hit", "hash", hashHex, "size", rec.Size)
			return hashHex, nil
		}
		// Index says present but data is missing — fall through to re-add.
		s.logger.Warn("resources: index present but data file missing, re-adding", "hash", hashHex)
	}

	// 2. Get file size.
	info, err := os.Stat(srcPath)
	if err != nil {
		return "", fmt.Errorf("resources: stat source: %w", err)
	}

	// 3. Check capacity; trigger eviction if over cap.
	if s.cfg.CapacityBytes > 0 {
		if err := s.ensureCapacity(ctx, info.Size()); err != nil {
			return "", err
		}
	}

	// 4. Copy file into sharded path (POSIX atomic via .part + rename).
	dataPath := HashDataPath(s.cfg.StoreDir, hashHex)
	if err := s.atomicCopy(srcPath, dataPath); err != nil {
		return "", fmt.Errorf("resources: copy blob: %w", err)
	}

	// 5. Write meta.json sidecar and index record.
	now := time.Now().UTC()
	rec := &metaRecord{
		Hash:         hashHex,
		OriginalName: filepath.Base(srcPath),
		Size:         info.Size(),
		AddedAt:      now,
		Refcount:     0, // caller controls via IncrementRef
		Pinned:       s.pinnedSet[hashHex],
		Source:       "local",
	}

	if err := writeMetaToDisk(HashMetaPath(s.cfg.StoreDir, hashHex), rec); err != nil {
		os.Remove(dataPath) // best-effort cleanup
		return "", fmt.Errorf("resources: write meta: %w", err)
	}

	if err := s.index.Put(rec); err != nil {
		os.Remove(dataPath)
		os.Remove(HashMetaPath(s.cfg.StoreDir, hashHex))
		return "", fmt.Errorf("resources: write index: %w", err)
	}

	s.logger.Debug("resources: added blob", "hash", hashHex, "size", info.Size(), "algo", algo)
	return hashHex, nil
}

// atomicCopy copies src to dst via a temp .part file and os.Rename.
// If multiple goroutines race on the same dst, the last rename wins
// (POSIX atomic) and the loser's bytes are unlinked. This is the
// concurrent-fetch recovery strategy from spec section 6.
func (s *CASStore) atomicCopy(src, dst string) error {
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	partPath := dst + ".part." + randomSuffix()
	defer os.Remove(partPath) // best-effort cleanup if rename succeeded or not

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(partPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create part: %w", err)
	}

	buf := make([]byte, 64*1024)
	if _, err := io.CopyBuffer(out, in, buf); err != nil {
		out.Close()
		return fmt.Errorf("copy: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close part: %w", err)
	}

	if err := os.Rename(partPath, dst); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Has returns true if the blob is currently in the local store (index has
// the record and the data file exists on disk).
func (s *CASStore) Has(hashHex string) bool {
	_, ok := s.index.Get(hashHex)
	if !ok {
		return false
	}
	dataPath := HashDataPath(s.cfg.StoreDir, hashHex)
	if _, err := os.Stat(dataPath); err != nil {
		s.logger.Debug("resources: has but data missing", "hash", hashHex)
		return false
	}
	return true
}

// GetPath returns the filesystem path to the blob's data file.
// Returns an error if the blob is not in the store.
func (s *CASStore) GetPath(hashHex string) (string, error) {
	if !s.Has(hashHex) {
		return "", ErrNotLocal
	}
	return HashDataPath(s.cfg.StoreDir, hashHex), nil
}

// IncrementRef atomically increments the refcount for a blob. No-op if the
// blob is not in the index.
func (s *CASStore) IncrementRef(hashHex string) {
	rec, ok := s.index.Get(hashHex)
	if !ok {
		return
	}
	rec.Refcount++
	_ = s.index.Put(rec)
}

// DecrementRef atomically decrements the refcount for a blob, flooring at
// zero. When refcount transitions from 1 to 0, the blob becomes eligible
// for eviction and a telemetry signal is emitted.
func (s *CASStore) DecrementRef(hashHex string) {
	rec, ok := s.index.Get(hashHex)
	if !ok {
		return
	}
	if rec.Refcount == 0 {
		// Already zero; no-op, no telemetry.
		return
	}
	wasPositive := rec.Refcount > 0
	rec.Refcount--
	_ = s.index.Put(rec)

	// Fire telemetry only on the 1→0 transition.
	if wasPositive && rec.Refcount == 0 {
		s.metrics.IncCASRefcountZeroEligible()
	}
}

// Pin marks a blob as exempt from eviction.
func (s *CASStore) Pin(hashHex string) {
	rec, ok := s.index.Get(hashHex)
	if !ok {
		return
	}
	rec.Pinned = true
	_ = s.index.Put(rec)
}

// Unpin removes the eviction exemption from a blob.
func (s *CASStore) Unpin(hashHex string) {
	rec, ok := s.index.Get(hashHex)
	if !ok {
		return
	}
	rec.Pinned = false
	_ = s.index.Put(rec)
}

// IsPinned returns true if the blob is pinned (either by config or by Pin()).
func (s *CASStore) IsPinned(hashHex string) bool {
	if s.pinnedSet[hashHex] {
		return true
	}
	rec, ok := s.index.Get(hashHex)
	return ok && rec.Pinned
}

// Refcount returns the current refcount for a blob, or 0 if not tracked.
func (s *CASStore) Refcount(hashHex string) int {
	rec, ok := s.index.Get(hashHex)
	if !ok {
		return 0
	}
	return rec.Refcount
}

// StoreDir returns the root directory of the CAS store.
func (s *CASStore) StoreDir() string { return s.cfg.StoreDir }

// CapacityBytes returns the configured capacity limit.
func (s *CASStore) CapacityBytes() int64 { return s.cfg.CapacityBytes }

// TotalSize returns the total size of all blobs currently in the store.
func (s *CASStore) TotalSize() int64 { return s.index.TotalSize() }

// ensureCapacity checks if adding a blob of incomingBytes would exceed the
// configured cap. If so, it triggers an eviction sweep. If the sweep cannot
// free enough space, it returns ErrCacheFull.
func (s *CASStore) ensureCapacity(ctx context.Context, incomingBytes int64) error {
	current := s.index.TotalSize()
	if current+incomingBytes <= s.cfg.CapacityBytes {
		return nil
	}

	// Trigger eviction to make room.
	_, err := s.Evict(ctx)
	if err != nil {
		return err
	}

	// Re-check after eviction.
	current = s.index.TotalSize()
	if current+incomingBytes > s.cfg.CapacityBytes {
		return ErrCacheFull
	}
	return nil
}

// VerifyBlob re-hashes the data file and compares against the expected hash.
// Used to detect corruption and to verify transferred blobs.
func (s *CASStore) VerifyBlob(hashHex, algo string) error {
	dataPath := HashDataPath(s.cfg.StoreDir, hashHex)
	actual, err := HashFile(dataPath, algo)
	if err != nil {
		return err
	}
	if actual != hashHex {
		return &ResourceCorrupt{Hash: hashHex, SourceNode: "self"}
	}
	return nil
}

// StoreBlob writes raw bytes into the CAS for a known hash, bypassing the
// hash computation. This is the receive path for fetch-from-peer: the caller
// has already streamed the bytes and verified the hash. The blob is added
// with refcount=0 (caller calls IncrementRef if needed).
func (s *CASStore) StoreBlob(ctx context.Context, hashHex string, data []byte, source string) error {
	// Check if already present.
	if s.Has(hashHex) {
		return nil
	}

	// Check capacity.
	if s.cfg.CapacityBytes > 0 {
		if err := s.ensureCapacity(ctx, int64(len(data))); err != nil {
			return err
		}
	}

	dataPath := HashDataPath(s.cfg.StoreDir, hashHex)
	dir := filepath.Dir(dataPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("resources: mkdir for blob: %w", err)
	}

	// Write via .part + rename for POSIX atomicity (spec section 6).
	partPath := dataPath + ".part." + randomSuffix()
	if err := os.WriteFile(partPath, data, 0o600); err != nil {
		os.Remove(partPath)
		return fmt.Errorf("resources: write blob part: %w", err)
	}
	if err := os.Rename(partPath, dataPath); err != nil {
		os.Remove(partPath)
		return fmt.Errorf("resources: rename blob: %w", err)
	}

	rec := &metaRecord{
		Hash:         hashHex,
		OriginalName: "",
		Size:         int64(len(data)),
		AddedAt:      time.Now().UTC(),
		Refcount:     0,
		Pinned:       s.pinnedSet[hashHex],
		Source:       source,
	}

	if err := writeMetaToDisk(HashMetaPath(s.cfg.StoreDir, hashHex), rec); err != nil {
		os.Remove(dataPath)
		return err
	}
	if err := s.index.Put(rec); err != nil {
		os.Remove(dataPath)
		os.Remove(HashMetaPath(s.cfg.StoreDir, hashHex))
		return err
	}

	s.metrics.IncCASBytesFetched(int64(len(data)))
	return nil
}

// randomSuffix generates a short random hex string for temp-file uniqueness.
func randomSuffix() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(b)
}
