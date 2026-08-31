package selfimprove

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func slogDiscardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// TestRollback_RestoresNestedPath verifies that Rollback writes the backup
// contents back to the original (nested) relative path rather than to the
// project root.
func TestRollback_RestoresNestedPath(t *testing.T) {
	tmpDir := t.TempDir()
	projectRoot := filepath.Join(tmpDir, "project")
	//nolint:gosec // test directory/file
	if err := os.MkdirAll(filepath.Join(projectRoot, "sub", "dir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	relPath := filepath.Join("sub", "dir", "foo.go")
	nestedFile := filepath.Join(projectRoot, relPath)
	originalContent := []byte("package original\n")
	//nolint:gosec // test directory/file
	if err := os.WriteFile(nestedFile, originalContent, 0o644); err != nil {
		t.Fatalf("write original file: %v", err)
	}

	// Backup directory (mirror applier layout).
	backupDir := filepath.Join(tmpDir, "backups")
	//nolint:gosec // test directory/file
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup: %v", err)
	}
	backupPath := filepath.Join(backupDir, "fix1_foo.go.backup")
	//nolint:gosec // test directory/file
	if err := os.WriteFile(backupPath, originalContent, 0o644); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	// Simulate modification to the nested file.
	//nolint:gosec // test directory/file
	if err := os.WriteFile(nestedFile, []byte("package modified\n"), 0o644); err != nil {
		t.Fatalf("modify file: %v", err)
	}

	applier := &ChangeApplier{
		projectRoot: projectRoot,
		backupDir:   backupDir,
		logger:      slogDiscardLogger(),
	}

	applied := &AppliedFix{
		FixID:             "fix1",
		AppliedAt:         time.Now(),
		ApprovedBy:        "auto",
		RollbackAvailable: true,
		BackupPath:        backupPath,
		OriginalPath:      relPath,
	}

	if err := applier.Rollback(applied); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// The nested file must be restored with the original contents.
	got, err := os.ReadFile(nestedFile)
	if err != nil {
		t.Fatalf("read nested file: %v", err)
	}
	if !bytes.Equal(got, originalContent) {
		t.Errorf("nested file content = %q, want %q", got, originalContent)
	}

	// There must NOT be a stray file at the project root.
	strayPath := filepath.Join(projectRoot, "foo.go")
	if _, err := os.Stat(strayPath); !os.IsNotExist(err) {
		t.Errorf("unexpected stray file at %s (err=%v)", strayPath, err)
	}
}

func TestValidateFixPath(t *testing.T) {
	projectRoot := t.TempDir()
	a := &ChangeApplier{projectRoot: projectRoot, logger: slogDiscardLogger()}

	cases := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"empty path rejected", "", true},
		{"dash-prefixed rejected", "-rf-boom", true},
		{"absolute path rejected", "/etc/passwd", true},
		{"traversal rejected", filepath.Join("..", "escape.go"), true},
		{"nested valid accepted", filepath.Join("sub", "dir", "foo.go"), false},
		{"simple valid accepted", "foo.go", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := a.validateFixPath(tc.path)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateFixPath(%q) err=%v wantErr=%v", tc.path, err, tc.wantErr)
			}
		})
	}
}

// newTestApplier constructs a ChangeApplier rooted at a temp project with an
// adjacent temp backup directory, bypassing NewChangeApplier's home-dir side
// effects.
func newTestApplier(t *testing.T) (*ChangeApplier, string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "project")
	backupDir := filepath.Join(t.TempDir(), "backups")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir project root: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup dir: %v", err)
	}
	a := &ChangeApplier{
		config:           SafetyConfig{},
		projectRoot:      root,
		backupDir:        backupDir,
		logger:           slogDiscardLogger(),
		pendingApprovals: make(map[string]*pendingFix),
	}
	return a, root, backupDir
}

// testDiff returns a well-formed conflict-marker diff replacing original
// with fixed.
func testDiff(original, fixed string) string {
	return "<<<<<<< ORIGINAL\n" + original + "\n=======\n" + fixed + "\n>>>>>>> FIXED\n"
}

