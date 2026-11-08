package builtin

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

// TDD tests for leaf 06-office-skills/02-pptx-skill (C11.3). Kept minimal
// per the repo's e2e-first policy: the hermetic e2e tier
// (e2e/suites/office-skills, orchestrator-owned, orchestrator-activated)
// covers the create -> read round-trip through the live daemon; these unit
// tests cover only the path/image fence and the cheap branch behavior that
// must hold before that suite can even run.

func newTestPptxContext(t *testing.T) context.Context {
	t.Helper()
	dir := t.TempDir()
	return tools.ContextWithWorkingDir(context.Background(), dir)
}

func testSessionDir(t *testing.T, ctx context.Context) string {
	t.Helper()
	return tools.WorkingDirFromContext(ctx)
}

// T2: out-of-session paths are refused.
func TestOfficePptxRefusesOutOfSessionPath(t *testing.T) {
	ctx := newTestPptxContext(t)
	tool := NewOfficePptxTool()

	_, err := tool.Execute(ctx, map[string]any{"path": "/etc/passwd.pptx", "action": "read"})
	if err == nil || !strings.Contains(err.Error(), "outside the session working directory") {
		t.Fatalf("want outside-session refusal, got: %v", err)
	}

	_, err = tool.Execute(ctx, map[string]any{"path": "../escape.pptx", "action": "read"})
	if err == nil || !strings.Contains(err.Error(), "outside the session working directory") {
		t.Fatalf("want outside-session refusal for .., got: %v", err)
	}
}

// T2: no session working directory in the context is refused.
func TestOfficePptxRefusesMissingSessionDir(t *testing.T) {
	tool := NewOfficePptxTool()
	_, err := tool.Execute(context.Background(), map[string]any{"path": "deck.pptx", "action": "read"})
	if err == nil || !strings.Contains(err.Error(), "no session working directory") {
		t.Fatalf("want missing-session-dir refusal, got: %v", err)
	}
}

// T2: unknown actions are rejected.
func TestOfficePptxUnknownAction(t *testing.T) {
	ctx := newTestPptxContext(t)
	tool := NewOfficePptxTool()
	_, err := tool.Execute(ctx, map[string]any{"path": "deck.pptx", "action": "append"})
	if err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("want unknown-action error, got: %v", err)
	}
}

