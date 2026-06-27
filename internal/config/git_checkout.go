package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

const (
	gitShallowDepth  = 1
	gitCloneRetryMax = 3
	gitPullRetryMax  = 3
)

// GitCheckout manages the config repo checkout lifecycle.
type GitCheckout struct {
	repoURL     string
	checkoutDir string
	logger      *slog.Logger
	repo        *git.Repository
}

// NewGitCheckout creates a new GitCheckout and performs an initial shallow clone if needed.
func NewGitCheckout(repoURL, checkoutDir string, logger *slog.Logger) (*GitCheckout, error) {
	g := &GitCheckout{
		repoURL:     repoURL,
		checkoutDir: checkoutDir,
		logger:      logger,
	}

	// Try to open existing repo; if missing, do shallow clone
	repo, err := git.PlainOpen(checkoutDir)
	if err != nil {
		if os.IsNotExist(err) || err == git.ErrRepositoryNotExists {
			if cloneErr := g.cloneShallow(); cloneErr != nil {
				return nil, fmt.Errorf("config sync: failed to clone %s: %w", repoURL, cloneErr)
			}
			repo = g.repo
		} else {
			return nil, fmt.Errorf("config sync: failed to open %s: %w", checkoutDir, err)
		}
	}
	g.repo = repo

	return g, nil
}

// cloneShallow performs a shallow clone of depth 1.
func (g *GitCheckout) cloneShallow() error {
	dir := checkoutDirParent(g.checkoutDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir checkout parent: %w", err)
	}

	for attempt := 1; attempt <= gitCloneRetryMax; attempt++ {
		repo, err := git.PlainClone(g.checkoutDir, false, &git.CloneOptions{
			URL:      g.repoURL,
			Depth:    gitShallowDepth,
			Progress: nil,
		})
		if err != nil {
			// Cleanup partial clone
			_ = os.RemoveAll(g.checkoutDir)
			if attempt < gitCloneRetryMax {
				g.logger.Warn("config sync: clone attempt failed, retrying",
					"attempt", attempt, "error", err)
				continue
			}
			return fmt.Errorf("clone failed after %d attempts: %w", gitCloneRetryMax, err)
		}
		g.repo = repo
		g.logger.Info("config sync: shallow clone successful", "dir", g.checkoutDir)
		return nil
	}
	return fmt.Errorf("clone failed after %d attempts", gitCloneRetryMax)
}