func TestDenyTrustedRoot(t *testing.T) {
	a, _, _ := newTestApplier(t)

	cases := []struct {
		name       string
		path       string
		wantErr    bool
		wantSentIN bool // errors.Is(err, ErrTrustedRoot) expected
	}{
		// Trusted-root prefix denials.
		{"security dir file", "internal/security/engine.go", true, true},
		{"security dir nested", "internal/security/sub/x.go", true, true},
		{"pkg security dir", "pkg/security/audit.go", true, true},
		{"eval dir file", "internal/eval/record.go", true, true},
		{"applier itself", "internal/selfimprove/applier.go", true, true},
		{"validator itself", "internal/selfimprove/validator.go", true, true},
		{"applier prefixed variant", "internal/selfimprove/applier.go.bak", true, true},
		// Traversal and absolute paths.
		{"dotdot traversal", "../../.ssh/id_rsa", true, true},
		{"single dotdot", "../escape.go", true, true},
		{"dotdot hidden mid-path", "sub/../../escape.go", true, true},
		{"absolute path", "/etc/passwd", true, true},
		// Gate-bearing config (set-gate CLI is the only mutator).
		{"gate config root", "config/meept.json5", true, true},
		{"employee gate config", "config/employees/ci-monitor.json5", true, true},
		{"config path normalized via clean", "config/skills/../meept.json5", true, true},
		{"eval oracle fixture", "testdata/eval/oracle_k2.json", true, true},
		{"eval oracle fixture nested", "testdata/eval/sub/run.json", true, true},
		// Skills stay writable (control cases).
		{"skill under config", "config/skills/my-skill.md", false, false},
		{"skill under .meept", ".meept/skills/x/SKILL.md", false, false},
		{"plain source file", "cmd/meept/main.go", false, false},
		{"tools package", "internal/tools/builtin/remember.go", false, false},
		{"selfimprove other file", "internal/selfimprove/controller.go", false, false},
		// Malformed paths: plain error, no sentinel (documented choice).
		{"empty path", "", true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := a.denyTrustedRoot(tc.path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("denyTrustedRoot(%q) err=%v wantErr=%v", tc.path, err, tc.wantErr)
			}
			if got := errors.Is(err, ErrTrustedRoot); got != tc.wantSentIN {
				t.Errorf("denyTrustedRoot(%q) errors.Is(err, ErrTrustedRoot)=%v want %v (err=%v)",
					tc.path, got, tc.wantSentIN, err)
			}
		})
	}
}

func TestDenyTrustedRoot_SymlinkEscape(t *testing.T) {
	a, root, _ := newTestApplier(t)
	outside := t.TempDir()

	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	// Symlinked directory and file pointing outside projectRoot.
	if err := os.Symlink(outside, filepath.Join(root, "linkdir")); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "linkfile")); err != nil {
		t.Fatalf("symlink file: %v", err)
	}

	// Symlink chain a -> b -> outside.
	if err := os.Symlink("b", filepath.Join(root, "a")); err != nil {
		t.Fatalf("symlink a: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "b")); err != nil {
		t.Fatalf("symlink b: %v", err)
	}

	// Symlink that stays inside must remain allowed.
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	insideFile := filepath.Join(root, "sub", "real.txt")
	if err := os.WriteFile(insideFile, []byte("inside"), 0o644); err != nil {
		t.Fatalf("write inside file: %v", err)
	}
	if err := os.Symlink(filepath.Join("sub", "real.txt"), filepath.Join(root, "oklink")); err != nil {
		t.Fatalf("symlink oklink: %v", err)
	}

	cases := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"symlinked dir escape", filepath.Join("linkdir", "secret.txt"), true},
		{"symlinked file escape", "linkfile", true},
		{"symlink chain escape", filepath.Join("a", "secret.txt"), true},
		{"symlink inside root ok", "oklink", false},
		{"nonexistent under root ok", filepath.Join("brand", "new", "file.go"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := a.denyTrustedRoot(tc.path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("denyTrustedRoot(%q) err=%v wantErr=%v", tc.path, err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, ErrTrustedRoot) {
				t.Errorf("escape denial must wrap ErrTrustedRoot, got %v", err)
			}
		})
	}
}

func TestApply_DeniesTrustedRoots(t *testing.T) {
	cases := []struct {
		name     string
		filePath string
	}{
		{"security engine", "internal/security/engine.go"},
		{"eval record", "internal/eval/record.go"},
		{"applier self-update", "internal/selfimprove/applier.go"},
		{"validator self-update", "internal/selfimprove/validator.go"},
		{"ssh traversal", "../../.ssh/id_rsa"},
		{"absolute passwd", "/etc/passwd"},
		{"gate config", "config/meept.json5"},
		{"employee gate config", "config/employees/ci-monitor.json5"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _, backupDir := newTestApplier(t)
			fix := &ProposedFix{
				ID:       "fix_deny",
				Type:     FixTypeCodeChange,
				FilePath: tc.filePath,
				Risk:     "low",
				Diff:     testDiff("original", "fixed"),
			}

			applied, err := a.Apply(context.Background(), fix, nil, "human")
			if err == nil {
				t.Fatalf("Apply(%q) succeeded, want trusted-root denial", tc.filePath)
			}
			if applied != nil {
				t.Errorf("Apply(%q) applied=%v, want nil", tc.filePath, applied)
			}
			if !errors.Is(err, ErrTrustedRoot) {
				t.Errorf("Apply(%q) err=%v, want errors.Is ErrTrustedRoot", tc.filePath, err)
			}

			// No backup may have been written: a traversal target's contents
			// must never be copied into the backup dir (exfil channel).
			entries, readErr := os.ReadDir(backupDir)
			if readErr != nil {
				t.Fatalf("read backup dir: %v", readErr)
			}
			if len(entries) != 0 {
				t.Errorf("backup dir not empty after denial: %d entries", len(entries))
			}
		})
	}
}

