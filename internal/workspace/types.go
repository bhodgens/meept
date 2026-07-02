// Package workspace manages ephemeral working trees for dispatched jobs.
//
// The WorkspaceManager materializes a sender's repo state (commit + optional
// uncommitted-edit diff) into a local working tree at ~/.meept/worktrees/<jobID>/.
// The receiver's own working state is never touched — each job gets its own
// ephemeral git worktree on a dedicated branch (meept-job-<jobID>).
//
// Spec reference: docs/superpowers/specs/2026-07-01-cluster-resource-model-design.md §4.2, §5, §6.
package workspace

import (
	"context"
	"errors"
	"fmt"
)

// WorkspaceRef captures source-tree state at dispatch time.
// It is the union value carried in DispatchJob envelopes.
type WorkspaceRef struct {
	// RepoURL is the git remote URL (e.g. "https://github.com/org/repo.git")
	// or "peer:<nodeID>" for P2P fetch via WorkspaceService.GitFetch.
	// Empty string is invalid for Ensure but tolerated by Snapshot callers
	// who fill it in later.
	RepoURL string `json:"repo_url"`

	// CommitSHA is the 40-char (or shortened) git commit SHA the workspace
	// is pinned to. Empty string is invalid for Ensure.
	CommitSHA string `json:"commit_sha"`

	// DiffBlobHash is the CAS hash of the uncommitted-edit diff patch.
	// Empty when Dirty=false (clean tree). Resolved via PatchResolver on
	// the receiver side.
	DiffBlobHash string `json:"diff_blob_hash"`

	// Dirty is true when the source tree had uncommitted changes at snapshot
	// time. When true, DiffBlobHash must be non-empty for Ensure to apply
	// the patch.
	Dirty bool `json:"dirty"`
}

// WorkspaceManager is the interface defined in spec §4.2. Implementations
// must be safe for concurrent use.
type WorkspaceManager interface {
	// Ensure materializes the workspace described by ref:
	//   1. clone/fetch the commit from RepoURL,
	//   2. check out ephemeral branch meept-job-<jobID>,
	//   3. apply the dirty diff patch (if Dirty && DiffBlobHash != ""),
	// Returns the local working-tree path. Agent cwd will be set to this.
	Ensure(ctx context.Context, ref WorkspaceRef) (worktreePath string, err error)

	// Snapshot captures the current state of a local repo as a WorkspaceRef.
	// Called by the dispatcher on the sending side before dispatch.
	// If the tree is dirty and a PatchStore is configured, the diff is added
	// to CAS and DiffBlobHash is populated.
	Snapshot(ctx context.Context, repoPath string) (WorkspaceRef, error)

	// Close removes an ephemeral worktree after task completion.
	// Idempotent: a second call on the same path is a no-op.
	// Best-effort: errors during cleanup are logged but the method returns nil
	// unless the path still exists after attempted removal.
	Close(worktreePath string) error
}

// PatchStore is the CAS-facing interface for diff blobs. It mirrors the
// subset of ResourceManager (spec §4.1) needed for adding/retrieving patch
// files. ResourceManager itself is built in a parallel phase; this interface
// keeps the workspace package decoupled.
//
// Add is called by Snapshot when the tree is dirty: the generated patch file
// path is passed in and a content-addressed hash is returned.
// Resolve is called by Ensure to materialize the patch file from a hash.
type PatchStore interface {
	Add(ctx context.Context, srcPath string) (hash string, err error)
	Resolve(hash string) (localPath string, err error)
}

// MetricsEmitter mirrors the telemetry surface from spec §8. The workspace
// package emits workspace_materialize_ms and workspace_patch_conflicts.
// SetMetricsEmitter wires this in with a nil-guard.
type MetricsEmitter interface {
	ObserveWorkspaceMaterializeMS(ms int64)
	IncWorkspacePatchConflicts()
}

// Sentinel errors. Use errors.Is to distinguish; the *typed* errors below
// carry context for callers who need detail.
var (
	// ErrWorkspaceUnavailable is returned when the commit cannot be fetched
	// from shared origin or any peer. Wraps a WorkspaceUnavailable struct.
	ErrWorkspaceUnavailable = errors.New("workspace unavailable")

	// ErrPatchConflict is returned when the diff patch fails to apply.
	// Wraps a PatchConflict struct with details.
	ErrPatchConflict = errors.New("patch conflict")

	// ErrPeerFetchNotImplemented is returned by Ensure when RepoURL has the
	// "peer:" prefix. Peer-to-peer GitFetch is a later phase; until then the
	// fallback path returns this error so callers can distinguish "peer not
	// yet supported" from "clone failed for other reasons".
	ErrPeerFetchNotImplemented = errors.New("peer fetch not implemented in this phase")

	// ErrPatchStoreNotConfigured is returned by Snapshot when the tree is dirty
	// but no PatchStore has been wired via SetPatchStore.
	ErrPatchStoreNotConfigured = errors.New("patch store not configured")

	// ErrInvalidWorkspaceRef is returned by Ensure when required fields on
	// WorkspaceRef are empty/invalid (e.g. empty CommitSHA, empty RepoURL).
	ErrInvalidWorkspaceRef = errors.New("invalid workspace ref")

	// ErrNotAWorktree is returned by Close when the given path is not under
	// the configured worktree root (defence against arbitrary path deletion).
	ErrNotAWorktree = errors.New("path is not under worktree root")
)

// WorkspaceUnavailable carries detail about which commit/repo could not be
// materialized. Caller can errors.As to extract. Implements Is so that
// errors.Is(err, ErrWorkspaceUnavailable) returns true for any
// *WorkspaceUnavailable without requiring the Manager to double-wrap.
type WorkspaceUnavailable struct {
	Commit  string // the SHA that couldn't be fetched
	RepoURL string // the remote that was tried
	Err     error  // underlying cause (network, git exit code, etc.)
}

func (e *WorkspaceUnavailable) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("workspace unavailable: commit %s from %s: %v", e.Commit, e.RepoURL, e.Err)
	}
	return fmt.Sprintf("workspace unavailable: commit %s from %s", e.Commit, e.RepoURL)
}

// Is reports true if target is ErrWorkspaceUnavailable. This lets callers
// write errors.Is(err, ErrWorkspaceUnavailable) against any
// *WorkspaceUnavailable returned by the Manager without needing the Manager
// to wrap the sentinel explicitly.
func (e *WorkspaceUnavailable) Is(target error) bool {
	return target == ErrWorkspaceUnavailable
}

// Unwrap returns the underlying cause (if any).
func (e *WorkspaceUnavailable) Unwrap() error { return e.Err }

// PatchConflict carries detail about why a diff patch failed to apply.
// Like WorkspaceUnavailable, implements Is so errors.Is(err, ErrPatchConflict)
// works against any *PatchConflict.
type PatchConflict struct {
	Commit string // the base commit the patch was generated against
	Reason string // human-readable reason (e.g. "file deleted", "context mismatch")
	Err    error  // underlying git apply error
}

func (e *PatchConflict) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("patch conflict on commit %s: %s: %v", e.Commit, e.Reason, e.Err)
	}
	return fmt.Sprintf("patch conflict on commit %s: %s", e.Commit, e.Reason)
}

// Is reports true if target is ErrPatchConflict.
func (e *PatchConflict) Is(target error) bool {
	return target == ErrPatchConflict
}

func (e *PatchConflict) Unwrap() error { return e.Err }
