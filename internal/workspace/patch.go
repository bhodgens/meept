package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// writePatchFile writes diffBytes to a temp file inside dir and returns the
// path. The caller MUST os.Remove the returned path when done with it (the
// manager defers this). Temp file is created inside dir (not os.TempDir())
// so that `git apply` operates on a path relative to the repo root — this
// avoids edge cases with symlinks in /tmp on macOS.
func writePatchFile(dir string, diffBytes []byte) (string, error) {
	if len(diffBytes) == 0 {
		return "", fmt.Errorf("writePatchFile: empty diff")
	}
	f, err := os.CreateTemp(dir, ".meept-patch-*.diff")
	if err != nil {
		return "", fmt.Errorf("writePatchFile: create temp: %w", err)
	}
	if _, err := f.Write(diffBytes); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("writePatchFile: write: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("writePatchFile: close: %w", err)
	}
	return f.Name(), nil
}

// applyPatch checks then applies a patch file to the repo at dir. Returns
// nil on success. On failure returns a *PatchConflict which implements Is
// to match ErrPatchConflict, so callers can use errors.Is for detection and
// errors.As for detail extraction.
//
// applyPatch does NOT remove the patch file — the caller defers cleanup so
// that error paths can inspect/log the patch contents.
func applyPatch(ctx context.Context, dir, patchPath string) error {
	// Dry-run first: detects conflicts without mutating the tree.
	if err := gitApplyCheck(ctx, dir, patchPath); err != nil {
		return &PatchConflict{
			Commit: "", // caller fills in if it has the SHA
			Reason: "git apply --check failed: patch does not apply cleanly",
			Err:    err,
		}
	}
	if err := gitApply(ctx, dir, patchPath); err != nil {
		return &PatchConflict{
			Commit: "",
			Reason: "git apply failed after successful --check (concurrent modification?)",
			Err:    err,
		}
	}
	return nil
}

// resolvePatchToTempFile resolves a DiffBlobHash through PatchResolver into
// a local patch file, writing the bytes into a temp file inside dir. Returns
// the temp file path; caller must os.Remove when done.
func resolvePatchToTempFile(ctx context.Context, resolver PatchResolver, dir, hash string) (string, error) {
	localPath, err := resolver.Resolve(hash)
	if err != nil {
		return "", fmt.Errorf("resolve patch hash %s: %w", hash, err)
	}
	diffBytes, err := os.ReadFile(localPath)
	if err != nil {
		return "", fmt.Errorf("read resolved patch at %s: %w", localPath, err)
	}
	patchPath, err := writePatchFile(dir, diffBytes)
	if err != nil {
		return "", err
	}
	return patchPath, nil
}

// PatchResolver mirrors PatchStore.Resolve for the receiver-side path. It's
// declared separately because the receiver may resolve via a different code
// path than the sender's Add (e.g. ResourceManager.Ensure rather than direct
// CAS file read). The manager accepts either via SetPatchResolver.
//
// Spec §4.1: ResourceManager.Ensure resolves a hash to a local path. But
// ResourceManager is built in a parallel phase, so we declare the interface
// here and wire it via a setter when ResourceManager is ready.
type PatchResolver interface {
	Resolve(hash string) (localPath string, err error)
}

// safeJoinWorktree joins worktreeRoot with jobID, then verifies the result
// is still under worktreeRoot. Defends against ../ traversal in jobID.
// Returns an absolute path.
func safeJoinWorktree(worktreeRoot, jobID string) (string, error) {
	abs, err := filepath.Abs(filepath.Join(worktreeRoot, jobID))
	if err != nil {
		return "", fmt.Errorf("safeJoinWorktree: abs: %w", err)
	}
	rootAbs, err := filepath.Abs(worktreeRoot)
	if err != nil {
		return "", fmt.Errorf("safeJoinWorktree: root abs: %w", err)
	}
	if !isSubPath(rootAbs, abs) {
		return "", fmt.Errorf("safeJoinWorktree: %q escapes root %q", jobID, worktreeRoot)
	}
	return abs, nil
}

// isSubPath returns true if target is root or under root. Both must be
// absolute. Uses lexical prefix match (sufficient here because jobIDs are
// generated, not user-controlled, and safeJoinWorktree already ran filepath.Clean
// via Abs).
func isSubPath(root, target string) bool {
	if root == target {
		return true
	}
	cleanRoot := filepath.Clean(root)
	cleanTarget := filepath.Clean(target)
	if cleanRoot == cleanTarget {
		return true
	}
	prefix := cleanRoot
	if !endsWithSeparator(prefix) {
		prefix += string(filepath.Separator)
	}
	return len(cleanTarget) > len(prefix) && cleanTarget[:len(prefix)] == prefix
}

func endsWithSeparator(s string) bool {
	return len(s) > 0 && s[len(s)-1] == filepath.Separator
}
