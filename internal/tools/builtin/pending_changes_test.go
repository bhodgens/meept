package builtin
import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageWrite(t *testing.T) {
	dir := t.TempDir()
	reg := NewPendingChangesRegistry()

	t.Run("hash and diff correctness", func(t *testing.T) {
		path := filepath.Join(dir, "stage1.txt")
		if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0o644); err != nil { //nolint:gosec // test temp dir
			t.Fatal(err)
		}
		original := "alpha\nbeta\n"
		modified := "alpha\ngamma\n"

		change, err := reg.StageWrite("sess-1", path, []byte(original), []byte(modified))
		if err != nil {
			t.Fatalf("StageWrite failed: %v", err)
		}
		if change.ID == "" {
			t.Error("expected non-empty change ID")
		}
		if change.SessionID != "sess-1" {
			t.Errorf("SessionID = %q, want %q", change.SessionID, "sess-1")
		}
		if change.FilePath != path {
			t.Errorf("FilePath = %q, want %q", change.FilePath, path)
		}
		if change.Original != original {
			t.Errorf("Original = %q, want %q", change.Original, original)
		}
		if change.Modified != modified {
			t.Errorf("Modified = %q, want %q", change.Modified, modified)
		}
		wantHash := sha256Hex(original)
		if change.PreImageSHA256 != wantHash {
			t.Errorf("PreImageSHA256 = %q, want %q", change.PreImageSHA256, wantHash)
		}
		if !strings.Contains(change.Diff, "-beta") || !strings.Contains(change.Diff, "+gamma") {
			t.Errorf("Diff missing expected hunks:\n%s", change.Diff)
		}

		got, ok := reg.Get(change.ID)
		if !ok {
			t.Fatalf("Get(%q) not found after StageWrite", change.ID)
		}
		if got.PreImageSHA256 != wantHash {
			t.Errorf("registry copy PreImageSHA256 = %q, want %q", got.PreImageSHA256, wantHash)
		}
	})

	t.Run("visible via GetBySession", func(t *testing.T) {
		path := filepath.Join(dir, "stage2.txt")
		change, err := reg.StageWrite("sess-2", path, []byte("x"), []byte("y"))
		if err != nil {
			t.Fatalf("StageWrite failed: %v", err)
		}
		changes := reg.GetBySession("sess-2")
		found := false
		for _, c := range changes {
			if c.ID == change.ID {
				found = true
			}
		}
		if !found {
			t.Errorf("change %s not visible via GetBySession(sess-2)", change.ID)
		}
	})

	t.Run("empty-original create case hashes empty string", func(t *testing.T) {
		path := filepath.Join(dir, "newfile.txt")
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("test precondition: %s should not exist", path)
		}
		change, err := reg.StageWrite("sess-3", path, nil, []byte("brand new content"))
		if err != nil {
			t.Fatalf("StageWrite failed: %v", err)
		}
		if change.PreImageSHA256 != sha256Hex("") {
			t.Errorf("PreImageSHA256 for empty pre-image = %q, want sha256(\"\") = %q",
				change.PreImageSHA256, sha256Hex(""))
		}
	})

	t.Run("diff is stable against generateDiffPreview", func(t *testing.T) {
		path := filepath.Join(dir, "stage4.txt")
		editTool := &FileEditTool{}
		ref := editTool.generateDiffPreview(path, "one\ntwo\nthree", "one\nTWO\nthree\nfour")

		change, err := reg.StageWrite("sess-4", path, []byte("one\ntwo\nthree"), []byte("one\nTWO\nthree\nfour"))
		if err != nil {
			t.Fatalf("StageWrite failed: %v", err)
		}
		if change.Diff != ref {
			t.Errorf("Diff mismatch with generateDiffPreview reference.\nStageWrite:\n%s\nReference:\n%s", change.Diff, ref)
		}
	})
}
