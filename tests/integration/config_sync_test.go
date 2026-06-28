package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// requireGit skips the test if git is not available on PATH.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available: " + err.Error())
	}
}

// runGit executes a git command in the given directory and returns its
// combined output. Failure is fatal to the test.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@meept.local",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@meept.local",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// initBareRepo creates an empty bare git repository at path and returns
// the path.
func initBareRepo(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir bare repo: %v", err)
	}
	runGit(t, path, "init", "--bare", path)
	return path
}

// initWorkRepo creates a working git repo at workDir, configured for main
// branch and connected to the bare repo as origin. Returns nothing — callers
// use commitFiles/commitToFile as needed.
func initWorkRepo(t *testing.T, workDir, bareRepo string) {
	t.Helper()
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir work dir: %v", err)
	}
	runGit(t, workDir, "init", workDir)
	runGit(t, workDir, "symbolic-ref", "HEAD", "refs/heads/main")
	runGit(t, workDir, "config", "user.name", "Test")
	runGit(t, workDir, "config", "user.email", "test@meept.local")
	runGit(t, workDir, "remote", "add", "origin", bareRepo)
}

// writeFiles writes the given relative-path→content map under workDir.
func writeFiles(t *testing.T, workDir string, files map[string]string) {
	t.Helper()
	for relPath, content := range files {
		fullPath := filepath.Join(workDir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", relPath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", relPath, err)
		}
	}
}

// commitAndPush stages all changes in workDir, commits with msg, and pushes
// to origin main. Returns the commit hash.
func commitAndPush(t *testing.T, workDir, msg string) string {
	t.Helper()
	runGit(t, workDir, "add", "-A")
	runGit(t, workDir, "commit", "-m", msg)
	runGit(t, workDir, "push", "origin", "main")
	return strings.TrimSpace(runGit(t, workDir, "rev-parse", "HEAD"))
}

// newConfigSyncer constructs a ConfigSyncer with common test defaults.
// The ConfigSyncer constructor performs an initial shallow clone, so the
// bare repo must already contain at least one commit on the main branch.
func newConfigSyncer(t *testing.T, bareRepo, baseDir, nodeID string) *config.ConfigSyncer {
	t.Helper()
	cfg := config.ConfigSyncConfig{
		Enabled:      true,
		RepoURL:      bareRepo,
		PullSchedule: time.Hour, // long interval; we manually start+stop
		ConflictMode: "local-wins",
	}
	syncer, err := config.NewConfigSyncer(cfg, nodeID, baseDir, newTestLogger())
	if err != nil {
		t.Fatalf("NewConfigSyncer: %v", err)
	}
	return syncer
}

// runOnePullCycle starts the syncer, waits for the initial pull, and stops it.
func runOnePullCycle(syncer *config.ConfigSyncer) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	syncer.Start(ctx)
	time.Sleep(2 * time.Second)
	syncer.Stop()
}

// TestConfigSyncer_PullAndMerge_ApplySharedConfig sets up a bare git repo with
// a shared config file, creates a ConfigSyncer (which does the initial shallow
// clone), and verifies the first pull cycle applies the config to the target
// directory. This exercises the "first pull forces merge" fix: a fresh clone
// whose HEAD already matches origin/main must still trigger the merge step.
func TestConfigSyncer_PullAndMerge_ApplySharedConfig(t *testing.T) {
	t.Parallel()
	requireGit(t)

	tmp := t.TempDir()
	bareRepo := filepath.Join(tmp, "shared.git")
	workDir := filepath.Join(tmp, "work")
	baseDir := filepath.Join(tmp, "target")

	initBareRepo(t, bareRepo)
	initWorkRepo(t, workDir, bareRepo)

	// Push a single commit containing the shared config. The ConfigSyncer
	// will shallow-clone this commit, and the first pull cycle must detect
	// that configs need to be applied even though HEAD didn't change.
	sharedContent := `{
  // shared setting
  "name": "shared-config",
  "version": 1
}`
	writeFiles(t, workDir, map[string]string{
		"config/shared/app.json5": sharedContent,
	})
	commitAndPush(t, workDir, "add shared config")

	// Create the syncer — this triggers a shallow clone of the only commit.
	syncer := newConfigSyncer(t, bareRepo, baseDir, "node-test")

	// The first pull cycle should apply the config even though HEAD is
	// already at origin/main (changed=false from Pull).
	runOnePullCycle(syncer)

	appliedPath := filepath.Join(baseDir, "app.json5")
	data, err := os.ReadFile(appliedPath)
	if err != nil {
		t.Fatalf("expected applied file at %s: %v", appliedPath, err)
	}
	if !strings.Contains(string(data), "shared-config") {
		t.Errorf("applied file content does not contain expected name; got:\n%s", data)
	}
}

