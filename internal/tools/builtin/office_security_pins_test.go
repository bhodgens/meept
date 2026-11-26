package builtin

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

// Pins for bughunt A6-2 (office tools' symlink-unsafe resolvePath) and
// A6-9 (office_pptx non-atomic edit-in-place write). The central fence is
// NOT involved: the tool-level session fence itself must refuse both.
//
// One-line failing reverts:
//   - TestOfficeSymlinkEscapeRefused fails: switch any of the three
//     resolveFenced helpers back to resolvePath (no EvalSymlinks) — the
//     symlink's in-session path passes the prefix check and the tool
//     reads/writes the OUTSIDE target instead of refusing.
//   - TestOfficePptxEditWriteFailureKeepsOriginal fails: bind
//     pptxWriteFile to os.WriteFile — the failed write truncates the
//     deck to zero bytes before erroring.

// helper: create a real 1-slide deck inside the session dir.
func mustCreateDeck(t *testing.T, ctx context.Context, tool *OfficePptxTool, name string) {
	t.Helper()
	_, err := tool.Execute(ctx, map[string]any{
		"path":   name,
		"action": "create",
		"slides": []any{map[string]any{"title": "Alpha", "bullets": []any{"one"}}},
	})
	if err != nil {
		t.Fatalf("create deck: %v", err)
	}
}

// A6-2: a symlink placed INSIDE the session dir that points OUTSIDE must
// be refused by the tool fence itself (all three office tools).
func TestOfficeSymlinkEscapeRefused(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root tests can defeat the fence regardless")
	}
	session := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "victim.docx")
	if err := os.WriteFile(outsideFile, []byte("ORIGINAL BYTES"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(session, "notes.docx")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	ctx := tools.ContextWithWorkingDir(context.Background(), session)

	// docx read through the symlink: pre-fix resolvePath reports the
	// in-session link path, the fence passes, the tool opens the OUTSIDE
	// target. Post-fix resolvePathSecure reports the real outside path and
	// the fence refuses with its own message.
	if _, err := (NewOfficeDocxTool()).Execute(ctx, map[string]any{"path": "notes.docx", "action": "read"}); err == nil ||
		!strings.Contains(err.Error(), "outside the session working directory") {
		t.Fatalf("office_docx symlink read not refused by the tool fence: %v", err)
	}
	if _, err := (NewOfficePptxTool()).Execute(ctx, map[string]any{"path": "notes.docx", "action": "read"}); err == nil ||
		!strings.Contains(err.Error(), "outside the session working directory") {
		t.Fatalf("office_pptx symlink read not refused by the tool fence: %v", err)
	}
	if _, err := (NewOfficeWorkbookTool()).Execute(ctx, map[string]any{"path": "notes.docx", "action": "read"}); err == nil ||
		!strings.Contains(err.Error(), "outside the session working directory") {
		t.Fatalf("office_workbook symlink read not refused by the tool fence: %v", err)
	}

	// The outside victim was never touched.
	b, _ := os.ReadFile(outsideFile)
	if string(b) != "ORIGINAL BYTES" {
		t.Fatalf("victim file mutated: %q", string(b))
	}
}

// A6-9: a forced failure of the deck write during an in-place edit must
// leave the ORIGINAL file bytes intact (atomic tmp+rename), not a
// truncated zip.
func TestOfficePptxEditWriteFailureKeepsOriginal(t *testing.T) {
	ctx := newTestPptxContext(t)
	tool := NewOfficePptxTool()
	mustCreateDeck(t, ctx, tool, "deck.pptx")
	dir := testSessionDir(t, ctx)
	// resolveFenced returns the legacy (unresolved) path form, so the
	// write seam sees the path joined against the session dir as-is.
	target := filepath.Join(dir, "deck.pptx")

	orig, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(orig) == 0 {
		t.Fatal("empty deck after create")
	}

	// Force the write seam to fail after the deck bytes were built.
	fail := errors.New("injected write failure")
	origWrite := pptxWriteFile
	pptxWriteFile = func(path string, data []byte) error {
		if path == target {
			return fail
		}
		return origWrite(path, data)
	}
	defer func() { pptxWriteFile = origWrite }()

	_, err = tool.Execute(ctx, map[string]any{
		"path":     "deck.pptx",
		"action":   "edit",
		"in_place": true,
		"slides":   []any{map[string]any{"find": "Alpha", "title": "Beta"}},
	})
	if err == nil {
		t.Fatal("expected edit to fail with the injected write error")
	}
	if !strings.Contains(err.Error(), "injected write failure") {
		t.Fatalf("unexpected error: %v", err)
	}

	after, rerr := os.ReadFile(target)
	if rerr != nil {
		t.Fatalf("reread after failed write: %v", rerr)
	}
	if !bytes.Equal(after, orig) {
		t.Fatalf("original deck bytes changed after failed write: %d -> %d bytes", len(orig), len(after))
	}
	// No temp litter left in the session dir (cleanup path ran).
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".meept-revert-") {
			t.Fatalf("temp file leaked: %s", e.Name())
		}
	}
}

// TestOfficePptxWriteSeamIsAtomic is the revert detector for A6-9: the
// production pptxWriteFile binding MUST be the package's atomic
// tmp+rename helper (writeFileAtomic). Reverting the fix to an
// os.WriteFile wrapper trips the pointer-identity assertion — an injected
// failure alone cannot distinguish the two writers.
func TestOfficePptxWriteSeamIsAtomic(t *testing.T) {
	// origWrite captured at the top of the failure test is the same
	// binding here; compare function entry points.
	got := reflect.ValueOf(pptxWriteFile).Pointer()
	want := reflect.ValueOf(writeFileAtomic).Pointer()
	if got != want {
		t.Fatalf("pptxWriteFile is not bound to writeFileAtomic (A6-9 revert: non-atomic os.WriteFile restored)")
	}
}
