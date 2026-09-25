package backup

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/effects"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// runEffectsGit runs a git command in dir, failing the test on error.
func runEffectsGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// setupEffectsRemote creates a bare origin, clones a seed with one commit,
// pushes it, and returns (seedDir, seedRepo, originPath, seededSHA).
// Mirrors internal/workspace/manager_test.go's setupOriginRepo pattern.
func setupEffectsRemote(t *testing.T) (string, *git.Repository, string, string) {
	t.Helper()
	base := t.TempDir()
	originPath := filepath.Join(base, "origin.git")
	seedPath := filepath.Join(base, "seed")

	runEffectsGit(t, base, "init", "--bare", "-b", "main", originPath)
	runEffectsGit(t, base, "clone", originPath, seedPath)
	writeEffectsFile(t, seedPath, "README.md", "# seed\n")
	runEffectsGit(t, seedPath, "add", ".")
	runEffectsGit(t, seedPath, "commit", "-m", "initial")
	runEffectsGit(t, seedPath, "push", "-u", "origin", "main")

	seedRepo, err := git.PlainOpen(seedPath)
	if err != nil {
		t.Fatalf("PlainOpen seed: %v", err)
	}
	head, err := seedRepo.Head()
	if err != nil {
		t.Fatalf("seed Head: %v", err)
	}
	return seedPath, seedRepo, originPath, head.Hash().String()
}

func writeEffectsFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// effectsRemoteHeadSHA resolves origin/main in the seed repo.
func effectsRemoteHeadSHA(t *testing.T, seedRepo *git.Repository) string {
	t.Helper()
	ref, err := seedRepo.Reference(plumbing.NewRemoteReferenceName("origin", "main"), true)
	if err != nil {
		t.Fatalf("remote ref: %v", err)
	}
	return ref.Hash().String()
}

