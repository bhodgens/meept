package backup

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const gitPushRetryMax = 3

// GitInit initializes a new git repository at path or opens an existing one.
func GitInit(repoPath string) (*git.Repository, string, error) {
	repo, err := git.PlainOpen(repoPath)
	if err == nil {
		return repo, repoPath, nil
	}

	if err != git.ErrRepositoryNotExists {
		return nil, "", Wrap("git_open", err)
	}

	if err := os.MkdirAll(repoPath, 0o700); err != nil {
		return nil, "", Wrap("git_init_mkdir", err)
	}

	repo, err = git.PlainInit(repoPath, false)
	if err != nil {
		return nil, "", Wrap("git_init", err)
	}

	slog.Info("backup: git repository initialized", "path", repoPath)
	return repo, repoPath, nil
}

// GitClone clones a repository, returning open repo and path.
func GitClone(url, path string) (*git.Repository, error) {
	repo, openErr := git.PlainOpen(path)
	if openErr == nil {
		return repo, nil
	}

	if !os.IsNotExist(openErr) {
		return nil, Wrap("git_clone_open", openErr)
	}

	repo, err := git.PlainClone(path, false, &git.CloneOptions{
		URL: url,
	})
	if err != nil {
		return nil, Wrap("git_clone", err)
	}

	return repo, nil
}

// GitPullRebase pulls from the default remote, rebasing local commits.
func GitPullRebase(repo *git.Repository) error {
	w, err := repo.Worktree()
	if err != nil {
		return Wrap("git_pull_worktree", err)
	}

	// Fetch
	if err := repo.Fetch(&git.FetchOptions{}); err != nil && err != git.NoErrAlreadyUpToDate {
		return Wrap("git_pull_fetch", err)
	}

	// Get current HEAD
	head, err := repo.Head()
	if err != nil {
		return Wrap("git_pull_head", err)
	}

	// Reset hard to origin/master or origin/main
	refName := "refs/remotes/origin/" + head.Name().Short()
	iter, err := repo.References()
	if err != nil {
		return Wrap("git_pull_refs", err)
	}

	found := false
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Name().String() == refName {
			if resetErr := w.Reset(&git.ResetOptions{
				Mode: git.HardReset,
				Commit: ref.Hash(),
			}); resetErr == nil {
				found = true
			}
			return nil
		}
		return nil
	})
	iter.Close()
	if err != nil {
		return Wrap("git_pull_iterate", err)
	}

	if !found {
		// No origin ref found, try the most recent one
		headObj, err := repo.Head()
		if err == nil {
			if err := w.Reset(&git.ResetOptions{
				Mode: git.HardReset,
				Commit: headObj.Hash(),
			}); err != nil {
				return Wrap("git_pull_reset_head", err)
			}
		}
		return nil
	}

	return nil
}

// GitAddCommitPush adds files to the repository, creates a commit, and attempts push.
// On conflict, it retries with rebase up to gitPushRetryMax times.
func GitAddCommitPush(repo *git.Repository, files []string, message string) error {
	w, err := repo.Worktree()
	if err != nil {
		return Wrap("git_commit_worktree", err)
	}

	for _, f := range files {
		_, err := w.Add(f)
		if err != nil {
			slog.Debug("backup: failed to add file to git (may already be staged)",
				"file", f, "error", err)
		}
	}

	status, _ := w.Status()
	if status.IsClean() {
		slog.Debug("backup: git working tree is clean, nothing to commit")
		return nil
	}

	_, err = w.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "meept-backup",
			Email: "backup@meept.local",
			When:  time.Now(),
		},
	})
	if err != nil {
		return Wrap("git_commit", err)
	}

	return gitPushWithRetry(repo)
}

func gitPushWithRetry(repo *git.Repository) error {
	for attempt := 0; attempt < gitPushRetryMax; attempt++ {
		err := repo.Push(&git.PushOptions{
			RemoteName: "origin",
		})

		if err == nil {
			return nil
		}
		if err == git.NoErrAlreadyUpToDate {
			return nil
		}

		if isGitConflict(err) {
			slog.Info("backup: git push conflict, attempting rebase", "attempt", attempt+1)

			if pullErr := GitPullRebase(repo); pullErr != nil {
				slog.Warn("backup: rebase failed", "attempt", attempt+1, "error", pullErr)
				if attempt < gitPushRetryMax-1 {
					time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
					continue
				}
				return &BackupError{
					Op:        "git_push",
					Err:       fmt.Errorf("push failed after %d attempts with conflict: %w", gitPushRetryMax, pullErr),
					Retryable: true,
				}
			}

			// Commit again after rebase
			w, wErr := repo.Worktree()
			if wErr == nil {
				_, _ = w.Commit("backup: rebase commit", &git.CommitOptions{
					Author: &object.Signature{
						Name:  "meept-backup",
						Email: "backup@meept.local",
						When:  time.Now(),
					},
				})
			}

			continue
		}

		return Wrap("git_push", err)
	}

	return &BackupError{
		Op:        "git_push",
		Err:       fmt.Errorf("push failed after %d attempts", gitPushRetryMax),
		Retryable: true,
	}
}

func isGitConflict(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "non-fast-forward") ||
		strings.Contains(s, "refused") ||
		strings.Contains(s, "failed to push") ||
		strings.Contains(s, "denied")
}

// GitListBackups returns sorted list of backup directory names (dates) for a node,
// in descending order. Reads current working tree.
func GitListBackups(repo *git.Repository, nodeID string) ([]string, error) {
	w, err := repo.Worktree()
	if err != nil {
		return nil, Wrap("git_list_backups_worktree", err)
	}

	entries, err := w.Filesystem.ReadDir("backups")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, Wrap("git_list_backups_read", err)
	}

	var backups []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dateDir := e.Name()
		nodeDir := filepath.Join("backups", dateDir, nodeID)

		nodeEntries, err := w.Filesystem.ReadDir(nodeDir)
		if err != nil {
			continue
		}

		if len(nodeEntries) > 0 {
			backups = append(backups, dateDir)
		}
	}

	sort.Sort(sort.Reverse(sort.StringSlice(backups)))

	return backups, nil
}

// EnsureRemote adds a remote to the repo if it doesn't already exist.
func EnsureRemote(repo *git.Repository, name, url string) error {
	_, err := repo.Remote(name)
	if err == nil {
		return nil // already exists
	}

	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: name,
		URLs: []string{url},
	})
	if err != nil {
		return Wrap("git_create_remote", err)
	}

	return nil
}