// TestConfigSyncer_NodeOverride_DeepMerge sets up a repo with both shared and
// per-node override configs in a second commit, then verifies the merged result
// lands on disk with deep-merged values from both layers.
func TestConfigSyncer_NodeOverride_DeepMerge(t *testing.T) {
	t.Parallel()
	requireGit(t)

	tmp := t.TempDir()
	bareRepo := filepath.Join(tmp, "shared.git")
	workDir := filepath.Join(tmp, "work")
	baseDir := filepath.Join(tmp, "target")
	nodeID := "node-test"

	initBareRepo(t, bareRepo)
	initWorkRepo(t, workDir, bareRepo)

	// Initial commit.
	writeFiles(t, workDir, map[string]string{"README.md": "init\n"})
	commitAndPush(t, workDir, "initial")

	syncer := newConfigSyncer(t, bareRepo, baseDir, nodeID)

	// Second commit: shared + node override.
	sharedContent := `{
  "name": "shared",
  "settings": {
    "a": 1,
    "b": 2,
    "nested": {
      "x": "from-shared"
    }
  }
}`
	overrideContent := `{
  "settings": {
    "b": 99,
    "nested": {
      "x": "from-node"
    }
  }
}`
	writeFiles(t, workDir, map[string]string{
		"config/shared/app.json5":               sharedContent,
		"config/nodes/" + nodeID + "/app.json5": overrideContent,
	})
	commitAndPush(t, workDir, "shared + node override")

	runOnePullCycle(syncer)

	appliedPath := filepath.Join(baseDir, "app.json5")
	data, err := os.ReadFile(appliedPath)
	if err != nil {
		t.Fatalf("expected merged file at %s: %v", appliedPath, err)
	}

	content := string(data)
	// Shared value should survive deep-merge.
	if !strings.Contains(content, `"a": 1`) {
		t.Errorf("merged file missing shared value a=1; got:\n%s", content)
	}
	// Node override should win.
	if !strings.Contains(content, `"b": 99`) {
		t.Errorf("merged file missing overridden value b=99; got:\n%s", content)
	}
	// Nested deep-merge.
	if !strings.Contains(content, `"x": "from-node"`) {
		t.Errorf("merged file missing nested override x=from-node; got:\n%s", content)
	}
	// Name from shared should still be present.
	if !strings.Contains(content, `"name": "shared"`) {
		t.Errorf("merged file missing name=shared; got:\n%s", content)
	}
}

// TestConfigSyncer_ReloadHookFires registers a reload hook for a specific
// config file, pushes a second commit containing that file, and verifies the
// hook is invoked with the new commit hash.
func TestConfigSyncer_ReloadHookFires(t *testing.T) {
	t.Parallel()
	requireGit(t)

	tmp := t.TempDir()
	bareRepo := filepath.Join(tmp, "shared.git")
	workDir := filepath.Join(tmp, "work")
	baseDir := filepath.Join(tmp, "target")

	initBareRepo(t, bareRepo)
	initWorkRepo(t, workDir, bareRepo)

	// Initial commit.
	writeFiles(t, workDir, map[string]string{"README.md": "init\n"})
	commitAndPush(t, workDir, "initial")

	syncer := newConfigSyncer(t, bareRepo, baseDir, "node-test")

	// Second commit with the config.
	sharedContent := `{
  "hook_test": true
}`
	writeFiles(t, workDir, map[string]string{
		"config/shared/app.json5": sharedContent,
	})
	hash := commitAndPush(t, workDir, "hook trigger")

	var hookCalls int32
	var capturedHash string
	syncer.RegisterReloadHook("app.json5", func(commitHash string) error {
		atomic.AddInt32(&hookCalls, 1)
		capturedHash = commitHash
		return nil
	})

	runOnePullCycle(syncer)

	if got := atomic.LoadInt32(&hookCalls); got != 1 {
		t.Errorf("hook calls = %d, want 1", got)
	}
	if capturedHash != hash {
		t.Errorf("hook commit = %q, want %q", capturedHash, hash)
	}
}

// TestConfigSyncer_InvalidConfigSkipped verifies that a JSON5 syntax error in
// one shared config file does not prevent other valid files from being applied.
func TestConfigSyncer_InvalidConfigSkipped(t *testing.T) {
	t.Parallel()
	requireGit(t)

	tmp := t.TempDir()
	bareRepo := filepath.Join(tmp, "shared.git")
	workDir := filepath.Join(tmp, "work")
	baseDir := filepath.Join(tmp, "target")

	initBareRepo(t, bareRepo)
	initWorkRepo(t, workDir, bareRepo)

	// Initial commit.
	writeFiles(t, workDir, map[string]string{"README.md": "init\n"})
	commitAndPush(t, workDir, "initial")

	syncer := newConfigSyncer(t, bareRepo, baseDir, "node-test")

	// Second commit with valid + invalid configs.
	validContent := `{
  "name": "valid"
}`
	// Intentionally broken JSON5 (hujson.Standardize will reject this).
	invalidContent := `{ not valid json5 ,,, }`

	writeFiles(t, workDir, map[string]string{
		"config/shared/good.json5": validContent,
		"config/shared/bad.json5":  invalidContent,
	})
	commitAndPush(t, workDir, "valid + invalid")

	runOnePullCycle(syncer)

	// good.json5 should exist.
	goodPath := filepath.Join(baseDir, "good.json5")
	if _, err := os.Stat(goodPath); err != nil {
		t.Errorf("expected valid config applied at %s: %v", goodPath, err)
	}

	// bad.json5 should NOT exist (was skipped during apply).
	badPath := filepath.Join(baseDir, "bad.json5")
	if _, err := os.Stat(badPath); err == nil {
		t.Errorf("invalid config should not have been applied at %s", badPath)
	}
}
