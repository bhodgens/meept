package selfimprove

import (
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
	if err := os.MkdirAll(filepath.Join(projectRoot, "sub", "dir"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	relPath := filepath.Join("sub", "dir", "foo.go")
	nestedFile := filepath.Join(projectRoot, relPath)
	originalContent := []byte("package original\n")
	if err := os.WriteFile(nestedFile, originalContent, 0644); err != nil {
		t.Fatalf("write original file: %v", err)
	}

	// Backup directory (mirror applier layout).
	backupDir := filepath.Join(tmpDir, "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatalf("mkdir backup: %v", err)
	}
	backupPath := filepath.Join(backupDir, "fix1_foo.go.backup")
	if err := os.WriteFile(backupPath, originalContent, 0644); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	// Simulate modification to the nested file.
	if err := os.WriteFile(nestedFile, []byte("package modified\n"), 0644); err != nil {
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
	if string(got) != string(originalContent) {
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