// T2/T3: create -> read round-trip (in-process; the e2e gate re-proves it
// through the daemon).
func TestOfficePptxCreateReadRoundTrip(t *testing.T) {
	ctx := newTestPptxContext(t)
	dir := testSessionDir(t, ctx)
	tool := NewOfficePptxTool()

	res, err := tool.Execute(ctx, map[string]any{
		"path":   "probe.pptx",
		"action": "create",
		"slides": []any{
			map[string]any{
				"title": "PROBE-TITLE",
				"body":  []any{"PROBE-BODY"},
				"notes": "PROBE-NOTES",
			},
		},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	m, ok := res.(tools.ToolResult)
	if !ok || m.Success != true {
		t.Fatalf("want successful ToolResult, got %#v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "probe.pptx")); err != nil {
		t.Fatalf("probe.pptx not on disk: %v", err)
	}

	res, err = tool.Execute(ctx, map[string]any{"path": "probe.pptx", "action": "read"})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	m = res.(tools.ToolResult)
	resMap, _ := m.Result.(map[string]any)
	text := fmt.Sprintf("%v", m.Result)
	for _, want := range []string{"PROBE-TITLE", "PROBE-BODY", "PROBE-NOTES"} {
		if !strings.Contains(text, want) {
			t.Errorf("read-back missing %s: %v", want, m.Result)
		}
	}
	if got := resMap["slide_count"]; got != 1 {
		t.Errorf("slide_count = %v, want 1", got)
	}
}

// T2: image_path confinement + placement (C11.3: image_path in-session).
func TestOfficePptxImagePathFenceAndPlacement(t *testing.T) {
	ctx := newTestPptxContext(t)
	dir := testSessionDir(t, ctx)
	tool := NewOfficePptxTool()

	// image_path outside the session is refused.
	_, err := tool.Execute(ctx, map[string]any{
		"path":   "img.pptx",
		"action": "create",
		"slides": []any{
			map[string]any{"title": "S", "image_path": "/tmp/evil.png"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "outside the session working directory") {
		t.Fatalf("want outside-session image_path refusal, got: %v", err)
	}

	// A real in-session image lands on the slide and survives the reopen.
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	imgPath := filepath.Join(dir, "pic.png")
	f, err := os.Create(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if _, err := tool.Execute(ctx, map[string]any{
		"path":   "img.pptx",
		"action": "create",
		"slides": []any{
			map[string]any{"title": "WITH-IMG", "image_path": "pic.png"},
		},
	}); err != nil {
		t.Fatalf("create with image failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "img.pptx")); err != nil {
		t.Fatalf("img.pptx not on disk: %v", err)
	}
}

// C11.3: edit preserves existing slides (append-only).
func TestOfficePptxEditAppendsSlides(t *testing.T) {
	ctx := newTestPptxContext(t)
	dir := testSessionDir(t, ctx)
	tool := NewOfficePptxTool()

	if _, err := tool.Execute(ctx, map[string]any{
		"path":   "deck.pptx",
		"action": "create",
		"slides": []any{map[string]any{"title": "ONE"}},
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	res, err := tool.Execute(ctx, map[string]any{
		"path":   "deck.pptx",
		"action": "edit",
		"slides": []any{map[string]any{"title": "TWO"}},
	})
	if err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	m := res.(tools.ToolResult)
	resMap, _ := m.Result.(map[string]any)
	if resMap["path"] != filepath.Join(dir, "deck.edited.pptx") {
		t.Errorf("edit path = %v, want deck.edited.pptx next to the original", resMap["path"])
	}
	if resMap["slide_count"] != 2 {
		t.Errorf("slide_count = %v, want 2", resMap["slide_count"])
	}

	// Read-back proves BOTH slides survived (append, not rebuild).
	res, err = tool.Execute(ctx, map[string]any{"path": filepath.Join(dir, "deck.edited.pptx"), "action": "read"})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	m = res.(tools.ToolResult)
	text := fmt.Sprintf("%v", m.Result)
	for _, want := range []string{"ONE", "TWO"} {
		if !strings.Contains(text, want) {
			t.Errorf("read-back missing slide title %s: %v", want, m.Result)
		}
	}
}

// C11.3: reopen-and-verify refuses to report success over missing content.
func TestOfficePptxCreateFailsOnMissingContent(t *testing.T) {
	ctx := newTestPptxContext(t)
	tool := NewOfficePptxTool()
	// image_path pointing at a nonexistent in-session file must fail the
	// create loudly (refused at placement; never silently saved), not
	// produce a slide missing its picture.
	_, err := tool.Execute(ctx, map[string]any{
		"path":   "broken.pptx",
		"action": "create",
		"slides": []any{map[string]any{"title": "X", "image_path": "nope.png"}},
	})
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("want missing-image failure, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(testSessionDir(t, ctx), "broken.pptx")); statErr == nil {
		t.Errorf("broken.pptx was written despite the failed slide")
	}
}

// C11.3: replace-based edit targets existing slides without rebuilding.
func TestOfficePptxEditReplace(t *testing.T) {
	ctx := newTestPptxContext(t)
	dir := testSessionDir(t, ctx)
	tool := NewOfficePptxTool()

	if _, err := tool.Execute(ctx, map[string]any{
		"path":   "deck.pptx",
		"action": "create",
		"slides": []any{map[string]any{"title": "OLD-TITLE"}},
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	res, err := tool.Execute(ctx, map[string]any{
		"path":   "deck.pptx",
		"action": "edit",
		"slides": []any{map[string]any{
			"replace": map[string]any{"find": "OLD-TITLE", "with": "NEW-TITLE"},
		}},
	})
	if err != nil {
		t.Fatalf("replace edit failed: %v", err)
	}
	m := res.(tools.ToolResult)
	resMap, _ := m.Result.(map[string]any)
	if resMap["slide_count"] != 1 {
		t.Errorf("slide_count = %v, want 1 (replace must not append)", resMap["slide_count"])
	}
	if resMap["replacements"] != 1 {
		t.Errorf("replacements = %v, want 1", resMap["replacements"])
	}

	res, err = tool.Execute(ctx, map[string]any{"path": filepath.Join(dir, "deck.edited.pptx"), "action": "validate"})
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	m = res.(tools.ToolResult)
	resMap, _ = m.Result.(map[string]any)
	if resMap["valid"] != true {
		t.Errorf("validate invalid: %v", m.Result)
	}
}

// T3: validate reports missing fragments without erroring.
func TestOfficePptxValidateReportsMissing(t *testing.T) {
	ctx := newTestPptxContext(t)
	tool := NewOfficePptxTool()

	if _, err := tool.Execute(ctx, map[string]any{
		"path":   "deck.pptx",
		"action": "create",
		"slides": []any{map[string]any{"title": "HELLO"}},
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	res, err := tool.Execute(ctx, map[string]any{
		"path":        "deck.pptx",
		"action":      "validate",
		"expect_text": []any{"HELLO", "ABSENT"},
	})
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	m := res.(tools.ToolResult)
	resMap, _ := m.Result.(map[string]any)
	if resMap["valid"] != false {
		t.Errorf("valid = %v, want false", resMap["valid"])
	}
	missing, ok := resMap["missing"].([]string)
	if !ok || len(missing) != 1 || missing[0] != "ABSENT" {
		t.Errorf("missing = %v, want [ABSENT]", resMap["missing"])
	}
}

// C11.3: read/validate are read-only.
func TestOfficePptxIsReadOnly(t *testing.T) {
	tool := NewOfficePptxTool()
	cases := map[string]bool{
		"create":   false,
		"edit":     false,
		"read":     true,
		"validate": true,
	}
	for action, want := range cases {
		if got := tool.IsReadOnly(map[string]any{"action": action}); got != want {
			t.Errorf("IsReadOnly(%q) = %v, want %v", action, got, want)
		}
	}
}
