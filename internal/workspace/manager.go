package workspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/pkg/id"
)

// Config holds workspace manager configuration. Mirrors the cluster.workspace
// block from spec §9.
type Config struct {
	// WorktreeRoot is where ephemeral worktrees live. Default "~/.meept/worktrees".
	WorktreeRoot string `json:"worktree_root"`

	// GitFallbackToPeer controls whether Ensure attempts peer GitFetch when
	// shared-origin fetch fails. true (default) → attempt peer (returns
	// ErrPeerFetchNotImplemented in this phase since peer transport isn't wired).
	// false → fail immediately on origin fetch failure.
	GitFallbackToPeer bool `json:"git_fallback_to_peer"`
}

// DefaultConfig returns the spec-default configuration.
func DefaultConfig() Config {
	return Config{
		WorktreeRoot:      "~/.meept/worktrees",
		GitFallbackToPeer: true,
	}
}

// Manager implements WorkspaceManager. It is safe for concurrent use.
// All I/O (git subprocess, filesystem) happens OUTSIDE any mutex — the
// mutex only guards the in-memory openWorktrees map (snapshot-under-lock,
// release, then operate, per CLAUDE.md mutex-scope rule).
type Manager struct {
	cfg    Config
	logger *slog.Logger

	// patchStore is wired by SetPatchStore (sender-side; Snapshot uses Add).
	// May be nil if Snapshot is never called with a dirty tree.
	patchStore PatchStore

	// patchResolver is wired by SetPatchResolver (receiver-side; Ensure uses
	// Resolve). May be nil if Ensure is never called with a dirty ref.
	patchResolver PatchResolver

	// metrics is wired by SetMetricsEmitter. May be nil; emit helpers no-op.
	metrics MetricsEmitter

	mu            sync.Mutex
	openWorktrees map[string]struct{} // worktreePath -> {} for idempotent Close
}

// Option configures a Manager at construction time.
type Option func(*Manager)

// WithLogger sets the structured logger. Defaults to slog.Default().
func WithLogger(l *slog.Logger) Option {
	return func(m *Manager) {
		if l != nil {
			m.logger = l
		}
	}
}

// WithPatchStore wires the CAS-side patch store (sender path).
func WithPatchStore(ps PatchStore) Option {
	return func(m *Manager) {
		if ps != nil {
			m.patchStore = ps
		}
	}
}

// WithPatchResolver wires the CAS-side patch resolver (receiver path).
func WithPatchResolver(pr PatchResolver) Option {
	return func(m *Manager) {
		if pr != nil {
			m.patchResolver = pr
		}
	}
}

// WithMetricsEmitter wires the telemetry emitter.
func WithMetricsEmitter(me MetricsEmitter) Option {
	return func(m *Manager) {
		if me != nil {
			m.metrics = me
		}
	}
}

// NewManager constructs a Manager with the given config and options.
// The returned Manager is ready to call Ensure/Snapshot/Close on.
func NewManager(cfg Config, opts ...Option) *Manager {
	m := &Manager{
		cfg:           cfg,
		logger:        slog.Default(),
		openWorktrees: make(map[string]struct{}),
	}
	for _, opt := range opts {
		opt(m)
	}
	// Expand ~ in worktree root for filesystem operations.
	m.cfg.WorktreeRoot = expandHome(m.cfg.WorktreeRoot)
	return m
}

// SetPatchStore sets the CAS patch store. Nil-guarded per CLAUDE.md.
func (m *Manager) SetPatchStore(ps PatchStore) {
	if ps != nil {
		m.patchStore = ps
	}
}

// SetPatchResolver sets the CAS patch resolver. Nil-guarded per CLAUDE.md.
func (m *Manager) SetPatchResolver(pr PatchResolver) {
	if pr != nil {
		m.patchResolver = pr
	}
}

// SetMetricsEmitter sets the telemetry emitter. Nil-guarded per CLAUDE.md.
func (m *Manager) SetMetricsEmitter(me MetricsEmitter) {
	if me != nil {
		m.metrics = me
	}
}