// TestDenyGateFrontmatter pins the C5 section-level guard: roster AGENT.md
// prompt bodies stay writable, but any diff containing a YAML frontmatter
// delimiter is rejected, and non-AGENT.md paths are untouched by it.
func TestDenyGateFrontmatter(t *testing.T) {
	a, _, _ := newTestApplier(t)

	if err := denyGateFrontmatter("config/agents/coder/AGENT.md", "no frontmatter in body edit"); err != nil {
		t.Errorf("body-only edit denied: %v", err)
	}
	if err := denyGateFrontmatter("config/agents/coder/AGENT.md", "line\n---\nline"); !errors.Is(err, ErrTrustedRoot) {
		t.Errorf("frontmatter edit not denied (err=%v)", err)
	}
	if err := denyGateFrontmatter("config/agents/coder/AGENT.md", "---\nname: coder\n---\nbody"); !errors.Is(err, ErrTrustedRoot) {
		t.Errorf("leading frontmatter edit not denied (err=%v)", err)
	}
	// Non-AGENT.md paths are the denyTrustedRoot pass's business, not this one.
	if err := denyGateFrontmatter("config/agents/coder/NOTES.md", "---\nx\n---\n"); err != nil {
		t.Errorf("non-AGENT.md path denied by frontmatter guard: %v", err)
	}
	// denyTrustedRoot must NOT blanket-deny AGENT.md paths (bodies stay writable).
	if err := a.denyTrustedRoot(filepath.Join("config", "agents", "coder", "AGENT.md")); err != nil {
		t.Errorf("AGENT.md body path blanket-denied: %v", err)
	}
	if err := a.denyTrustedRoot(filepath.Join("config", "agents", "coder", "other.go")); err != nil {
		t.Errorf("non-AGENT.md file under config/agents denied: %v", err)
	}
}

func TestApply_AllowsSkillWrite(t *testing.T) {
	a, root, backupDir := newTestApplier(t)

	relPath := filepath.Join("config", "skills", "my-skill.md")
	target := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	original := "does the thing\n"
	if err := os.WriteFile(target, []byte(original), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	fix := &ProposedFix{
		ID:       "fix_skill",
		Type:     FixTypeConfigChange,
		FilePath: relPath,
		Risk:     "low",
		Diff:     testDiff("does the thing", "does the thing better"),
	}

	applied, err := a.Apply(context.Background(), fix, nil, "human")
	if err != nil {
		t.Fatalf("Apply skill write failed: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "does the thing better\n" {
		t.Errorf("target content = %q, want patched content", got)
	}

	if !isWithinDir(backupDir, applied.BackupPath) {
		t.Errorf("backup path %q escapes backup dir %q", applied.BackupPath, backupDir)
	}
	backupContent, err := os.ReadFile(applied.BackupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backupContent) != original {
		t.Errorf("backup content = %q, want original %q", backupContent, original)
	}
}

func TestCreateBackup_BackupDestStaysInBackupDir(t *testing.T) {
	a, root, backupDir := newTestApplier(t)

	relPath := filepath.Join("config", "skills", "tool.md")
	target := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("body\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	nested := filepath.Join(root, "sub", "dir", "file.go")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(nested, []byte("nested body\n"), 0o644); err != nil {
		t.Fatalf("write nested: %v", err)
	}

	cases := []struct {
		name     string
		fixID    string
		filePath string
		wantBase string
	}{
		{"traversal fix id", "../../evil", relPath, ".._.._evil_tool.md.backup"},
		{"slashy fix id", "a/b", relPath, "a_b_tool.md.backup"},
		{"nested file uses base only", "f9", filepath.Join("sub", "dir", "file.go"), "f9_file.go.backup"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fix := &ProposedFix{ID: tc.fixID, FilePath: tc.filePath}
			backupPath, err := a.createBackup(fix)
			if err != nil {
				t.Fatalf("createBackup: %v", err)
			}
			if filepath.Base(backupPath) != tc.wantBase {
				t.Errorf("backup base = %q, want %q", filepath.Base(backupPath), tc.wantBase)
			}
			if !isWithinDir(backupDir, backupPath) {
				t.Errorf("backup path %q escapes backup dir %q", backupPath, backupDir)
			}
			if err := os.Remove(backupPath); err != nil {
				t.Fatalf("cleanup backup: %v", err)
			}
		})
	}

	// Nothing may exist outside backupDir from the traversal-ID case.
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("backup dir has %d leftover entries, want 0", len(entries))
	}
}

func TestApply_ApprovePathAlsoDenied(t *testing.T) {
	a, _, _ := newTestApplier(t)
	a.config.RequireHumanApproval = true

	fix := &ProposedFix{
		ID:       "fix_gate",
		Type:     FixTypeCodeChange,
		FilePath: "internal/security/engine.go",
		Risk:     "low",
		Diff:     testDiff("original", "fixed"),
	}

	// Queued for approval (Apply returns ErrApprovalRequired first).
	if _, err := a.Apply(context.Background(), fix, nil, "agent"); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("Apply err=%v, want ErrApprovalRequired", err)
	}

	// But approving must NOT apply it — the deny check guards applyFix too.
	if _, err := a.Approve(context.Background(), fix.ID); !errors.Is(err, ErrTrustedRoot) {
		t.Errorf("Approve err=%v, want errors.Is ErrTrustedRoot", err)
	}
}
