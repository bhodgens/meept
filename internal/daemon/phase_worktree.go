package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// phaseWorktreeProvisioner returns a production SetPhaseWorktreeProvisioner
// callback: it materializes an isolated `git worktree` of the active
// project's local repo per phase, under the phase-worktrees root
// (<state_dir|data_dir>/phase-worktrees/<taskID>-<phaseName>), so
// concurrently active phases never share one working tree.
//
// Design notes:
//   - `git worktree add` (not clone) keeps the per-phase cost proportional
//     to the repo's checkout, not its history, and shares the object store.
//   - The worktree branches from the project's current HEAD; phase-level
//     artifact isolation is the point, not divergence from main.
//   - Provisioning failure degrades to "no worktree" (the orchestrator logs
//     a Warn and the phase runs on the session/project dir) — never fails
//     the phase start.
//   - Existing path (daemon restart mid-task) is reused when it still
//     contains a .git file, making provisioning idempotent per phase.
func phaseWorktreeProvisioner(wtRoot string, activeProjectPath func(ctx context.Context) string, logger *slog.Logger) func(ctx context.Context, taskID, phaseID, phaseName string) (string, error) {
	return func(ctx context.Context, taskID, phaseID, phaseName string) (string, error) {
		if logger == nil {
			logger = slog.Default()
		}
		repoPath := activeProjectPath(ctx)
		if repoPath == "" {
			return "", nil // no active project: nothing to isolate
		}
		if !isGitRepo(repoPath) {
			return "", nil // non-git project: isolation impossible, run shared
		}
		slug := strings.ReplaceAll(strings.ToLower(phaseName), " ", "-")
		wtPath := filepath.Join(wtRoot, fmt.Sprintf("%s-%s", taskID, slug))
		if wtPath == wtRoot || !strings.HasPrefix(wtPath, wtRoot+string(os.PathSeparator)) {
			return "", fmt.Errorf("phase worktree path %q escapes root %q", wtPath, wtRoot)
		}
		if fi, err := os.Stat(filepath.Join(wtPath, ".git")); err == nil && !fi.IsDir() {
			return wtPath, nil // already provisioned (restart mid-task)
		}
		if err := os.MkdirAll(wtRoot, 0o755); err != nil {
			return "", fmt.Errorf("phase worktree root: %w", err)
		}
		cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "worktree", "add", "--detach", wtPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git worktree add %s: %w: %s", wtPath, err, strings.TrimSpace(string(out)))
		}
		logger.Info("phase worktree provisioned", "task_id", taskID, "phase", phaseName, "path", wtPath)
		return wtPath, nil
	}
}

// isGitRepo reports whether dir is inside a git work tree.
func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}
