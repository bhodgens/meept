package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/effects"
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

	if !errors.Is(err, git.ErrRepositoryNotExists) {
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
	if err := repo.Fetch(&git.FetchOptions{}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
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
				Mode:   git.HardReset,
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
				Mode:   git.HardReset,
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

// SetEffectsLedgerFunc is the process-wide effects-ledger injection point
// for the backup package. The GitBackupScheduler holds no long-lived
// ledger handle; instead the daemon wiring calls SetEffectsLedger at
// startup and every GitAddCommitPushWithLedger call routes through it.
// Nil (default) keeps the legacy unclaimed behavior.
var effectsLedgerFunc func() effects.Ledger

// SetEffectsLedger wires the external-effect ledger used by
// GitAddCommitPushWithLedger. Nil-guarded per the setter convention: a
// nil argument is ignored so wiring code can pass a possibly-nil ledger
// from degraded startup paths.
func SetEffectsLedger(l effects.Ledger) {
	if l != nil {
		effectsLedgerFunc = func() effects.Ledger { return l }
	}
}

// GitAddCommitPushWithLedger adds files, commits, and pushes — claiming
// the push through the effects ledger when one is wired (via
// SetEffectsLedger). Returns pushed=false when a completed prior made the
// push an idempotent no-op. Falls back to plain GitAddCommitPush when no
// ledger is wired.
//
// Seam note (leaf 02 Task 2): the ledger wraps ONLY the push, not the
// add/commit prefix. Re-running the full flow after a crash would see a
// clean tree and skip the push, so the effect key — derived from the
// remote path + the freshly committed head SHA, never wall time — must be
// claimed around the irreversible external call itself.
func GitAddCommitPushWithLedger(repo *git.Repository, ledger effects.Ledger, files []string, message string, remotePath string) (bool, error) {
	if ledger == nil && effectsLedgerFunc != nil {
		ledger = effectsLedgerFunc()
	}
	if ledger == nil {
		return true, GitAddCommitPush(repo, files, message)
	}

	// add + commit first (local, reversible — no claim needed), then claim
	// around the push. The effect key needs the commit SHA, so the commit
	// must exist before claiming.
	if err := gitAddAndCommit(repo, files, message); err != nil {
		return false, err
	}

	head, err := repo.Head()
	if err != nil {
		return false, Wrap("git_effects_head", err)
	}
	headSHA := head.Hash().String()
	key := effects.EffectKey("backup.git_push", remotePath, headSHA)

	receipt, reused, err := effects.Run(context.Background(), ledger, key, effects.EffectMeta{
		Tool:               "backup.git_push",
		ProviderIdempotent: true,
		Payload:            mustEffectJSON(map[string]any{"remote": remotePath, "head": headSHA}),
	}, func(ctx context.Context) (json.RawMessage, error) {
		// AlreadyUpToDate (the remote already has this commit) IS
		// provider-side idempotency: check BEFORE pushing so the receipt
		// records the pre-push state.
		alreadyUpToDate, listErr := remoteRefIsUpToDate(repo, headSHA)
		if listErr != nil {
			return nil, listErr
		}
		if !alreadyUpToDate {
			if pushErr := gitPushWithRetry(repo); pushErr != nil {
				return nil, pushErr
			}
			// Verify the remote accepted the ref before recording the receipt.
			landed, verifyErr := remoteRefIsUpToDate(repo, headSHA)
			if verifyErr != nil {
				return nil, verifyErr
			}
			if !landed {
				return nil, fmt.Errorf("backup push: remote %s did not accept %s", remotePath, headSHA)
			}
		}
		return mustEffectJSON(map[string]any{
			"head":               headSHA,
			"remote":             remotePath,
			"already_up_to_date": alreadyUpToDate,
		}), nil
	})
	if err != nil {
		return false, fmt.Errorf("backup push effects: %w", err)
	}
	if reused {
		slog.Debug("backup push already completed", "key", key)
		return false, nil
	}
	if err := ledger.Complete(context.Background(), key); err != nil {
		return false, fmt.Errorf("backup push effects complete %s: %w", key, err)
	}
	_ = receipt
	return true, nil
}

// gitAddAndCommit is GitAddCommitPush minus the push: add the files, then
// commit when the tree is dirty.
func gitAddAndCommit(repo *git.Repository, files []string, message string) error {
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
	return nil
}

// remoteRefIsUpToDate reports whether the remote tracking ref for the
// default branch already points at headSHA — i.e. the push landed (or was
// never needed). This is the "verify" half of the pinned protocol.
func remoteRefIsUpToDate(repo *git.Repository, headSHA string) (bool, error) {
	remote, err := repo.Remote("origin")
	if err != nil {
		return false, Wrap("git_effects_remote", err)
	}
	refs, err := remote.List(&git.ListOptions{})
	if err != nil {
		return false, Wrap("git_effects_remote_list", err)
	}
	for _, ref := range refs {
		if ref.Name().IsBranch() && ref.Hash().String() == headSHA {
			return true, nil
		}
	}
	return false, nil
}

// mustEffectJSON marshals v for receipts/payloads; every value is
// caller-constructed (strings + bools), so an error is a programming bug.
// It still propagates the error rather than ignoring it.
func mustEffectJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("backup: effects json marshal failed", "error", err)
		return json.RawMessage(`{}`)
	}
	return b
}

func gitPushWithRetry(repo *git.Repository) error {
	for attempt := 0; attempt < gitPushRetryMax; attempt++ {
		err := repo.Push(&git.PushOptions{
			RemoteName: "origin",
		})

		if err == nil {
			return nil
		}
		if errors.Is(err, git.NoErrAlreadyUpToDate) {
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