// Pull attempts a shallow pull. Returns (commitHash, changed, error).
// If the working tree is dirty, pulls are skipped to avoid local changes overwriting.
func (g *GitCheckout) Pull(_ context.Context) (commitHash string, changed bool, err error) {
	if g.repo == nil {
		return "", false, fmt.Errorf("not initialized")
	}

	// Check dirty
	dirty, dErr := g.IsDirty()
	if dErr != nil {
		return "", false, dErr
	}
	if dirty {
		return "", false, ErrCheckoutDirty
	}

	w, err := g.repo.Worktree()
	if err != nil {
		return "", false, fmt.Errorf("git_pull_worktree: %w", err)
	}

	headBefore, err := g.repo.Head()
	if err != nil {
		return "", false, fmt.Errorf("git_pull_head_before: %w", err)
	}

	// Fetch
	err = g.repo.Fetch(&git.FetchOptions{
		Depth: gitShallowDepth,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return "", false, fmt.Errorf("git_pull_fetch: %w", err)
	}

	// Try to reset to origin/main
	_, rErr := g.repo.Remote("origin")
	if rErr == nil {
		ref := plumbing.NewRemoteReferenceName("origin", "main")
		obj, objErr := g.repo.ResolveRevision(plumbing.Revision(ref))
		if objErr == nil {
			_ = w.Reset(&git.ResetOptions{
				Mode:   git.HardReset,
				Commit: *obj,
			})
			headAfter, h2Err := g.repo.Head()
			if h2Err == nil {
				return headAfter.Hash().String(), headAfter.Hash() != headBefore.Hash(), nil
			}
		}
	}

	// Fallback: scan references for any origin ref
	iter, refErr := g.repo.References()
	if refErr == nil {
		err = iter.ForEach(func(ref *plumbing.Reference) error {
			name := ref.Name().String()
			if strings.HasPrefix(name, "refs/remotes/origin/") {
				if resetErr := w.Reset(&git.ResetOptions{
					Mode:   git.HardReset,
					Commit: ref.Hash(),
				}); resetErr == nil {
					return nil
				}
			}
			return nil
		})
		iter.Close()
		if err == nil {
			headAfter, hErr := g.repo.Head()
			if hErr == nil {
				return headAfter.Hash().String(), headAfter.Hash() != headBefore.Hash(), nil
			}
		}
	}

	// Last resort: return current HEAD unchanged
	currentHead, curErr := g.repo.Head()
	if curErr != nil {
		return headBefore.Hash().String(), false, nil
	}
	return currentHead.Hash().String(), currentHead.Hash() != headBefore.Hash(), nil
}

// CommitAndPush creates a commit from working tree changes and attempts push.
func (g *GitCheckout) CommitAndPush(_ context.Context, message string) error {
	if g.repo == nil {
		return fmt.Errorf("not initialized")
	}

	w, err := g.repo.Worktree()
	if err != nil {
		return fmt.Errorf("git_checkout_commit_worktree: %w", err)
	}

	status, err := w.Status()
	if err != nil {
		return fmt.Errorf("git_checkout_status: %w", err)
	}

	if status.IsClean() {
		return nil
	}

	_, err = w.Add(".")
	if err != nil {
		return fmt.Errorf("git_checkout_add: %w", err)
	}

	_, err = w.Commit(message, &git.CommitOptions{})
	if err != nil {
		return fmt.Errorf("git_checkout_commit: %w", err)
	}

	// Push with retry on conflict
	for attempt := 1; attempt <= gitPullRetryMax; attempt++ {
		err = g.repo.Push(&git.PushOptions{})
		if err == nil {
			return nil
		}
		if err == git.NoErrAlreadyUpToDate {
			return nil
		}

		// Retry on conflict
		s := err.Error()
		if strings.Contains(s, "non-fast-forward") ||
			strings.Contains(s, "failed to push") ||
			strings.Contains(s, "denied") {

			_ = g.repo.Fetch(&git.FetchOptions{Depth: gitShallowDepth})
			continue
		}

		return fmt.Errorf("git_checkout_push: %w", err)
	}

	return fmt.Errorf("git_checkout_push: %w (conflict after %d retries)",
		ErrGitConflict, gitPullRetryMax)
}

// GetLatestCommit returns the latest commit hash in the repo.
func (g *GitCheckout) GetLatestCommit() (string, error) {
	if g.repo == nil {
		return "", fmt.Errorf("not initialized")
	}
	head, err := g.repo.Head()
	if err != nil {
		return "", err
	}
	return head.Hash().String(), nil
}

// IsDirty returns true if the working tree has uncommitted changes.
func (g *GitCheckout) IsDirty() (bool, error) {
	if g.repo == nil {
		return false, fmt.Errorf("not initialized")
	}
	w, err := g.repo.Worktree()
	if err != nil {
		return false, err
	}
	status, err := w.Status()
	if err != nil {
		return false, err
	}
	return !status.IsClean(), nil
}

// Path returns the checkout directory.
func (g *GitCheckout) Path() string {
	return g.checkoutDir
}

// Repo returns the underlying git.Repository, if available.
func (g *GitCheckout) Repo() *git.Repository {
	return g.repo
}

// checkoutDirParent computes the parent directory of the checkout path,
// handling the case where checkoutDir is a bare name (no slash).
func checkoutDirParent(dir string) string {
	parent := dir
	for len(parent) > 0 && parent[len(parent)-1] == '/' {
		parent = parent[:len(parent)-1]
	}
	for len(parent) > 0 && parent[0] == '/' {
		parent = parent[1:]
	}
	if slash := strings.LastIndexByte(parent, '/'); slash >= 0 {
		return parent[:slash]
	}
	return "."
}
