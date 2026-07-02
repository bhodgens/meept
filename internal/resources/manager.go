package resources

import (
	"context"
	"fmt"
	"log/slog"
)

// Manager implements ResourceManager by wrapping a CASStore. In this phase
// (Phase 1), Ensure is local-only: a cache miss returns ErrNotLocal. The
// fetch-from-peer wiring arrives with the gRPC transport phase.
//
// The manager handles ref-prefix routing: blake3/sha256 refs are resolved
// through the CAS; gitcommit/workspace refs return ErrNotLocal (handled by
// WorkspaceManager in another phase).
type Manager struct {
	store  *CASStore
	logger *slog.Logger

	// fetcher is the optional peer-fetch hook. When nil, Ensure misses
	// return ErrNotLocal. Wired by the cluster transport layer in a later
	// phase.
	fetcher PeerFetcher
}

// PeerFetcher is the interface for fetching a blob from a remote peer.
// It will be implemented by the gRPC transport layer. When the fetch
// succeeds, the blob is stored in the local CAS.
type PeerFetcher interface {
	// Fetch retrieves a blob from the cluster mesh and stores it in the
	// local CAS. Returns the local path. The source node is recorded in
	// the blob's metadata.
	Fetch(ctx context.Context, hashHex, algo string) (string, error)
}

// NewManager creates a ResourceManager backed by the given CASStore.
func NewManager(store *CASStore, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		store:  store,
		logger: logger,
	}
}

// SetPeerFetcher wires the cluster peer-fetch hook. Nil-safe.
func (m *Manager) SetPeerFetcher(pf PeerFetcher) {
	if pf != nil {
		m.fetcher = pf
	}
}

// Ensure resolves a ResourceRef to a local filesystem path.
//
// Ref-prefix routing:
//   - blake3:/sha256: → check CAS. Hit → return local path + increment refcount.
//     Miss → try PeerFetcher if wired; otherwise ErrNotLocal.
//   - gitcommit:/workspace: → ErrNotLocal (handled by WorkspaceManager).
//
// The caller MUST call Release(ref) when done with the resource to
// decrement the refcount.
func (m *Manager) Ensure(ctx context.Context, ref ResourceRef) (string, error) {
	algo, body, isCAS := ParseRef(ref.Raw)
	if !isCAS {
		m.logger.Debug("resources: ensure non-CAS ref", "ref", ref.Raw)
		return "", ErrNotLocal
	}

	// Check local CAS first.
	if m.store.Has(body) {
		path, err := m.store.GetPath(body)
		if err != nil {
			return "", err
		}
		m.store.IncrementRef(body)
		m.store.metrics.IncCASHits()
		m.logger.Debug("resources: ensure cache hit", "ref", ref.Raw, "path", path)
		return path, nil
	}

	// Cache miss.
	m.store.metrics.IncCASMisses()

	// Try peer fetch if wired.
	if m.fetcher != nil {
		path, err := m.fetcher.Fetch(ctx, body, algo)
		if err != nil {
			return "", fmt.Errorf("resources: peer fetch failed for %s: %w", ref.Raw, err)
		}
		// Fetch stored it locally; increment refcount.
		m.store.IncrementRef(body)
		m.logger.Debug("resources: ensure fetched from peer", "ref", ref.Raw, "path", path)
		return path, nil
	}

	// No peer fetcher wired (Phase 1).
	m.logger.Debug("resources: ensure miss, no peer fetcher", "ref", ref.Raw)
	return "", ErrNotLocal
}

// Release decrements the refcount on a previously-Ensured resource.
// Idempotent: calling on a non-tracked ref is a no-op.
func (m *Manager) Release(ref ResourceRef) {
	_, body, isCAS := ParseRef(ref.Raw)
	if !isCAS {
		return
	}
	m.store.DecrementRef(body)
}

// Add registers a local file in the CAS store, returning the hash with
// algorithm prefix (e.g. "blake3:abcd..."). Called by the dispatcher at
// send-time; never speculatively.
func (m *Manager) Add(ctx context.Context, srcPath string) (string, error) {
	hashHex, err := m.store.Add(ctx, srcPath)
	if err != nil {
		return "", err
	}
	algo := m.store.cfg.HashAlgorithm
	if algo == "" {
		algo = AlgoBlake3
	}
	return HashPrefix(algo, hashHex), nil
}

// Has returns true if the blob (identified by hash with or without algorithm
// prefix) is currently in the local store.
func (m *Manager) Has(hash string) bool {
	_, body, isCAS := ParseRef(hash)
	if isCAS {
		return m.store.Has(body)
	}
	// Unprefixed hash: assume it's a bare hash in the default algorithm.
	return m.store.Has(hash)
}

// Store returns the underlying CASStore. Used by callers that need direct
// access to CAS-level operations (e.g. the executor bridge for StoreBlob).
func (m *Manager) Store() *CASStore { return m.store }
