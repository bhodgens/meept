package project

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// CreateWorktree creates a new git worktree for a project, scoped to a session
// or plan. It creates a new branch, adds the worktree, and records it in the
// store.
func (pm *ProjectManager) CreateWorktree(ctx context.Context, projectID, sessionID, planID string) (*Worktree, error) {
	p, err := pm.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	if p.Mode != ModeGit {
		return nil, fmt.Errorf("worktrees require git project, got mode %q", p.Mode)
	}

	wtID := uuid.New().String()

	// Determine branch name
	var branch string
	switch {
	case sessionID != "":
		branch = "session/" + sessionID
	case planID != "":
		branch = "plan/" + planID
	default:
		branch = "worktree/" + wtID
	}

	// Worktree directory: <project>/.git-worktrees/<id>
	worktreesDir := filepath.Join(p.LocalPath, ".git-worktrees")
	if err := os.MkdirAll(worktreesDir, 0o755); err != nil {
		return nil, fmt.Errorf("create worktrees dir: %w", err)
	}
	wtPath := filepath.Join(worktreesDir, wtID)

	// Create the worktree with a new branch
	if err := pm.runGit(ctx, p.LocalPath, "worktree", "add", wtPath, "-b", branch); err != nil {
		return nil, fmt.Errorf("git worktree add: %w", err)
	}

	w := &Worktree{
		ID:        wtID,
		ProjectID: projectID,
		SessionID: sessionID,
		PlanID:    planID,
		Path:      wtPath,
		Branch:    branch,
		Status:    "active",
	}

	if err := pm.store.CreateWorktree(ctx, w); err != nil {
		// Clean up the worktree on store failure
		_ = pm.runGit(ctx, p.LocalPath, "worktree", "remove", wtPath)
		return nil, fmt.Errorf("store worktree: %w", err)
	}

	pm.logger.Info("created worktree",
		"id", wtID,
		"project", projectID,
		"branch", branch,
		"path", wtPath,
	)
	return w, nil
}

// ReleaseWorktree removes a git worktree and marks it as cleaned in the store.
func (pm *ProjectManager) ReleaseWorktree(ctx context.Context, worktreeID string) error {
	w, err := pm.store.GetWorktree(ctx, worktreeID)
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}

	p, err := pm.store.GetProject(ctx, w.ProjectID)
	if err != nil {
		return fmt.Errorf("get project: %w", err)
	}

	// Remove the worktree
	if err := pm.runGit(ctx, p.LocalPath, "worktree", "remove", w.Path); err != nil {
		// Try force remove
		if forceErr := pm.runGit(ctx, p.LocalPath, "worktree", "remove", "--force", w.Path); forceErr != nil {
			return fmt.Errorf("git worktree remove: %w (force: %v)", err, forceErr)
		}
	}

	w.Status = "cleaned"
	if err := pm.store.UpdateWorktree(ctx, w); err != nil {
		return fmt.Errorf("update worktree status: %w", err)
	}

	pm.logger.Info("released worktree",
		"id", worktreeID,
		"project", w.ProjectID,
		"branch", w.Branch,
	)
	return nil
}

// MergeWorktree merges the worktree's branch into the target branch in the
// project's main repo.
func (pm *ProjectManager) MergeWorktree(ctx context.Context, worktreeID, targetBranch string) error {
	w, err := pm.store.GetWorktree(ctx, worktreeID)
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}

	p, err := pm.store.GetProject(ctx, w.ProjectID)
	if err != nil {
		return fmt.Errorf("get project: %w", err)
	}

	// Reject target branch names beginning with '-' to prevent option injection
	// (matches the guard in CheckoutBranch). w.Branch is internally generated
	// but guarded too for defense in depth.
	if strings.HasPrefix(targetBranch, "-") {
		return fmt.Errorf("target branch %q starts with '-' (refusing ambiguous git arg)", targetBranch)
	}
	if strings.HasPrefix(w.Branch, "-") {
		return fmt.Errorf("worktree branch %q starts with '-' (refusing ambiguous git arg)", w.Branch)
	}

	// Checkout target branch in main repo. The `--` separator goes AFTER
	// the branch name (not before): `git checkout -- <x>` treats <x> as a
	// pathspec; `git checkout <branch> --` separates the branch from any
	// optional pathspecs.
	if err := pm.runGit(ctx, p.LocalPath, "checkout", targetBranch, "--"); err != nil {
		return fmt.Errorf("checkout %s: %w", targetBranch, err)
	}

	// Merge the worktree branch
	if err := pm.runGit(ctx, p.LocalPath, "merge", w.Branch, "--"); err != nil {
		return fmt.Errorf("merge %s into %s: %w", w.Branch, targetBranch, err)
	}

	pm.logger.Info("merged worktree branch",
		"worktree", worktreeID,
		"branch", w.Branch,
		"target", targetBranch,
	)
	return nil
}

// GetActiveWorktree returns the active worktree for a session, if any.
func (pm *ProjectManager) GetActiveWorktree(ctx context.Context, sessionID string) (*Worktree, error) {
	return pm.store.GetActiveWorktreeBySession(ctx, sessionID)
}

// ShouldIsolatePlan decides whether a plan should be executed in an isolated
// worktree based on the configuration and plan characteristics.
func (pm *ProjectManager) ShouldIsolatePlan(fileCount int, planType string) bool {
	switch pm.cfg.WorktreePerPlan {
	case "always":
		return true
	case "never":
		return false
	case "auto":
		threshold := pm.cfg.WorktreeIsolationThreshold
		if threshold <= 0 {
			threshold = 5
		}
		return fileCount >= threshold
	default:
		return false
	}
}

// CountActiveWorktrees returns the number of active worktrees for a project.
func (pm *ProjectManager) CountActiveWorktrees(ctx context.Context, projectID string) (int, error) {
	worktrees, err := pm.store.ListWorktreesByProject(ctx, projectID)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, w := range worktrees {
		if w.Status == "active" {
			count++
		}
	}
	return count, nil
}