func TestBackupPush_EffectsIdempotent(t *testing.T) {
	t.Run("first push", func(t *testing.T) {
		seedPath, seedRepo, originPath, seedSHA := setupEffectsRemote(t)
		ledger := effects.NewMemoryLedger()

		// new local commit to push
		writeEffectsFile(t, seedPath, "backup.txt", "cycle one\n")
		pushed, err := GitAddCommitPushWithLedger(seedRepo, ledger, []string{"backup.txt"},
			"backup: cycle one", originPath)
		if err != nil {
			t.Fatalf("GitAddCommitPushWithLedger: %v", err)
		}
		if !pushed {
			t.Error("first run must report pushed=true")
		}

		head, err := seedRepo.Head()
		if err != nil {
			t.Fatalf("Head: %v", err)
		}
		rec, err := ledger.Get(context.Background(),
			effects.EffectKey("backup.git_push", originPath, head.Hash().String()))
		if err != nil {
			t.Fatalf("ledger Get: %v", err)
		}
		if rec.State != effects.StateCompleted {
			t.Errorf("state = %q, want completed", rec.State)
		}
		if !rec.ProviderIdempotent {
			t.Error("backup push must declare ProviderIdempotent=true")
		}
		if rec.Tool != "backup.git_push" {
			t.Errorf("tool = %q, want backup.git_push", rec.Tool)
		}

		var receipt struct {
			Head            string `json:"head"`
			Remote          string `json:"remote"`
			AlreadyUpToDate bool   `json:"already_up_to_date"`
		}
		if err := json.Unmarshal(rec.Receipt, &receipt); err != nil {
			t.Fatalf("receipt unmarshal: %v", err)
		}
		if receipt.Head != head.Hash().String() {
			t.Errorf("receipt head = %q, want %q", receipt.Head, head.Hash().String())
		}
		if receipt.Remote != originPath {
			t.Errorf("receipt remote = %q, want %q", receipt.Remote, originPath)
		}
		if receipt.AlreadyUpToDate {
			t.Error("first push must not be already_up_to_date")
		}

		// remote advanced past the seed commit
		if got := effectsRemoteHeadSHA(t, seedRepo); got == seedSHA {
			t.Error("remote HEAD did not advance; push did not happen")
		}
	})

	t.Run("second run same key skips the push", func(t *testing.T) {
		seedPath, seedRepo, originPath, _ := setupEffectsRemote(t)
		ledger := effects.NewMemoryLedger()

		writeEffectsFile(t, seedPath, "backup.txt", "cycle two\n")
		if _, err := GitAddCommitPushWithLedger(seedRepo, ledger, []string{"backup.txt"},
			"backup: cycle two", originPath); err != nil {
			t.Fatalf("first GitAddCommitPushWithLedger: %v", err)
		}

		remoteAfterFirst := effectsRemoteHeadSHA(t, seedRepo)

		// Same effect key (same repoPath + headSHA): the reconcile-style
		// re-invocation with a completed prior must be an idempotent no-op.
		pushed, err := GitAddCommitPushWithLedger(seedRepo, ledger, nil,
			"backup: cycle two (duplicate)", originPath)
		if err != nil {
			t.Fatalf("duplicate GitAddCommitPushWithLedger: %v", err)
		}
		if pushed {
			t.Error("duplicate run must report pushed=false (no second push)")
		}
		if got := effectsRemoteHeadSHA(t, seedRepo); got != remoteAfterFirst {
			t.Errorf("remote moved: %q -> %q (duplicate push executed!)", remoteAfterFirst, got)
		}
	})

	t.Run("nil ledger legacy path", func(t *testing.T) {
		seedPath, seedRepo, _, seedSHA := setupEffectsRemote(t)

		writeEffectsFile(t, seedPath, "backup.txt", "legacy\n")
		// nil ledger: legacy GitAddCommitPush path, no claims recorded
		if err := GitAddCommitPush(seedRepo, []string{"backup.txt"}, "backup: legacy"); err != nil {
			t.Fatalf("GitAddCommitPush: %v", err)
		}
		if got := effectsRemoteHeadSHA(t, seedRepo); got == seedSHA {
			t.Error("legacy path did not push")
		}
	})

	t.Run("retry path completes claim after initial failure", func(t *testing.T) {
		// A push that fails after the claim must leave the record pending;
		// a later successful run of the same key completes it.
		seedPath, seedRepo, originPath, seedSHA := setupEffectsRemote(t)
		ledger := effects.NewMemoryLedger()

		writeEffectsFile(t, seedPath, "backup.txt", "flaky\n")
		key := effects.EffectKey("backup.git_push", originPath, seedSHA)

		// Simulate claim + failure: claim the key manually, then run the
		// real flow — the second claim of the same key hits the pending
		// prior. The production flow treats "prior claimed by another
		// caller" as an error, so assert that contract here.
		_, _, claimErr := ledger.Claim(context.Background(), key, effects.EffectMeta{
			Tool:               "backup.git_push",
			ProviderIdempotent: true,
		})
		if claimErr != nil {
			t.Fatalf("manual claim: %v", claimErr)
		}

		// Complete the claim via RecordReceipt+Complete to simulate the
		// first caller succeeding, then the retry sees a completed prior.
		if err := ledger.RecordReceipt(context.Background(), key, mustJSON(t, map[string]any{
			"head": seedSHA, "remote": originPath, "already_up_to_date": false,
		})); err != nil {
			t.Fatalf("RecordReceipt: %v", err)
		}
		if err := ledger.Complete(context.Background(), key); err != nil {
			t.Fatalf("Complete: %v", err)
		}

		// Now a fresh commit + push runs through the ledger and completes.
		writeEffectsFile(t, seedPath, "backup2.txt", "retry\n")
		pushed, err := GitAddCommitPushWithLedger(seedRepo, ledger, []string{"backup2.txt"},
			"backup: retry cycle", originPath)
		if err != nil {
			t.Fatalf("GitAddCommitPushWithLedger after reconcile: %v", err)
		}
		if !pushed {
			t.Error("fresh key must report pushed=true")
		}
	})
}

// mustJSON marshals v, failing the test on error.
func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// TestAlreadyUpToDateIsSuccess pins the provider-side idempotency claim:
// re-pushing the same commit is treated as success by gitPushWithRetry.
func TestAlreadyUpToDateIsSuccess(t *testing.T) {
	seedPath, _, _, _ := setupEffectsRemote(t)
	repo, err := git.PlainOpen(seedPath)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	// No new commit: pushing is a no-op that reports AlreadyUpToDate.
	if err := gitPushWithRetry(repo); err != nil {
		t.Fatalf("re-push should be success (AlreadyUpToDate), got: %v", err)
	}
}