// Ensure materializes the workspace described by ref.
//
// Algorithm (spec §5 Phase 3):
//  1. Validate ref (non-empty RepoURL, CommitSHA).
//  2. If RepoURL is "peer:<nodeID>", return ErrPeerFetchNotImplemented.
//  3. Generate a job ID; worktree path = cfg.WorktreeRoot/<jobID>.
//  4. git clone <RepoURL> <worktreePath>.
//  5. git checkout -b meept-job-<jobID> <CommitSHA>.
//  6. If Dirty && DiffBlobHash != "": resolve patch, apply (--check then apply).
//  7. Record path in openWorktrees; return path.
func (m *Manager) Ensure(ctx context.Context, ref WorkspaceRef) (string, error) {
	start := time.Now()
	defer func() {
		m.emitMaterializeMS(time.Since(start).Milliseconds())
	}()

	if err := validateRef(ref); err != nil {
		return "", err
	}

	if strings.HasPrefix(ref.RepoURL, "peer:") {
		return "", ErrPeerFetchNotImplemented
	}

	jobID := id.Generate("job-")
	worktreePath, err := safeJoinWorktree(m.cfg.WorktreeRoot, jobID)
	if err != nil {
		return "", err
	}

	// Ensure parent exists.
	if err := os.MkdirAll(m.cfg.WorktreeRoot, 0o755); err != nil {
		return "", fmt.Errorf("ensure worktree root: %w", err)
	}

	m.logger.Info("workspace: materializing",
		"job_id", jobID,
		"repo_url", ref.RepoURL,
		"commit", ref.CommitSHA,
		"dirty", ref.Dirty,
		"worktree_path", worktreePath,
	)

	// Clone. We use clone-then-checkout (rather than worktree add) because
	// the receiver may not have the origin repo locally at all.
	if err := gitClone(ctx, ref.RepoURL, worktreePath); err != nil {
		return "", &WorkspaceUnavailable{
			Commit:  ref.CommitSHA,
			RepoURL: ref.RepoURL,
			Err:     fmt.Errorf("clone: %w", err),
		}
	}

	// Checkout ephemeral branch at the requested commit.
	branchName := "meept-job-" + jobID
	if err := gitFetchAndCheckout(ctx, worktreePath, branchName, ref.CommitSHA); err != nil {
		// Cleanup partial clone before returning.
		_ = os.RemoveAll(worktreePath)
		return "", &WorkspaceUnavailable{
			Commit:  ref.CommitSHA,
			RepoURL: ref.RepoURL,
			Err:     fmt.Errorf("checkout: %w", err),
		}
	}

	// Apply dirty patch if present.
	if ref.Dirty && ref.DiffBlobHash != "" {
		if err := m.applyDirtyPatch(ctx, worktreePath, ref); err != nil {
			// Cleanup partial clone.
			_ = os.RemoveAll(worktreePath)
			return "", err
		}
	}

	// Record under lock (no I/O under lock — just map insert).
	m.mu.Lock()
	m.openWorktrees[worktreePath] = struct{}{}
	m.mu.Unlock()

	m.logger.Info("workspace: materialized",
		"job_id", jobID,
		"worktree_path", worktreePath,
	)
	return worktreePath, nil
}

// applyDirtyPatch resolves the diff blob via PatchResolver and applies it.
// Emits workspace_patch_conflicts on failure.
func (m *Manager) applyDirtyPatch(ctx context.Context, worktreePath string, ref WorkspaceRef) error {
	if m.patchResolver == nil {
		return fmt.Errorf("dirty patch requested (hash %s) but no patch resolver configured: %w",
			ref.DiffBlobHash, ErrPatchStoreNotConfigured)
	}

	patchPath, err := resolvePatchToTempFile(ctx, m.patchResolver, worktreePath, ref.DiffBlobHash)
	if err != nil {
		return err
	}
	defer os.Remove(patchPath)

	if err := applyPatch(ctx, worktreePath, patchPath); err != nil {
		m.incPatchConflicts()
		// Enrich the conflict with the commit SHA for caller introspection.
		// applyPatch wraps *PatchConflict via fmt.Errorf, so use errors.As.
		var pc *PatchConflict
		if errors.As(err, &pc) {
			pc.Commit = ref.CommitSHA
		}
		return err
	}
	m.logger.Debug("workspace: applied dirty patch",
		"commit", ref.CommitSHA,
		"diff_hash", ref.DiffBlobHash,
	)
	return nil
}

