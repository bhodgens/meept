package validator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// TestFilesystemValidatorDirectorySubjectPasses pins the semantics fix
// from bench run 45514312978: list_directory issues file_exists evidence
// with the listed DIRECTORY as subject, and the validator previously
// rejected any directory ("path is a directory, not a file" -> reported
// as "file not found"), failing legitimate plan steps whose first action
// was listing the working directory. Existence of a directory satisfies
// the file_exists claim.
func TestFilesystemValidatorDirectorySubjectPasses(t *testing.T) {
	dir := t.TempDir()
	v := NewFilesystemValidator()

	step := &task.TaskStep{
		Evidence: []models.Evidence{
			models.NewEvidence(models.EvidenceFileExists, dir, "entries=1,recursive=false,truncated=false", "list_directory"),
		},
	}
	res := v.Validate(context.Background(), step)
	if !res.Valid {
		t.Fatalf("directory subject must satisfy file_exists, got errors: %v", res.Errors)
	}
}

// TestFilesystemValidatorDeletedPathPasses pins the file_delete polarity
// fix (bughunt 2026-11-24 P1): DeleteFileTool used to emit file_exists for
// the path it had just removed, so this validator failed EVERY file_delete
// step as "file not found" and the tactical retry loop exhausted. The tool
// now emits EvidenceFileDeleted; the validator asserts the path is GONE.
func TestFilesystemValidatorDeletedPathPasses(t *testing.T) {
	dir := t.TempDir()
	gone := filepath.Join(dir, "removed.txt")
	still := filepath.Join(dir, "still-here.txt")
	if err := os.WriteFile(still, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := NewFilesystemValidator()

	// A deleted path satisfies file_deleted.
	res := v.Validate(context.Background(), &task.TaskStep{
		Evidence: []models.Evidence{
			models.NewEvidence(models.EvidenceFileDeleted, gone, "removed", "file_delete"),
		},
	})
	if !res.Valid {
		t.Fatalf("a removed path must satisfy file_deleted, got errors: %v", res.Errors)
	}

	// A path that still exists fails file_deleted.
	res = v.Validate(context.Background(), &task.TaskStep{
		Evidence: []models.Evidence{
			models.NewEvidence(models.EvidenceFileDeleted, still, "removed", "file_delete"),
		},
	})
	if res.Valid {
		t.Fatalf("a path that still exists must fail file_deleted")
	}
	if len(res.Errors) == 0 || !strings.Contains(res.Errors[0], "still exists") {
		t.Fatalf("expected 'still exists' error, got %v", res.Errors)
	}
}

// TestFilesystemValidatorMissingPathStillFails guards the other side:
// a nonexistent path must still fail.
func TestFilesystemValidatorMissingPathStillFails(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.txt")
	v := NewFilesystemValidator()
	step := &task.TaskStep{
		Evidence: []models.Evidence{
			models.NewEvidence(models.EvidenceFileExists, missing, "written", "file_write"),
		},
	}
	res := v.Validate(context.Background(), step)
	if res.Valid {
		t.Fatalf("missing path must fail file_exists validation")
	}
	if len(res.Errors) == 0 || !strings.Contains(res.Errors[0], "file not found") {
		t.Fatalf("expected 'file not found' error, got %v", res.Errors)
	}
}
