package project

import (
	"context"
	"fmt"
	"strings"
)

// BranchInfo contains information about a git branch.
type BranchInfo struct {
	Name      string `json:"name"`
	IsCurrent bool   `json:"is_current"`
	IsHead    bool   `json:"is_head"` // detached HEAD state
}

// ListBranches returns all branches for a git project.
func (pm *ProjectManager) ListBranches(ctx context.Context, id string) ([]*BranchInfo, error) {
	p, err := pm.store.GetProject(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Mode != ModeGit {
		return nil, fmt.Errorf("cannot list branches for non-git project %s", id)
	}

	// Get current branch
	currentBranch, _ := pm.gitOutput(ctx, p.LocalPath, "rev-parse", "--abbrev-ref", "HEAD")
	currentBranch = strings.TrimSpace(currentBranch)

	// Check for detached HEAD
	isDetached := currentBranch == "HEAD"

	// Get all branches (local and remote-tracking)
	output, err := pm.gitOutput(ctx, p.LocalPath, "branch", "-a")
	if err != nil {
		return nil, fmt.Errorf("git branch: %w", err)
	}

	var branches []*BranchInfo
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse branch line - format: "* branch-name" or "  branch-name"
		isCurrent := false
		if strings.HasPrefix(line, "* ") {
			isCurrent = true
			line = strings.TrimPrefix(line, "* ")
		} else if strings.HasPrefix(line, "*") {
			isCurrent = true
			line = strings.TrimPrefix(line, "*")
		}
		line = strings.TrimSpace(line)

		// Skip detached HEAD indicator
		if strings.HasPrefix(line, "(HEAD detached at") || strings.HasPrefix(line, "(no branch") {
			continue
		}

		branches = append(branches, &BranchInfo{
			Name:      line,
			IsCurrent: isCurrent && !isDetached,
			IsHead:    isCurrent && isDetached,
		})
	}

	return branches, nil
}

// CheckoutBranch checks out a branch in a git project.
func (pm *ProjectManager) CheckoutBranch(ctx context.Context, id, branch string) error {
	p, err := pm.store.GetProject(ctx, id)
	if err != nil {
		return err
	}
	if p.Mode != ModeGit {
		return fmt.Errorf("cannot checkout branch for non-git project %s", id)
	}

	// Checkout the branch
	if err := pm.runGit(ctx, p.LocalPath, "checkout", branch); err != nil {
		return fmt.Errorf("git checkout: %w", err)
	}

	// Update project branch
	p.Branch = branch
	p.UpdatedAt = pm.now()
	return pm.store.UpdateProject(ctx, p)
}
