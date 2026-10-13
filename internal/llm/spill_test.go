package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestSpillCompactor builds a ContextCompactor with spill enabled and a
// temp MEEPT_HOME. Returns the compactor and the spill root it will use.
func newTestSpillCompactor(t *testing.T, spillEnabled bool) (*ContextCompactor, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)
	cfg := DefaultCompactorConfig()
	cfg.SpillEnabled = spillEnabled
	c := NewContextCompactor(cfg, nil, nil, nil)
	return c, filepath.Join(home, "spill")
}

func newSpillPruneMessages(oldContent string) []ChatMessage {
	recentOutput := makeLongString(39000)
	return []ChatMessage{
		{Role: RoleUser, Content: "do old work"},
		{Role: RoleAssistant, Content: "working", ToolCalls: []ToolCall{
			{ID: "tc_old", Function: ToolCallFunction{Name: "shell_execute", Arguments: `{}`}},
		}},
		{Role: RoleTool, Content: oldContent, ToolCallID: "tc_old"},
		{Role: RoleUser, Content: "do recent work"},
		{Role: RoleAssistant, Content: "working", ToolCalls: []ToolCall{
			{ID: "tc_recent", Function: ToolCallFunction{Name: "shell_execute", Arguments: `{}`}},
		}},
		{Role: RoleTool, Content: recentOutput, ToolCallID: "tc_recent"},
	}
}

func TestSpillServiceWritesAndReturnsPointer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)

	svc := NewSpillService("")
	content := strings.Repeat("tool output line\n", 300) // ~1800 tokens

	pointer, err := svc.Spill(context.Background(), "session-abc123", content)
	if err != nil {
		t.Fatalf("Spill returned error: %v", err)
	}

	wantPath := filepath.Join(home, "spill", "session-abc123")
	sum := sha256.Sum256([]byte(content))
	wantFile := filepath.Join(wantPath, hex.EncodeToString(sum[:])[:16]+".txt")

	// File exists on disk with the raw (unwrapped) content.
	got, err := os.ReadFile(wantFile)
	if err != nil {
		t.Fatalf("spill file not written: %v", err)
	}
	if string(got) != content {
		t.Error("spill file content must be the raw, unwrapped tool output")
	}

	// File mode 0600, dir mode 0700.
	info, err := os.Stat(wantFile)
	if err != nil {
		t.Fatalf("stat spill file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("spill file perm = %o, want 600", perm)
	}
	dirInfo, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("stat spill dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0700 {
		t.Errorf("spill dir perm = %o, want 700", perm)
	}

	// Exact pointer line with absolute path and token count (HeuristicTokenizer: 3 chars/token).
	tokens := (len(content) + 2) / 3
	want := fmt.Sprintf("[Output spilled to %s — re-read with the file tool if needed (%d tokens)]", wantFile, tokens)
	if pointer != want {
		t.Errorf("pointer line = %q, want %q", pointer, want)
	}
	if !filepath.IsAbs(wantFile) {
		t.Error("pointer path must be absolute")
	}
}

func TestSpillServiceSanitizesSessionID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)

	svc := NewSpillService("")
	hostile := "../../etc/passwd"

	pointer, err := svc.Spill(context.Background(), hostile, "content")
	if err != nil {
		t.Fatalf("Spill returned error: %v", err)
	}

	// Path must stay under the spill root; traversal components stripped.
	if strings.Contains(pointer, "..") {
		t.Errorf("pointer path escaped spill root: %q", pointer)
	}
	wantDir := filepath.Join(home, "spill", hostilesanitized(hostile))
	gotPath := pointer[len("[Output spilled to "):]
	if idx := strings.Index(gotPath, " — re-read"); idx >= 0 {
		gotPath = gotPath[:idx]
	}
	if !strings.HasPrefix(gotPath, wantDir+string(filepath.Separator)) {
		t.Errorf("pointer = %q, want it under %q", pointer, wantDir)
	}
}

func hostilesanitized(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func TestSpillDisabledReturnsTruncation(t *testing.T) {
	c, spillRoot := newTestSpillCompactor(t, false)
	largeOutput := makeLongString(30000)

	msgs := newSpillPruneMessages(largeOutput)
	result := c.pruneToolOutputs(msgs)

	// Old output replaced with the EXACT legacy truncation text — byte-identical
	// behavior when spill is disabled.
	stub := fmt.Sprintf("[Output truncated — %d tokens saved]", 30000)
	want := fmt.Sprintf("[Output truncated — %d tokens saved]", 30000-c.tokenizer.CountTokens(stub))
	if got := result[2].Content; got != want {
		t.Errorf("disabled spill should keep legacy truncation text, got: %q", got)
	}
	// Nothing spilled.
	if _, err := os.Stat(spillRoot); !os.IsNotExist(err) {
		t.Errorf("spill dir must not be created when spill disabled (stat err: %v)", err)
	}
}

func TestSpillWriteFailureFallsBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)

	// Make $MEEPT_HOME read-only so MkdirAll for spill/<session> fails.
	if err := os.Chmod(home, 0500); err != nil {
		t.Fatalf("chmod home: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0700) })

	cfg := DefaultCompactorConfig()
	cfg.SpillEnabled = true
	c := NewContextCompactor(cfg, nil, nil, nil)

	largeOutput := makeLongString(30000)
	msgs := newSpillPruneMessages(largeOutput)

	// Must NOT fail the turn: falls back to truncation text, no error out.
	result := c.pruneToolOutputs(msgs)
	if got := result[2].Content; !strings.Contains(got, "Output truncated") {
		t.Errorf("spill write failure should fall back to truncation text, got: %q", got)
	}
	if got := result[2].Content; strings.Contains(got, "Output spilled") {
		t.Errorf("spill pointer must not appear when the spill write failed, got: %q", got)
	}
}

func TestSpillEnabledPrunedOutputUsesPointer(t *testing.T) {
	c, spillRoot := newTestSpillCompactor(t, true)
	largeOutput := makeLongString(30000)

	msgs := newSpillPruneMessages(largeOutput)
	result := c.pruneToolOutputs(msgs)

	got := result[2].Content
	if !strings.HasPrefix(got, "[Output spilled to ") || !strings.Contains(got, "re-read with the file tool if needed") {
		t.Errorf("spill-enabled prune should use the pointer line, got: %q", got)
	}

	// Pointer path must point at the real spill file containing the raw output.
	start := len("[Output spilled to ")
	end := strings.Index(got, " — re-read")
	if end < 0 {
		t.Fatalf("pointer line malformed: %q", got)
	}
	path := got[start:end]
	if !filepath.IsAbs(path) || !strings.HasPrefix(path, spillRoot+string(filepath.Separator)) {
		t.Fatalf("pointer path %q not under spill root %q", path, spillRoot)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("spill file missing: %v", err)
	}
	if string(data) != largeOutput {
		t.Error("spilled content must be the raw tool output")
	}
	if strings.Contains(got, "Output truncated") {
		t.Error("pointer line must not contain the truncation text when spill succeeded")
	}

	// Recent protected output untouched.
	if result[5].Content != newSpillPruneMessages(largeOutput)[5].Content {
		t.Error("recent tool output within protected budget should not be pruned")
	}
}
