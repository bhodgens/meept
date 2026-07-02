package workspace

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitRunner is the low-level exec helper. All git invocations route through
// here so env setup (GIT_TERMINAL_PROMPT=0, GIT_TERMINAL_AUTOEXIT=0) stays
// consistent. Mirrors the pattern in internal/cluster/git_sync.go.
//
// All methods are pure functions over the given ctx and dir — they hold no
// locks and store no state, so they are trivially safe for concurrent use.
//
// The zero value is ready to use. Use the package-level defaultGitRunner
// variable to avoid Go's parser limitation that forbids composite literals
// (Type{}) inside `if` init statements (the `{` is ambiguous with the if
// block).
type gitRunner struct{}

// defaultGitRunner is the package-level instance used by all helper
// functions. Tests may shadow this to inject fakes.
var defaultGitRunner = gitRunner{}

// run executes git with args in dir and returns combined stdout+stderr.
// The caller is responsible for interpreting exit codes (git is inconsistent
// about which exit code means "nothing to commit" vs "real failure").
func (gitRunner) run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// Disable interactive prompts — if git needs a credential it must fail
	// fast rather than hang the dispatch lifecycle.
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_TERMINAL_AUTOEXIT=0",
		"GIT_LFS_SKIP_FETCH_FILTERS=1", // don't block on LFS in headless envs
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("git %s in %s: %w: %s",
			strings.Join(args, " "), dir, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// runSilent is like run but does NOT include output in the wrapped error —
// used for commands whose stdout is the payload (rev-parse, diff) where
// callers want to read stdout directly and treat non-zero as a hard error.
func (g gitRunner) runSilent(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_TERMINAL_AUTOEXIT=0",
		"GIT_LFS_SKIP_FETCH_FILTERS=1",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("git %s in %s: %w: %s",
			strings.Join(args, " "), dir, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// --- High-level git operations used by manager.go ---

// gitHeadSHA returns `git rev-parse HEAD` for the repo at dir.
func gitHeadSHA(ctx context.Context, dir string) (string, error) {
	out, err := defaultGitRunner.runSilent(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitIsDirty returns true if `git status --porcelain` produces any output.
// This includes staged AND unstaged changes (matches `git diff HEAD` scope).
func gitIsDirty(ctx context.Context, dir string) (bool, error) {
	out, err := defaultGitRunner.runSilent(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return len(bytes.TrimSpace(out)) > 0, nil
}

// gitDiffHEAD returns the unified diff of all uncommitted changes vs HEAD.
// Includes staged and unstaged changes. Excludes untracked files (git diff
// doesn't see them; this is consistent with spec §4.2 which captures
// "uncommitted-edit diff patch").
func gitDiffHEAD(ctx context.Context, dir string) ([]byte, error) {
	out, err := defaultGitRunner.runSilent(ctx, dir, "diff", "HEAD")
	if err != nil {
		return nil, err
	}
	return out, nil
}

// gitClone clones repoURL into destPath. Used by Ensure when no shared
// origin is available. Depth-1 clones are NOT used — the dispatcher may
// need to resolve an arbitrary CommitSHA that isn't HEAD.
//
// The cmd.Dir is set to the PARENT of destPath so that git clone's "create
// the destination directory" behaviour works (if we cd into destPath first,
// git clone fails because the directory doesn't exist yet).
func gitClone(ctx context.Context, repoURL, destPath string) error {
	g := defaultGitRunner
	parent := filepath.Dir(destPath)
	// Inline clone rather than using g.run, because g.run sets cmd.Dir to
	// destPath (which doesn't exist yet). We need cmd.Dir = parent.
	cmd := exec.CommandContext(ctx, "git", "clone", repoURL, destPath)
	cmd.Dir = parent
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_TERMINAL_AUTOEXIT=0",
		"GIT_LFS_SKIP_FETCH_FILTERS=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone %s into %s: %w: %s",
			repoURL, destPath, err, strings.TrimSpace(string(out)))
	}
	_ = g // keep g referenced for future use; suppress unused-var lint
	return nil
}

// gitFetchAndCheckout fetches the given commit from origin and checks out
// an ephemeral branch at that commit. Used by Ensure on an already-cloned
// repo to pin to a specific SHA.
func gitFetchAndCheckout(ctx context.Context, dir, branchName, commitSHA string) error {
	g := defaultGitRunner
	// Best-effort fetch; the commit may already be local (shallow clone of
	// the same repo). fetch failures are tolerated — if the SHA isn't local
	// AND fetch fails, checkout will fail with a clear error below.
	_, _ = g.run(ctx, dir, "fetch", "origin")

	// Create the ephemeral branch at the requested commit.
	_, err := g.run(ctx, dir, "checkout", "-b", branchName, commitSHA)
	if err != nil {
		// checkout -b may fail if branch already exists (idempotent Ensure).
		// Fall back to: checkout existing branch, then reset to commit.
		if _, err2 := g.run(ctx, dir, "checkout", branchName); err2 != nil {
			return fmt.Errorf("checkout branch %s at %s: %w (fallback also failed: %v)",
				branchName, commitSHA, err, err2)
		}
		if _, err2 := g.run(ctx, dir, "reset", "--hard", commitSHA); err2 != nil {
			return fmt.Errorf("reset %s to %s: %w", branchName, commitSHA, err2)
		}
	}
	return nil
}

// gitApplyCheck runs `git apply --check` to dry-run a patch. Returns nil
// if the patch would apply cleanly, error otherwise. Does NOT modify the
// working tree.
func gitApplyCheck(ctx context.Context, dir, patchPath string) error {
	g := defaultGitRunner
	_, err := g.run(ctx, dir, "apply", "--check", patchPath)
	return err
}

// gitApply runs `git apply` to apply a patch for real. Caller should run
// gitApplyCheck first to detect conflicts before mutating.
func gitApply(ctx context.Context, dir, patchPath string) error {
	g := defaultGitRunner
	_, err := g.run(ctx, dir, "apply", patchPath)
	return err
}

// gitWorktreeRemove runs `git worktree remove --force <path>`. Best-effort:
// the manager falls back to rm -rf if this fails (e.g. when the worktree
// admin files are already gone).
func gitWorktreeRemove(ctx context.Context, dir, worktreePath string) error {
	g := defaultGitRunner
	_, err := g.run(ctx, dir, "worktree", "remove", "--force", worktreePath)
	return err
}
