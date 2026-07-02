// Package resources implements the content-addressable storage (CAS) layer
// for the cluster resource model. Files entering CAS are immutable blobs
// addressed by their blake3 hash. The CAS is a transit cache, not a
// persistent store: blobs enter when an in-flight dispatched task references
// them and leave when refcount-driven eviction reclaims them.
//
// See docs/superpowers/specs/2026-07-01-cluster-resource-model-design.md
// sections 2.4, 4.1, 6, 7, 8, 9 for the design rationale.
package resources

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ResourceRef is the union type for resource identifiers. The Raw field
// carries a prefix that routes resolution: "blake3:" and "sha256:" hit the
// CAS; "gitcommit:" and "workspace:" are handled by the WorkspaceManager in
// a later phase.
type ResourceRef struct {
	Raw string // "blake3:...", "sha256:...", "gitcommit:...", "workspace:..."
}

// ResourceManager is the interface consumed by the executor bridge and
// dispatcher. In this phase, Ensure is local-only: a cache miss returns
// ErrNotLocal. Fetch-from-peer wiring arrives with the gRPC transport phase.
//
// (spec section 4.1)
type ResourceManager interface {
	// Ensure resolves a ref to a local filesystem path. Fetches over the
	// cluster mesh if absent locally. Refcount-aware: increment on fetch,
	// caller must call Release when done.
	Ensure(ctx context.Context, ref ResourceRef) (path string, err error)

	// Release decrements the refcount on a previously-Ensured resource.
	// Idempotent: calling on a non-tracked ref is a no-op.
	Release(ref ResourceRef)

	// Add registers a local file in the CAS store, returning its hash
	// (prefixed with the algorithm, e.g. "blake3:abcd..."). Called by the
	// dispatcher at send-time; never speculatively.
	Add(ctx context.Context, srcPath string) (hash string, err error)

	// Has returns true if the blob is currently in the local store.
	Has(hash string) bool
}

// Hash algorithms supported by the CAS.
const (
	AlgoBlake3 = "blake3"
	AlgoSHA256 = "sha256"
)

// Ref prefixes matching the hash algorithms above. Non-CAS prefixes return
// ErrNotLocal in this phase.
const (
	prefixBlake3    = "blake3:"
	prefixSHA256    = "sha256:"
	prefixGitCommit = "gitcommit:"
	prefixWorkspace = "workspace:"
)

// Errors (spec section 6, resource-layer).
var (
	// ErrResourceUnavailable indicates no peer can supply the blob.
	ErrResourceUnavailable = errors.New("resources: unavailable")

	// ErrResourceCorrupt indicates a hash mismatch on receive that
	// persisted across retries. The wrapped value is ResourceCorrupt.
	ErrResourceCorrupt = errors.New("resources: corrupt")

	// ErrCacheFull indicates the CAS store is at capacity and eviction
	// could not reclaim enough space (all remaining entries are pinned or
	// referenced).
	ErrCacheFull = errors.New("resources: cache full")

	// ErrNotLocal indicates the ref is not a CAS-managed blob (e.g.
	// gitcommit/workspace refs, or a blake3/sha256 ref that is not present
	// locally in this phase).
	ErrNotLocal = errors.New("resources: not local")
)

// ResourceUnavailable carries diagnostic context for ErrResourceUnavailable.
type ResourceUnavailable struct {
	Hash       string
	SourceNode string
}

func (e *ResourceUnavailable) Error() string {
	return fmt.Sprintf("resources: unavailable (hash=%s, source=%s)", e.Hash, e.SourceNode)
}

func (e *ResourceUnavailable) Unwrap() error { return ErrResourceUnavailable }

// ResourceCorrupt carries diagnostic context for ErrResourceCorrupt.
type ResourceCorrupt struct {
	Hash       string
	SourceNode string
}

func (e *ResourceCorrupt) Error() string {
	return fmt.Sprintf("resources: corrupt (hash=%s, source=%s)", e.Hash, e.SourceNode)
}

func (e *ResourceCorrupt) Unwrap() error { return ErrResourceCorrupt }

// ParseRef splits a ResourceRef.Raw into algorithm and hash body.
// Returns the algorithm string (without colon), the hash body, and true if
// the prefix is a CAS-managed hash type (blake3/sha256). Returns false for
// non-CAS prefixes (gitcommit, workspace) and unrecognised prefixes.
func ParseRef(raw string) (algo, body string, isCAS bool) {
	switch {
	case strings.HasPrefix(raw, prefixBlake3):
		return AlgoBlake3, strings.TrimPrefix(raw, prefixBlake3), true
	case strings.HasPrefix(raw, prefixSHA256):
		return AlgoSHA256, strings.TrimPrefix(raw, prefixSHA256), true
	default:
		if strings.HasPrefix(raw, prefixGitCommit) {
			return "gitcommit", strings.TrimPrefix(raw, prefixGitCommit), false
		}
		if strings.HasPrefix(raw, prefixWorkspace) {
			return "workspace", strings.TrimPrefix(raw, prefixWorkspace), false
		}
		return "", "", false
	}
}

// HashPrefix returns the canonical ref string for a hash body under the
// given algorithm (e.g. ("blake3", "abcd...") -> "blake3:abcd...").
func HashPrefix(algo, body string) string {
	return algo + ":" + body
}

// MetricsEmitter is the optional telemetry sink for CAS operations
// (spec section 8). All methods must be safe for concurrent calls.
// Implementations are nil-guarded by the store: a nil emitter is a no-op.
type MetricsEmitter interface {
	IncCASHits()
	IncCASMisses()
	IncCASBytesFetched(n int64)
	IncCASBytesEvicted(n int64)
	IncCASRefcountZeroEligible()
}

// nopMetricsEmitter is the default sink when none is wired.
type nopMetricsEmitter struct{}

func (nopMetricsEmitter) IncCASHits()                 {}
func (nopMetricsEmitter) IncCASMisses()               {}
func (nopMetricsEmitter) IncCASBytesFetched(n int64)  {}
func (nopMetricsEmitter) IncCASBytesEvicted(n int64)  {}
func (nopMetricsEmitter) IncCASRefcountZeroEligible() {}