// Snapshot captures the current state of a local repo as a WorkspaceRef.
//
// Algorithm (spec §5 Phase 1):
//  1. git rev-parse HEAD → CommitSHA.
//  2. git status --porcelain → if empty, Dirty=false, DiffBlobHash="".
//  3. If dirty: git diff HEAD → patch bytes → temp file → PatchStore.Add → DiffBlobHash.
//  4. RepoURL is NOT determined here (caller fills it in from project config).
func (m *Manager) Snapshot(ctx context.Context, repoPath string) (WorkspaceRef, error) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return WorkspaceRef{}, fmt.Errorf("snapshot: abs path: %w", err)
	}

	commitSHA, err := gitHeadSHA(ctx, absPath)
	if err != nil {
		return WorkspaceRef{}, fmt.Errorf("snapshot: rev-parse HEAD: %w", err)
	}

	dirty, err := gitIsDirty(ctx, absPath)
	if err != nil {
		return WorkspaceRef{}, fmt.Errorf("snapshot: status: %w", err)
	}

	ref := WorkspaceRef{
		CommitSHA: commitSHA,
		Dirty:     dirty,
	}

	if !dirty {
		m.logger.Debug("workspace: snapshot clean",
			"repo", absPath,
			"commit", commitSHA,
		)
		return ref, nil
	}

	// Dirty: generate diff, add to CAS via PatchStore.
	if m.patchStore == nil {
		return WorkspaceRef{}, fmt.Errorf("snapshot: dirty tree but no patch store: %w",
			ErrPatchStoreNotConfigured)
	}

	diffBytes, err := gitDiffHEAD(ctx, absPath)
	if err != nil {
		return WorkspaceRef{}, fmt.Errorf("snapshot: diff HEAD: %w", err)
	}
	if len(diffBytes) == 0 {
		// git status --porcelain reported changes but git diff HEAD is empty.
		// This happens when the only changes are untracked files (which git
		// diff doesn't include). Treat as clean for dispatch purposes — the
		// receiver won't get untracked files, but the commit SHA is still
		// valid and the worktree is materializable.
		m.logger.Warn("workspace: snapshot: status dirty but diff empty (untracked files only?), treating as clean",
			"repo", absPath,
			"commit", commitSHA,
		)
		ref.Dirty = false
		return ref, nil
	}

	// Write diff to temp file (inside repo dir so it's on the same volume),
	// add to CAS, then remove the temp file.
	patchPath, err := writePatchFile(absPath, diffBytes)
	if err != nil {
		return WorkspaceRef{}, fmt.Errorf("snapshot: write patch: %w", err)
	}
	defer os.Remove(patchPath)

	hash, err := m.patchStore.Add(ctx, patchPath)
	if err != nil {
		return WorkspaceRef{}, fmt.Errorf("snapshot: patch store add: %w", err)
	}
	ref.DiffBlobHash = hash

	m.logger.Info("workspace: snapshot dirty",
		"repo", absPath,
		"commit", commitSHA,
		"diff_hash", hash,
		"diff_bytes", len(diffBytes),
	)
	return ref, nil
}

// Close removes an ephemeral worktree. Idempotent: calling Close twice on
// the same path is a no-op. Best-effort: if git worktree remove fails, falls
// back to rm -rf. Returns nil once the path no longer exists.
//
// Close does NOT commit or push — promotion of workspace changes is an
// orchestrator policy decision (spec §5 Phase 5, §4.2 comment).
func (m *Manager) Close(worktreePath string) error {
	// Defence: only touch paths under our configured root.
	if !isSubPath(m.cfg.WorktreeRoot, worktreePath) {
		return fmt.Errorf("close: %w: %s not under %s",
			ErrNotAWorktree, worktreePath, m.cfg.WorktreeRoot)
	}

	// Snapshot-and-release: check membership, delete entry, release.
	// No I/O under lock.
	m.mu.Lock()
	_, isOpen := m.openWorktrees[worktreePath]
	delete(m.openWorktrees, worktreePath)
	m.mu.Unlock()

	if !isOpen {
		// Already closed or never opened by this manager.
		// Still best-effort remove the path if it exists on disk.
		return nil
	}

	abs, err := filepath.Abs(worktreePath)
	if err != nil {
		return fmt.Errorf("close: abs: %w", err)
	}

	// If path doesn't exist, we're done (idempotent).
	if _, err := os.Stat(abs); os.IsNotExist(err) {
		return nil
	}

	ctx := context.Background()

	// Best-effort git worktree remove. This cleans up .git/worktrees/<name>
	// admin files in the main repo. Failure is tolerated (worktree may have
	// been cloned rather than worktree-added, so admin dir doesn't apply).
	if err := gitWorktreeRemove(ctx, filepath.Dir(abs), abs); err != nil {
		m.logger.Debug("workspace: close: git worktree remove failed (falling back to rm -rf)",
			"path", abs,
			"error", err,
		)
	}

	// rm -rf the path itself.
	if err := os.RemoveAll(abs); err != nil {
		return fmt.Errorf("close: remove %s: %w", abs, err)
	}

	m.logger.Info("workspace: closed",
		"worktree_path", abs,
	)
	return nil
}

// --- Helpers ---

// validateRef returns ErrInvalidWorkspaceRef if required fields are missing.
func validateRef(ref WorkspaceRef) error {
	if ref.RepoURL == "" {
		return fmt.Errorf("%w: empty repo_url", ErrInvalidWorkspaceRef)
	}
	if ref.CommitSHA == "" {
		return fmt.Errorf("%w: empty commit_sha", ErrInvalidWorkspaceRef)
	}
	return nil
}

// emitMaterializeMS emits the materialize latency metric if a MetricsEmitter
// is wired. No-op otherwise.
func (m *Manager) emitMaterializeMS(ms int64) {
	if m.metrics != nil {
		m.metrics.ObserveWorkspaceMaterializeMS(ms)
	}
}

// incPatchConflicts increments the patch-conflict counter if a MetricsEmitter
// is wired. No-op otherwise.
func (m *Manager) incPatchConflicts() {
	if m.metrics != nil {
		m.metrics.IncWorkspacePatchConflicts()
	}
}

// expandHome replaces a leading ~ with the user's home directory.
// Returns the input unchanged if no ~ prefix is present or if home dir
// lookup fails (defensive — the caller's default paths all use ~).
func expandHome(p string) string {
	if p == "" || !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	// ~user/ — we don't resolve other users; return as-is.
	return p
}
