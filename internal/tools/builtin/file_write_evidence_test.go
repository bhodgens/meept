package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
)

// F-C5 pin: file_write attaches verification-grade evidence (path, size, hash,
// timestamp) so the claim-vs-evidence validator can accept a write claim without
// any file_read read-back.
func TestFileWriteAttachesVerificationEvidence(t *testing.T) {
	dir := t.TempDir()
	realDir, err := filepath.EvalSymlinks(dir) // macOS /var -> /private/var
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(realDir, "hello.txt")

	tool := NewWriteFileTool(nil)
	res, err := tool.Execute(context.Background(), map[string]any{
		"path":    path,
		"content": "hello world\n",
	})
	if err != nil {
		t.Fatalf("file_write failed: %v", err)
	}
	tr, ok := res.(tools.ToolResult)
	if !ok {
		t.Fatalf("expected tools.ToolResult, got %T", res)
	}
	if !tr.Success {
		t.Fatalf("expected success, got error: %s", tr.Error)
	}

	var exists, hash *models.Evidence
	for i := range tr.Evidence {
		switch tr.Evidence[i].Type {
		case models.EvidenceFileExists:
			exists = &tr.Evidence[i]
		case models.EvidenceFileHash:
			hash = &tr.Evidence[i]
		}
	}
	if exists == nil {
		t.Fatalf("no file_exists evidence; got %v", tr.Evidence)
	}
	if hash == nil {
		t.Fatalf("no file_hash evidence; got %v", tr.Evidence)
	}
	if exists.Subject != path {
		t.Errorf("file_exists subject = %q, want %q", exists.Subject, path)
	}
	if exists.Value != "size=12" {
		t.Errorf("file_exists value = %q, want size=12", exists.Value)
	}
	if exists.Timestamp.IsZero() {
		t.Error("file_exists evidence timestamp is zero")
	}
	if exists.Source != "file_write" {
		t.Errorf("evidence source = %q, want file_write", exists.Source)
	}
	sum := sha256.Sum256([]byte("hello world\n"))
	wantHash := hex.EncodeToString(sum[:])
	if hash.Value != wantHash {
		t.Errorf("file_hash value = %q, want %q", hash.Value, wantHash)
	}
	if hash.Subject != path {
		t.Errorf("file_hash subject = %q, want %q", hash.Subject, path)
	}

	// Description must tell the model the evidence IS the proof.
	if !strings.Contains(tool.Description(), "do not re-read the file to verify") {
		t.Errorf("file_write description missing no-re-read line: %s", tool.Description())
	}
}

// F-C5 pin: file_edit (direct mode) attaches the same verification-grade
// evidence and the no-re-read guidance.
func TestFileEditAttachesVerificationEvidence(t *testing.T) {
	dir := t.TempDir()
	realDir, err := filepath.EvalSymlinks(dir) // macOS /var -> /private/var
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(realDir, "edit.txt")
	if err := os.WriteFile(path, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cache := NewReadCache(10)
	tool := NewFileEditTool(nil, cache)
	// Read first so the anchor hash is registered the same way production flows do.
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cache.Store(path, strings.Split(strings.TrimSuffix(string(content), "\n"), "\n"))

	res, err := tool.Execute(context.Background(), map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{
				"op":      "replace",
				"anchor":  "1:" + ComputeLineHash("line one"),
				"content": "line ONE",
			},
		},
	})
	if err != nil {
		t.Fatalf("file_edit failed: %v", err)
	}
	tr, ok := res.(tools.ToolResult)
	if !ok {
		t.Fatalf("expected tools.ToolResult, got %T", res)
	}
	if !tr.Success {
		t.Fatalf("expected success, got error: %s", tr.Error)
	}

	var exists, hash *models.Evidence
	for i := range tr.Evidence {
		switch tr.Evidence[i].Type {
		case models.EvidenceFileExists:
			exists = &tr.Evidence[i]
		case models.EvidenceFileHash:
			hash = &tr.Evidence[i]
		}
	}
	if exists == nil || hash == nil {
		t.Fatalf("file_edit evidence missing file_exists/file_hash; got %v", tr.Evidence)
	}
	if exists.Subject != path || hash.Subject != path {
		t.Errorf("evidence subjects = %q / %q, want %q", exists.Subject, hash.Subject, path)
	}
	// file_edit joins the resulting lines with "\n" (no trailing newline).
	sum := sha256.Sum256([]byte("line ONE\nline two"))
	if hash.Value != hex.EncodeToString(sum[:]) {
		t.Errorf("file_hash value = %q, want sha256 of edited content", hash.Value)
	}
	if !strings.Contains(tool.Description(), "do not re-read the file to verify") {
		t.Errorf("file_edit description missing no-re-read line: %s", tool.Description())
	}
}

// F-C6 pin: task_get description names task_id explicitly as required; the
// schema carries it in Required.
func TestTaskGetDescriptionNamesRequiredArg(t *testing.T) {
	tool := NewTaskGetTool(nil)
	desc := tool.Description()
	if !strings.Contains(desc, "task_id") {
		t.Errorf("task_get description does not name task_id: %s", desc)
	}
	if !strings.Contains(desc, "required") {
		t.Errorf("task_get description does not mark the argument required: %s", desc)
	}
	params := tool.Parameters()
	found := false
	for _, r := range params.Required {
		if r == "task_id" {
			found = true
		}
	}
	if !found {
		t.Errorf("task_get Required = %v, want it to contain task_id", params.Required)
	}
	if _, ok := params.Properties["task_id"]; !ok {
		t.Errorf("task_get properties missing task_id: %v", params.Properties)
	}
}
