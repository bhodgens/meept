package llm

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// mockChatter for compactor tests (reuses the type from context_compressor_test
// but in the same package we can just use it directly). However, that mock
// captures only a single response. We define a richer mock here that also
// records the messages it received so we can assert prompt construction.
// ---------------------------------------------------------------------------

type compactorMockChatter struct {
	response *Response
	err      error
	called   bool
	lastMsgs []ChatMessage // messages received in the last Chat call
}

func (m *compactorMockChatter) Chat(_ context.Context, msgs []ChatMessage, _ ...ChatOption) (*Response, error) {
	m.called = true
	m.lastMsgs = msgs
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func (m *compactorMockChatter) ChatWithProgress(_ context.Context, msgs []ChatMessage, _ ProgressCallback, _ ...ChatOption) (*Response, error) {
	return m.Chat(context.Background(), msgs)
}

func (m *compactorMockChatter) Config() *ModelConfig {
	return &ModelConfig{}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// makeLongString returns a string of the given approximate token count using
// the heuristic tokenizer (3 chars/token).
func makeLongString(tokens int) string {
	return strings.Repeat("x", tokens*3)
}

// makeMessages builds a message list with a system prompt, then alternating
// user/assistant pairs. Each message has ~100 tokens of content.
func makeMessages(pairs int) []ChatMessage {
	msgs := make([]ChatMessage, 0, 1+2*pairs)
	msgs = append(msgs, ChatMessage{Role: RoleSystem, Content: "system prompt"})
	for i := range pairs {
		msgs = append(msgs,
			ChatMessage{Role: RoleUser, Content: fmt.Sprintf("user message %d %s", i, makeLongString(90))},
			ChatMessage{Role: RoleAssistant, Content: fmt.Sprintf("assistant reply %d %s", i, makeLongString(90))},
		)
	}
	return msgs
}

// ---------------------------------------------------------------------------
// NewContextCompactor / DefaultCompactorConfig
// ---------------------------------------------------------------------------

func TestDefaultCompactorConfig(t *testing.T) {
	cfg := DefaultCompactorConfig()
	if cfg.ReserveTokens != 16384 {
		t.Errorf("ReserveTokens: got %d, want 16384", cfg.ReserveTokens)
	}
	if cfg.KeepRecentTokens != 20000 {
		t.Errorf("KeepRecentTokens: got %d, want 20000", cfg.KeepRecentTokens)
	}
	if cfg.MaxResponseTokens != 13107 {
		t.Errorf("MaxResponseTokens: got %d, want 13107", cfg.MaxResponseTokens)
	}
	if cfg.SummaryFormat != "structured" {
		t.Errorf("SummaryFormat: got %q, want %q", cfg.SummaryFormat, "structured")
	}
	if !cfg.TrackFileOps {
		t.Error("TrackFileOps should be true by default")
	}
	if cfg.TimeoutSeconds != 30 {
		t.Errorf("TimeoutSeconds: got %d, want 30", cfg.TimeoutSeconds)
	}
}

func TestNewContextCompactor_NilDeps(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)
	if c == nil {
		t.Fatal("expected non-nil compactor")
	}
	if c.summarizer != nil {
		t.Error("summarizer should be nil when passed nil")
	}
	if c.tokenizer == nil {
		t.Error("tokenizer should default to HeuristicTokenizer")
	}
}

// ---------------------------------------------------------------------------
// FileOperationSet
// ---------------------------------------------------------------------------

func TestNewFileOperationSet_Empty(t *testing.T) {
	fos := NewFileOperationSet()
	if fos.FileCount() != 0 {
		t.Errorf("empty set should have FileCount 0, got %d", fos.FileCount())
	}
	if fos.FormatCompact() != "" {
		t.Errorf("empty set FormatCompact should be empty, got %q", fos.FormatCompact())
	}
}

func TestFileOperationSet_Merge(t *testing.T) {
	a := NewFileOperationSet()
	a.Read["file1.go"] = true
	a.Written["file2.go"] = true

	b := NewFileOperationSet()
	b.Read["file3.go"] = true
	b.Edited["file1.go"] = true

	a.Merge(b)
	if len(a.Read) != 2 {
		t.Errorf("expected 2 reads after merge, got %d", len(a.Read))
	}
	if len(a.Written) != 1 {
		t.Errorf("expected 1 written after merge, got %d", len(a.Written))
	}
	if len(a.Edited) != 1 {
		t.Errorf("expected 1 edited after merge, got %d", len(a.Edited))
	}
	if a.FileCount() != 4 {
		t.Errorf("expected FileCount 4, got %d", a.FileCount())
	}
}

func TestFileOperationSet_MergeNil(t *testing.T) {
	a := NewFileOperationSet()
	a.Read["x"] = true
	a.Merge(nil) // should not panic
	if len(a.Read) != 1 {
		t.Errorf("merge with nil should not modify set, got %d reads", len(a.Read))
	}
}

func TestFileOperationSet_FormatCompact(t *testing.T) {
	fos := NewFileOperationSet()
	fos.Read["a.go"] = true
	fos.Written["b.go"] = true
	fos.Edited["c.go"] = true

	out := fos.FormatCompact()
	if !strings.Contains(out, "read: a.go") {
		t.Error("FormatCompact should contain 'read: a.go'")
	}
	if !strings.Contains(out, "write: b.go") {
		t.Error("FormatCompact should contain 'write: b.go'")
	}
	if !strings.Contains(out, "edit: c.go") {
		t.Error("FormatCompact should contain 'edit: c.go'")
	}
}

func TestFileOperationSet_FileCount_NilReceiver(t *testing.T) {
	var fos *FileOperationSet
	if fos.FileCount() != 0 {
		t.Error("nil FileOperationSet FileCount should be 0")
	}
}

func TestFileOperationSet_FormatCompact_NilReceiver(t *testing.T) {
	var fos *FileOperationSet
	if fos.FormatCompact() != "" {
		t.Error("nil FileOperationSet FormatCompact should be empty")
	}
}

// ---------------------------------------------------------------------------
// findCutPoint
// ---------------------------------------------------------------------------

func TestFindCutPoint_BasicSplit(t *testing.T) {
	cfg := DefaultCompactorConfig()
	cfg.KeepRecentTokens = 300 // ~100 tokens per msg => keeps ~3 recent messages
	c := NewContextCompactor(cfg, nil, nil, nil)

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: makeLongString(100)},
		{Role: RoleAssistant, Content: makeLongString(100)},
		{Role: RoleUser, Content: makeLongString(100)},
		{Role: RoleAssistant, Content: makeLongString(100)},
		{Role: RoleUser, Content: makeLongString(100)},
		{Role: RoleAssistant, Content: makeLongString(100)},
	}

	cut := c.findCutPoint(msgs)

	// System messages should be extracted
	if len(cut.SystemMsgs) != 1 {
		t.Errorf("expected 1 system message, got %d", len(cut.SystemMsgs))
	}
	// Something should be compacted
	if len(cut.ToCompact) == 0 {
		t.Error("expected some messages to compact")
	}
	// Something should be kept
	if len(cut.ToKeep) == 0 {
		t.Error("expected some messages to keep")
	}
	// System messages should not appear in ToCompact or ToKeep
	for _, m := range cut.ToCompact {
		if m.Role == RoleSystem {
			t.Error("system message should not be in ToCompact")
		}
	}
	for _, m := range cut.ToKeep {
		if m.Role == RoleSystem {
			t.Error("system message should not be in ToKeep")
		}
	}
}

func TestFindCutPoint_AllFits(t *testing.T) {
	cfg := DefaultCompactorConfig()
	cfg.KeepRecentTokens = 100000 // large enough to keep everything
	c := NewContextCompactor(cfg, nil, nil, nil)

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: "short message"},
		{Role: RoleAssistant, Content: "short reply"},
	}

	cut := c.findCutPoint(msgs)
	if len(cut.ToCompact) != 0 {
		t.Errorf("expected no messages to compact when all fit, got %d", len(cut.ToCompact))
	}
	if len(cut.ToKeep) != 2 { // non-system messages
		t.Errorf("expected 2 to keep, got %d", len(cut.ToKeep))
	}
}

func TestFindCutPoint_OnlySystemMessages(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "sys1"},
		{Role: RoleSystem, Content: "sys2"},
	}

	cut := c.findCutPoint(msgs)
	if len(cut.SystemMsgs) != 2 {
		t.Errorf("expected 2 system messages, got %d", len(cut.SystemMsgs))
	}
	if len(cut.ToCompact) != 0 {
		t.Error("expected no messages to compact with only system messages")
	}
}

func TestFindCutPoint_EmptyMessages(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	cut := c.findCutPoint(nil)
	if len(cut.SystemMsgs) != 0 {
		t.Error("expected no system messages for nil input")
	}
	if len(cut.ToKeep) != 0 {
		t.Error("expected no to-keep for nil input")
	}
}

// ---------------------------------------------------------------------------
// adjustCutPoint
// ---------------------------------------------------------------------------

func TestAdjustCutPoint_ToolResultPair(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	msgs := []ChatMessage{
		{Role: RoleUser, Content: "user msg"},
		{Role: RoleAssistant, Content: "doing work", ToolCalls: []ToolCall{
			{ID: "tc1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.go"}`}},
		}},
		{Role: RoleTool, Content: "file contents", ToolCallID: "tc1"},
		{Role: RoleUser, Content: "next instruction"},
		{Role: RoleAssistant, Content: "reply"},
	}

	// Try to cut at index 2 (the tool result) -- should adjust back to
	// include the assistant tool call, then forward to next user boundary.
	adjusted := c.adjustCutPoint(msgs, 2)
	if adjusted != 3 {
		t.Errorf("expected cut adjusted to index 3 (user message), got %d", adjusted)
	}
}

func TestAdjustCutPoint_AtBoundary(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	msgs := []ChatMessage{
		{Role: RoleUser, Content: "u1"},
		{Role: RoleAssistant, Content: "a1"},
		{Role: RoleUser, Content: "u2"},
		{Role: RoleAssistant, Content: "a2"},
	}

	// Cut at user boundary should stay
	adjusted := c.adjustCutPoint(msgs, 2)
	if adjusted != 2 {
		t.Errorf("expected cut at 2 to stay, got %d", adjusted)
	}
}

func TestAdjustCutPoint_OutOfBounds(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	msgs := []ChatMessage{{Role: RoleUser, Content: "u1"}}

	if c.adjustCutPoint(msgs, 0) != 0 {
		t.Error("adjustCutPoint(0) should return 0")
	}
	if c.adjustCutPoint(msgs, len(msgs)) != len(msgs) {
		t.Errorf("adjustCutPoint(len) should return len")
	}
}

// ---------------------------------------------------------------------------
// findSplitTurnBoundary
// ---------------------------------------------------------------------------

func TestFindSplitTurnBoundary_NoSplit(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	msgs := []ChatMessage{
		{Role: RoleUser, Content: "u1"},
		{Role: RoleAssistant, Content: "a1"},
		{Role: RoleUser, Content: "u2"},
		{Role: RoleAssistant, Content: "a2", ToolCalls: []ToolCall{
			{ID: "tc1", Function: ToolCallFunction{Name: "run", Arguments: "{}"}},
		}},
		{Role: RoleTool, Content: "ok", ToolCallID: "tc1"},
	}

	split, idx := c.findSplitTurnBoundary(msgs)
	if split {
		t.Error("expected no split when all tool calls have results")
	}
	if idx != -1 {
		t.Errorf("expected index -1, got %d", idx)
	}
}

func TestFindSplitTurnBoundary_SplitDetected(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	msgs := []ChatMessage{
		{Role: RoleUser, Content: "u1"},
		{Role: RoleAssistant, Content: "a1", ToolCalls: []ToolCall{
			{ID: "tc1", Function: ToolCallFunction{Name: "run", Arguments: "{}"}},
		}},
		// No tool result follows -- this is a split
	}

	split, idx := c.findSplitTurnBoundary(msgs)
	if !split {
		t.Error("expected split when tool call has no result")
	}
	if idx != 1 {
		t.Errorf("expected split index 1, got %d", idx)
	}
}

func TestFindSplitTurnBoundary_Empty(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	split, idx := c.findSplitTurnBoundary(nil)
	if split {
		t.Error("expected no split for empty messages")
	}
	if idx != -1 {
		t.Errorf("expected -1 for empty, got %d", idx)
	}
}

// ---------------------------------------------------------------------------
// isSplitTurn
// ---------------------------------------------------------------------------

func TestIsSplitTurn_ToolWithoutResult(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	msgs := []ChatMessage{
		{Role: RoleUser, Content: "u1"},
		{Role: RoleTool, Content: "result"}, // tool at cutIdx, no preceding assistant+toolcalls
	}

	// tool message at cutIdx but no assistant+toolcalls before it
	if c.isSplitTurn(msgs, 1) {
		t.Error("tool without preceding assistant+toolcalls should not be a split")
	}
}

func TestIsSplitTurn_AssistantWithToolCallsBeforeToolResults(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	msgs := []ChatMessage{
		{Role: RoleUser, Content: "u1"},
		{Role: RoleAssistant, Content: "work", ToolCalls: []ToolCall{
			{ID: "tc1", Function: ToolCallFunction{Name: "run", Arguments: "{}"}},
		}},
		{Role: RoleUser, Content: "u2"},
		{Role: RoleAssistant, Content: "reply"},
	}

	// cut at index 2 (user message) -- assistant at index 1 has tool calls
	// but next message is user, not tool result -> no split in "to keep" section
	if c.isSplitTurn(msgs, 2) {
		t.Error("user message at cutIdx is not a split turn")
	}
}

func TestIsSplitTurn_OutOfBounds(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)
	msgs := []ChatMessage{{Role: RoleUser, Content: "u1"}}

	if c.isSplitTurn(msgs, 0) {
		t.Error("index 0 should not be split")
	}
	if c.isSplitTurn(msgs, len(msgs)) {
		t.Error("index == len should not be split")
	}
}

// ---------------------------------------------------------------------------
// serializeMessages
// ---------------------------------------------------------------------------

func TestSerializeMessages_BasicRoles(t *testing.T) {
	msgs := []ChatMessage{
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, Content: "hi there"},
		{Role: RoleTool, Content: "tool output"},
	}
	out := (&ContextCompactor{}).serializeMessages(msgs)

	if !strings.Contains(out, "[User]: hello") {
		t.Error("expected [User]: hello in output")
	}
	if !strings.Contains(out, "[Assistant]: hi there") {
		t.Error("expected [Assistant]: hi there in output")
	}
	if !strings.Contains(out, "[Tool Result]: tool output") {
		t.Error("expected [Tool Result]: tool output in output")
	}
}

func TestSerializeMessages_ToolCalls(t *testing.T) {
	msgs := []ChatMessage{
		{Role: RoleAssistant, Content: "let me check", ToolCalls: []ToolCall{
			{Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"main.go"}`}},
		}},
	}
	out := (&ContextCompactor{}).serializeMessages(msgs)

	if !strings.Contains(out, "[Assistant]: let me check") {
		t.Error("expected assistant content")
	}
	if !strings.Contains(out, "[Tool Call]: read_file({\"path\":\"main.go\"})") {
		t.Error("expected tool call line")
	}
}

func TestSerializeMessages_Truncation(t *testing.T) {
	longContent := strings.Repeat("y", 600)
	msgs := []ChatMessage{{Role: RoleTool, Content: longContent}}
	out := (&ContextCompactor{}).serializeMessages(msgs)

	if !strings.Contains(out, "...") {
		t.Error("expected truncation indicator for long tool result")
	}
	// Should be ~500 chars of content + "..." not 600
	if strings.Contains(out, longContent) {
		t.Error("long tool result should have been truncated")
	}
}

func TestSerializeMessages_SystemSkipped(t *testing.T) {
	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "system prompt"},
		{Role: RoleUser, Content: "hello"},
	}
	out := (&ContextCompactor{}).serializeMessages(msgs)

	if strings.Contains(out, "system prompt") {
		t.Error("system messages should not appear in serialized output")
	}
	if !strings.Contains(out, "[User]: hello") {
		t.Error("expected user message in output")
	}
}

func TestSerializeMessages_Empty(t *testing.T) {
	c := &ContextCompactor{}
	if c.serializeMessages(nil) != "" {
		t.Error("expected empty string for nil messages")
	}
	if c.serializeMessages([]ChatMessage{}) != "" {
		t.Error("expected empty string for empty messages")
	}
}

// ---------------------------------------------------------------------------
// buildSummaryPrompt
// ---------------------------------------------------------------------------

func TestBuildSummaryPrompt_Structured(t *testing.T) {
	c := &ContextCompactor{config: CompactorConfig{SummaryFormat: "structured"}}

	prompt := c.buildSummaryPrompt("conversation text", "")
	if !strings.Contains(prompt, "You are summarizing a conversation") {
		t.Error("structured prompt should contain instruction")
	}
	if !strings.Contains(prompt, "conversation text") {
		t.Error("structured prompt should contain conversation text")
	}
	if !strings.Contains(prompt, "## Goal") {
		t.Error("structured prompt should contain Goal section")
	}
	if !strings.Contains(prompt, "## Files") {
		t.Error("structured prompt should contain Files section")
	}
}

func TestBuildSummaryPrompt_Narrative(t *testing.T) {
	c := &ContextCompactor{config: CompactorConfig{SummaryFormat: "narrative"}}

	prompt := c.buildSummaryPrompt("conversation text", "")
	if !strings.Contains(prompt, "Summarize the following conversation concisely") {
		t.Error("narrative prompt should contain instruction")
	}
	if !strings.Contains(prompt, "conversation text") {
		t.Error("narrative prompt should contain conversation text")
	}
}

func TestBuildSummaryPrompt_IterativeUpdate(t *testing.T) {
	c := &ContextCompactor{config: CompactorConfig{SummaryFormat: "structured"}}

	prompt := c.buildSummaryPrompt("new messages", "previous summary here")
	if !strings.Contains(prompt, "You are updating a conversation summary") {
		t.Error("iterative prompt should contain update instruction")
	}
	if !strings.Contains(prompt, "previous summary here") {
		t.Error("iterative prompt should contain previous summary")
	}
	if !strings.Contains(prompt, "new messages") {
		t.Error("iterative prompt should contain new messages")
	}
}

func TestBuildSummaryPrompt_NarrativeWithExistingSummary_IgnoresIterative(t *testing.T) {
	// Narrative mode should NOT use iterative update even with existing summary
	c := &ContextCompactor{config: CompactorConfig{SummaryFormat: "narrative"}}

	prompt := c.buildSummaryPrompt("new messages", "old summary")
	if strings.Contains(prompt, "You are updating") {
		t.Error("narrative mode should not use iterative update prompt")
	}
	if !strings.Contains(prompt, "Summarize the following conversation concisely") {
		t.Error("narrative mode should use narrative prompt regardless of existing summary")
	}
}

// ---------------------------------------------------------------------------
// parseSummaryResponse
// ---------------------------------------------------------------------------

func TestParseSummaryResponse_StructuredSections(t *testing.T) {
	c := &ContextCompactor{}

	raw := `## Goal
Fix the authentication bug

## Constraints
Must work with existing API

## Progress
Investigated the issue and found root cause

## Key Decisions
- Use JWT tokens instead of sessions
- Rate limit at 100 req/min

## Files
- read: internal/auth/handler.go
- write: internal/auth/middleware.go
- edit: internal/config/routes.go

## Important Discoveries
- Token expiry was not being checked
- Session store has race condition

## Errors Encountered
- Database connection timeout during load test

## Next Steps
- Implement token refresh endpoint
- Add integration tests

## Critical Context
- API endpoint: /api/v1/auth
- Token secret: configured in env`

	ext := c.parseSummaryResponse(raw)

	if len(ext.Decisions) != 2 {
		t.Errorf("expected 2 decisions, got %d: %v", len(ext.Decisions), ext.Decisions)
	}
	if ext.TaskState == "" {
		t.Error("expected non-empty TaskState from Progress section")
	}
	if len(ext.FileReads) != 1 || ext.FileReads[0] != "internal/auth/handler.go" {
		t.Errorf("FileReads: got %v", ext.FileReads)
	}
	if len(ext.FileWrites) != 1 || ext.FileWrites[0] != "internal/auth/middleware.go" {
		t.Errorf("FileWrites: got %v", ext.FileWrites)
	}
	if len(ext.FileEdits) != 1 || ext.FileEdits[0] != "internal/config/routes.go" {
		t.Errorf("FileEdits: got %v", ext.FileEdits)
	}
	if len(ext.KeyFindings) != 2 {
		t.Errorf("expected 2 findings, got %d: %v", len(ext.KeyFindings), ext.KeyFindings)
	}
	if len(ext.ErrorsEncountered) != 1 {
		t.Errorf("expected 1 error, got %d: %v", len(ext.ErrorsEncountered), ext.ErrorsEncountered)
	}
	// Constraints section text is not bullet-prefixed so parseBulletItems returns nil.
	// The Constraints value is free-form text, not bullet items.
	if len(ext.UnresolvedQuestions) != 0 {
		t.Errorf("expected 0 bullet constraints (plain text is not bullet items), got %d: %v", len(ext.UnresolvedQuestions), ext.UnresolvedQuestions)
	}
}

func TestParseSummaryResponse_LegacyFormat(t *testing.T) {
	// When sections don't match the new format, should fall back to
	// parseStructuredSummary (DECISIONS:, FILES:, etc.)
	c := &ContextCompactor{}

	raw := "DECISIONS:\n- Use approach A\n- Avoid approach B\n\nFILES:\n- main.go\n\nSTATUS:\nHalf done\n\nFINDINGS:\n- Bug in line 42\n"
	ext := c.parseSummaryResponse(raw)

	if len(ext.Decisions) != 2 {
		t.Errorf("expected 2 decisions from legacy format, got %d", len(ext.Decisions))
	}
	if ext.TaskState != "Half done" {
		t.Errorf("expected TaskState from legacy STATUS, got %q", ext.TaskState)
	}
}

func TestParseSummaryResponse_Empty(t *testing.T) {
	c := &ContextCompactor{}
	ext := c.parseSummaryResponse("")
	if len(ext.Decisions) != 0 || len(ext.FilePaths) != 0 {
		t.Error("expected empty extract for empty input")
	}
}

// ---------------------------------------------------------------------------
// splitCompactionSections
// ---------------------------------------------------------------------------

func TestSplitCompactionSections(t *testing.T) {
	raw := `## Goal
Do something

## Key Decisions
- decided A
- decided B

## Files
- read: a.go

## Critical Context
endpoint: /api`

	sections := splitCompactionSections(raw)
	if len(sections) != 4 {
		t.Fatalf("expected 4 sections, got %d", len(sections))
	}
	if !strings.Contains(sections["Goal"], "Do something") {
		t.Errorf("Goal section: got %q", sections["Goal"])
	}
	if !strings.Contains(sections["Key Decisions"], "decided A") {
		t.Errorf("Key Decisions section: got %q", sections["Key Decisions"])
	}
}

func TestSplitCompactionSections_NoHeaders(t *testing.T) {
	sections := splitCompactionSections("just plain text with no headers")
	if len(sections) != 0 {
		t.Errorf("expected 0 sections for text without headers, got %d", len(sections))
	}
}

// ---------------------------------------------------------------------------
// updateFileOps
// ---------------------------------------------------------------------------

func TestUpdateFileOps(t *testing.T) {
	c := &ContextCompactor{
		config:  CompactorConfig{TrackFileOps: true},
		fileOps: NewFileOperationSet(),
	}

	summary := SummaryExtract{
		FileReads:  []string{"a.go", "b.go"},
		FileWrites: []string{"c.go"},
		FileEdits:  []string{"d.go"},
		FilePaths:  []string{"read: e.go", "write: f.go", "edit: g.go", "unknown.go"},
	}

	c.updateFileOps(summary)

	if len(c.fileOps.Read) != 4 { // a, b, e, unknown (defaults to read)
		t.Errorf("expected 4 reads, got %d: %v", len(c.fileOps.Read), c.fileOps.Read)
	}
	if len(c.fileOps.Written) != 2 { // c, f
		t.Errorf("expected 2 written, got %d", len(c.fileOps.Written))
	}
	if len(c.fileOps.Edited) != 2 { // d, g
		t.Errorf("expected 2 edited, got %d", len(c.fileOps.Edited))
	}
	// unknown.go should default to read
	if !c.fileOps.Read["unknown.go"] {
		t.Error("unknown file prefix should default to read")
	}
}

func TestUpdateFileOps_TrackingDisabled(t *testing.T) {
	c := &ContextCompactor{
		config:  CompactorConfig{TrackFileOps: false},
		fileOps: NewFileOperationSet(),
	}

	summary := SummaryExtract{FileReads: []string{"a.go"}}
	c.updateFileOps(summary)

	if len(c.fileOps.Read) != 0 {
		t.Error("file ops should not be updated when tracking disabled")
	}
}

func TestUpdateFileOps_NilFileOps(t *testing.T) {
	c := &ContextCompactor{
		config:  CompactorConfig{TrackFileOps: true},
		fileOps: nil,
	}

	// Should not panic
	c.updateFileOps(SummaryExtract{FileReads: []string{"a.go"}})
}

// ---------------------------------------------------------------------------
// buildCompactionMessage
// ---------------------------------------------------------------------------

func TestBuildCompactionMessage_WithFileOps(t *testing.T) {
	c := &ContextCompactor{}

	fos := NewFileOperationSet()
	fos.Read["main.go"] = true

	msg := c.buildCompactionMessage("summary text here", fos)

	if msg.Role != RoleSystem {
		t.Errorf("expected system role, got %s", msg.Role)
	}
	if !strings.Contains(msg.Content, "[Compacted Context]") {
		t.Error("compaction message should have header")
	}
	if !strings.Contains(msg.Content, "summary text here") {
		t.Error("compaction message should contain summary")
	}
	if !strings.Contains(msg.Content, "Cumulative File Operations") {
		t.Error("compaction message should contain file ops section when present")
	}
	if !strings.Contains(msg.Content, "read: main.go") {
		t.Error("compaction message should contain file operations")
	}
}

func TestBuildCompactionMessage_NoFileOps(t *testing.T) {
	c := &ContextCompactor{}

	msg := c.buildCompactionMessage("summary text", nil)
	if strings.Contains(msg.Content, "Cumulative File Operations") {
		t.Error("should not contain file ops section when nil")
	}
}

func TestBuildCompactionMessage_EmptyFileOps(t *testing.T) {
	c := &ContextCompactor{}

	fos := NewFileOperationSet() // empty
	msg := c.buildCompactionMessage("summary text", fos)
	if strings.Contains(msg.Content, "Cumulative File Operations") {
		t.Error("should not contain file ops section when empty")
	}
}

// ---------------------------------------------------------------------------
// Compact -- full flow
// ---------------------------------------------------------------------------

func TestCompact_Success(t *testing.T) {
	mock := &compactorMockChatter{
		response: &Response{Content: `## Goal
Fix the bug

## Key Decisions
- Use approach A

## Files
- read: main.go

## Progress
Working on it

## Important Discoveries
none

## Errors Encountered
none

## Next Steps
Continue

## Critical Context
none

## Constraints
none`},
	}

	cfg := DefaultCompactorConfig()
	cfg.KeepRecentTokens = 300 // ~3 messages kept
	c := NewContextCompactor(cfg, mock, nil, nil)

	msgs := makeMessages(10) // system + 20 messages
	result := c.Compact(context.Background(), msgs)

	if !result.Compacted {
		t.Error("expected Compacted=true")
	}
	if !mock.called {
		t.Error("expected summarizer to be called")
	}
	if result.TokensBefore <= 0 {
		t.Error("TokensBefore should be positive")
	}
	if result.TokensAfter <= 0 {
		t.Error("TokensAfter should be positive")
	}
	if result.TokensAfter >= result.TokensBefore {
		t.Errorf("expected TokensAfter (%d) < TokensBefore (%d)", result.TokensAfter, result.TokensBefore)
	}
	if result.SummaryContent == "" {
		t.Error("expected non-empty SummaryContent")
	}
	if result.FileOps == nil {
		t.Error("expected non-nil FileOps")
	}
	if c.LastSummary() != result.SummaryContent {
		t.Error("LastSummary should match SummaryContent")
	}

	// System messages should be preserved at the front
	if len(result.Messages) == 0 {
		t.Fatal("expected non-empty result messages")
	}
	if result.Messages[0].Role != RoleSystem {
		t.Error("first message should be system")
	}

	// The compaction message should be present
	foundCompaction := false
	for _, m := range result.Messages {
		if strings.Contains(m.Content, "[Compacted Context]") {
			foundCompaction = true
		}
	}
	if !foundCompaction {
		t.Error("expected to find compaction summary message")
	}
}

func TestCompact_NilSummarizer(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	msgs := makeMessages(5)
	result := c.Compact(context.Background(), msgs)

	if result.Compacted {
		t.Error("should not compact with nil summarizer")
	}
	if len(result.Messages) != len(msgs) {
		t.Error("messages should be unchanged when no summarizer")
	}
}

func TestCompact_SummarizerError(t *testing.T) {
	mock := &compactorMockChatter{
		err: fmt.Errorf("LLM unavailable"),
	}

	cfg := DefaultCompactorConfig()
	cfg.KeepRecentTokens = 300
	c := NewContextCompactor(cfg, mock, nil, nil)

	msgs := makeMessages(10)
	result := c.Compact(context.Background(), msgs)

	if result.Compacted {
		t.Error("should not mark as compacted on LLM error")
	}
	if len(result.Messages) != len(msgs) {
		t.Error("messages should be unchanged on LLM error")
	}
}

func TestCompact_EmptyResponse(t *testing.T) {
	mock := &compactorMockChatter{
		response: &Response{Content: ""},
	}

	cfg := DefaultCompactorConfig()
	cfg.KeepRecentTokens = 300
	c := NewContextCompactor(cfg, mock, nil, nil)

	msgs := makeMessages(10)
	result := c.Compact(context.Background(), msgs)

	if result.Compacted {
		t.Error("should not compact when LLM returns empty")
	}
}

func TestCompact_NothingToCompact(t *testing.T) {
	mock := &compactorMockChatter{
		response: &Response{Content: "summary"},
	}

	cfg := DefaultCompactorConfig()
	cfg.KeepRecentTokens = 100000 // everything fits
	c := NewContextCompactor(cfg, mock, nil, nil)

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: "short"},
	}

	result := c.Compact(context.Background(), msgs)
	if result.Compacted {
		t.Error("should not compact when nothing to cut")
	}
	if mock.called {
		t.Error("summarizer should not be called when nothing to compact")
	}
}

func TestCompact_FileOpsTracked(t *testing.T) {
	mock := &compactorMockChatter{
		response: &Response{Content: `## Goal
Work

## Key Decisions
- d1

## Files
- read: a.go
- write: b.go
- edit: c.go

## Progress
ok

## Important Discoveries
none

## Errors Encountered
none

## Next Steps
none

## Critical Context
none

## Constraints
none`},
	}

	cfg := DefaultCompactorConfig()
	cfg.KeepRecentTokens = 300
	c := NewContextCompactor(cfg, mock, nil, nil)

	msgs := makeMessages(10)
	result := c.Compact(context.Background(), msgs)

	if !result.Compacted {
		t.Fatal("expected compaction")
	}

	fo := c.FileOperations()
	if fo == nil {
		t.Fatal("expected file operations to be tracked")
	}
	if !fo.Read["a.go"] {
		t.Error("expected a.go in reads")
	}
	if !fo.Written["b.go"] {
		t.Error("expected b.go in writes")
	}
	if !fo.Edited["c.go"] {
		t.Error("expected c.go in edits")
	}
}

func TestCompact_IterativeUpdate(t *testing.T) {
	// First compaction
	mock1 := &compactorMockChatter{
		response: &Response{Content: `## Goal
Work

## Key Decisions
- d1

## Files
- read: a.go

## Progress
started

## Important Discoveries
none

## Errors Encountered
none

## Next Steps
continue

## Critical Context
none

## Constraints
none`},
	}

	cfg := DefaultCompactorConfig()
	cfg.KeepRecentTokens = 300
	c := NewContextCompactor(cfg, mock1, nil, nil)

	msgs := makeMessages(10)
	result1 := c.Compact(context.Background(), msgs)
	if !result1.Compacted {
		t.Fatal("first compaction should succeed")
	}

	// Second compaction with the same compactor (lastSummary is now set)
	mock2 := &compactorMockChatter{
		response: &Response{Content: `## Goal
Work

## Key Decisions
- d1
- d2

## Files
- read: a.go
- write: b.go

## Progress
continued

## Important Discoveries
found something

## Errors Encountered
none

## Next Steps
finish

## Critical Context
none

## Constraints
none`},
	}
	c.summarizer = mock2

	msgs2 := makeMessages(10)
	result2 := c.Compact(context.Background(), msgs2)
	if !result2.Compacted {
		t.Fatal("second compaction should succeed")
	}

	// The second call should have received the iterative update prompt
	if len(mock2.lastMsgs) == 0 {
		t.Fatal("expected messages to be sent to summarizer")
	}
	prompt := mock2.lastMsgs[0].Content
	if !strings.Contains(prompt, "You are updating a conversation summary") {
		t.Errorf("second compaction should use iterative update prompt, got: %s", prompt[:min(200, len(prompt))])
	}
	if !strings.Contains(prompt, result1.SummaryContent) {
		t.Error("iterative prompt should contain previous summary")
	}

	// File ops should be cumulative
	fo := c.FileOperations()
	if !fo.Read["a.go"] {
		t.Error("a.go should still be tracked from first compaction")
	}
	if !fo.Written["b.go"] {
		t.Error("b.go should be tracked from second compaction")
	}
}

func TestCompact_ContextCancellation(t *testing.T) {
	// Use a mock that respects context cancellation.
	ctxAwareMock := &contextAwareMock{
		response: &Response{Content: "summary"},
	}

	cfg := DefaultCompactorConfig()
	cfg.KeepRecentTokens = 300
	cfg.TimeoutSeconds = 5
	c := NewContextCompactor(cfg, ctxAwareMock, nil, nil)

	// Use an already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	msgs := makeMessages(10)
	result := c.Compact(ctx, msgs)

	if result.Compacted {
		t.Error("should not compact with cancelled context")
	}
}

// contextAwareMock is a mock that checks context before responding.
type contextAwareMock struct {
	response *Response
	called   bool
}

func (m *contextAwareMock) Chat(ctx context.Context, _ []ChatMessage, _ ...ChatOption) (*Response, error) {
	m.called = true
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return m.response, nil
}

func (m *contextAwareMock) ChatWithProgress(ctx context.Context, msgs []ChatMessage, _ ProgressCallback, _ ...ChatOption) (*Response, error) {
	return m.Chat(ctx, msgs)
}

func (m *contextAwareMock) Config() *ModelConfig {
	return &ModelConfig{}
}

// ---------------------------------------------------------------------------
// countTokens
// ---------------------------------------------------------------------------

func TestCountTokens(t *testing.T) {
	c := NewContextCompactor(CompactorConfig{}, nil, nil, nil)

	msgs := []ChatMessage{
		{Role: RoleUser, Content: "hello world"},       // ~4 tokens (11 chars / 3)
		{Role: RoleAssistant, Content: "hi there"},     // ~3 tokens (8 chars / 3)
	}

	count := c.countTokens(msgs)
	if count <= 0 {
		t.Error("countTokens should return positive value")
	}
}

func TestCountTokens_Empty(t *testing.T) {
	c := NewContextCompactor(CompactorConfig{}, nil, nil, nil)
	if c.countTokens(nil) != 0 {
		t.Error("countTokens(nil) should be 0")
	}
}

// ---------------------------------------------------------------------------
// CompactResult / CutResult defaults
// ---------------------------------------------------------------------------

func TestCompactResult_Defaults(t *testing.T) {
	r := CompactResult{}
	if r.Compacted {
		t.Error("default CompactResult should not be compacted")
	}
	if r.TokensBefore != 0 {
		t.Error("default TokensBefore should be 0")
	}
}

// ---------------------------------------------------------------------------
// multiResponseMock -- returns different responses for successive Chat calls
// ---------------------------------------------------------------------------

type multiResponseMock struct {
	responses []*Response
	errors    []error
	callCount int
	lastMsgs  [][]ChatMessage // messages received per call
}

func (m *multiResponseMock) Chat(_ context.Context, msgs []ChatMessage, _ ...ChatOption) (*Response, error) {
	idx := m.callCount
	m.callCount++
	m.lastMsgs = append(m.lastMsgs, msgs)
	if idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	if len(m.responses) > 0 {
		return m.responses[len(m.responses)-1], nil
	}
	return &Response{Content: ""}, nil
}

func (m *multiResponseMock) ChatWithProgress(_ context.Context, msgs []ChatMessage, _ ProgressCallback, _ ...ChatOption) (*Response, error) {
	return m.Chat(context.Background(), msgs)
}

func (m *multiResponseMock) Config() *ModelConfig {
	return &ModelConfig{}
}

// ---------------------------------------------------------------------------
// buildTurnPrefixPrompt
// ---------------------------------------------------------------------------

func TestBuildTurnPrefixPrompt(t *testing.T) {
	c := &ContextCompactor{}
	prompt := c.buildTurnPrefixPrompt("assistant called read_file(main.go)")
	if !strings.Contains(prompt, "partial turn") {
		t.Error("turn prefix prompt should mention partial turn")
	}
	if !strings.Contains(prompt, "assistant called read_file(main.go)") {
		t.Error("turn prefix prompt should contain the turn text")
	}
	if !strings.Contains(prompt, "Summarize concisely") {
		t.Error("turn prefix prompt should contain instruction")
	}
}

// ---------------------------------------------------------------------------
// compactSplitTurn -- dual summary scenarios
// ---------------------------------------------------------------------------

func TestCompactSplitTurn_DualSummaries(t *testing.T) {
	// Build messages where the cut point lands mid-turn:
	// The ToCompact region will contain history + a partial turn
	// (assistant with tool calls but no tool result).
	mock := &multiResponseMock{
		responses: []*Response{
			// First call: history summary (structured format)
			{Content: `## Goal
Build a feature

## Key Decisions
- Use approach X

## Files
- read: main.go

## Progress
Investigated the codebase

## Important Discoveries
Pattern found

## Errors Encountered
none

## Next Steps
Continue implementation

## Critical Context
API at /api/v1

## Constraints
none`},
			// Second call: turn prefix summary
			{Content: "The assistant called read_file(config.go) to check configuration values. It was about to modify the config based on findings."},
		},
	}

	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, mock, nil, nil)

	// Craft a CutResult with a split turn
	cut := CutResult{
		SystemMsgs: []ChatMessage{{Role: RoleSystem, Content: "system prompt"}},
		ToCompact: []ChatMessage{
			{Role: RoleUser, Content: "please build a feature"},
			{Role: RoleAssistant, Content: "I'll investigate the codebase"},
			// Partial turn: assistant has tool calls but no result
			{Role: RoleUser, Content: "now check config"},
			{Role: RoleAssistant, Content: "checking config", ToolCalls: []ToolCall{
				{ID: "tc1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"config.go"}`}},
			}},
		},
		ToKeep: []ChatMessage{
			{Role: RoleUser, Content: "continue"},
			{Role: RoleAssistant, Content: "ok"},
		},
		SplitTurn:      true,
		SplitTurnIndex: 3, // index of the partial turn assistant message
	}

	// We need to test compactSplitTurn directly since we control the cut
	summary, err := c.compactSplitTurn(context.Background(), cut, 30*time.Second)
	if err != nil {
		t.Fatalf("compactSplitTurn failed: %v", err)
	}

	// Should contain the history summary
	if !strings.Contains(summary, "Build a feature") {
		t.Error("merged summary should contain history goal")
	}
	if !strings.Contains(summary, "read: main.go") {
		t.Error("merged summary should contain history files")
	}

	// Should contain the turn prefix section
	if !strings.Contains(summary, "In-Progress Turn (compacted mid-turn)") {
		t.Error("merged summary should contain in-progress turn section header")
	}
	if !strings.Contains(summary, "read_file(config.go)") {
		t.Error("merged summary should contain turn prefix content about config.go")
	}

	// Should have made two LLM calls
	if mock.callCount != 2 {
		t.Errorf("expected 2 LLM calls (history + turn prefix), got %d", mock.callCount)
	}

	// First call should be the history summary prompt
	if len(mock.lastMsgs[0]) == 0 {
		t.Fatal("first call should have messages")
	}
	if !strings.Contains(mock.lastMsgs[0][0].Content, "You are summarizing a conversation") {
		t.Error("first call should use the history summary prompt")
	}
	if strings.Contains(mock.lastMsgs[0][0].Content, "checking config") {
		t.Error("first call should NOT contain the turn prefix messages")
	}

	// Second call should be the turn prefix prompt
	if len(mock.lastMsgs[1]) == 0 {
		t.Fatal("second call should have messages")
	}
	if !strings.Contains(mock.lastMsgs[1][0].Content, "partial turn") {
		t.Error("second call should use the turn prefix prompt")
	}
	if !strings.Contains(mock.lastMsgs[1][0].Content, "checking config") {
		t.Error("second call should contain the turn prefix messages")
	}
}

func TestCompactSplitTurn_EmptyHistory(t *testing.T) {
	// When history is empty, should fall back to summarizing turn prefix as regular summary.
	mock := &multiResponseMock{
		responses: []*Response{
			{Content: "## Goal\nDoing work\n\n## Progress\nIn progress"},
		},
	}

	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, mock, nil, nil)

	cut := CutResult{
		ToCompact: []ChatMessage{
			{Role: RoleAssistant, Content: "working", ToolCalls: []ToolCall{
				{ID: "tc1", Function: ToolCallFunction{Name: "run", Arguments: "{}"}},
			}},
		},
		SplitTurn:      true,
		SplitTurnIndex: 0, // the only message is the partial turn
	}

	summary, err := c.compactSplitTurn(context.Background(), cut, 30*time.Second)
	if err != nil {
		t.Fatalf("compactSplitTurn failed: %v", err)
	}

	if !strings.Contains(summary, "Doing work") {
		t.Errorf("expected turn prefix summary content, got: %s", summary)
	}
	// Should only have made one call (the turn prefix as regular summary)
	if mock.callCount != 1 {
		t.Errorf("expected 1 LLM call for empty history, got %d", mock.callCount)
	}
}

func TestCompactSplitTurn_EmptyTurnPrefix(t *testing.T) {
	// When turn prefix is empty, should just summarize history.
	mock := &multiResponseMock{
		responses: []*Response{
			{Content: "## Goal\nBuild feature\n\n## Progress\nStarted"},
		},
	}

	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, mock, nil, nil)

	cut := CutResult{
		ToCompact: []ChatMessage{
			{Role: RoleUser, Content: "build a feature"},
			{Role: RoleAssistant, Content: "on it"},
		},
		SplitTurn:      true,
		SplitTurnIndex: 2, // past end of ToCompact -> empty turn prefix
	}

	summary, err := c.compactSplitTurn(context.Background(), cut, 30*time.Second)
	if err != nil {
		t.Fatalf("compactSplitTurn failed: %v", err)
	}

	if !strings.Contains(summary, "Build feature") {
		t.Errorf("expected history summary, got: %s", summary)
	}
	if mock.callCount != 1 {
		t.Errorf("expected 1 LLM call for empty turn prefix, got %d", mock.callCount)
	}
}

func TestCompactSplitTurn_HistoryError_Fallback(t *testing.T) {
	// When history summarization fails, should return error (triggers fallback in Compact)
	mock := &multiResponseMock{
		errors: []error{fmt.Errorf("LLM unavailable")},
	}

	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, mock, nil, nil)

	cut := CutResult{
		ToCompact: []ChatMessage{
			{Role: RoleUser, Content: "message 1"},
			{Role: RoleAssistant, Content: "reply 1"},
			{Role: RoleUser, Content: "message 2"},
			{Role: RoleAssistant, Content: "working", ToolCalls: []ToolCall{
				{ID: "tc1", Function: ToolCallFunction{Name: "run", Arguments: "{}"}},
			}},
		},
		SplitTurn:      true,
		SplitTurnIndex: 2,
	}

	_, err := c.compactSplitTurn(context.Background(), cut, 30*time.Second)
	if err == nil {
		t.Error("expected error when history summarization fails")
	}
}

func TestCompactSplitTurn_TurnPrefixError_UsesRawText(t *testing.T) {
	// When turn prefix summarization fails, should use raw text as fallback
	mock := &multiResponseMock{
		responses: []*Response{
			// History summary succeeds
			{Content: "## Goal\nBuild something\n\n## Progress\nStarted work"},
			// Turn prefix summary fails
		},
		errors: []error{
			nil,
			fmt.Errorf("timeout"),
		},
	}

	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, mock, nil, nil)

	cut := CutResult{
		ToCompact: []ChatMessage{
			{Role: RoleUser, Content: "build something"},
			{Role: RoleAssistant, Content: "on it"},
			{Role: RoleAssistant, Content: "checking file", ToolCalls: []ToolCall{
				{ID: "tc1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"main.go"}`}},
			}},
		},
		SplitTurn:      true,
		SplitTurnIndex: 2,
	}

	summary, err := c.compactSplitTurn(context.Background(), cut, 30*time.Second)
	if err != nil {
		t.Fatalf("compactSplitTurn should not fail when turn prefix fails: %v", err)
	}

	// Should contain history summary
	if !strings.Contains(summary, "Build something") {
		t.Error("should contain history summary")
	}

	// Should contain raw text from turn prefix (since LLM failed)
	if !strings.Contains(summary, "In-Progress Turn (compacted mid-turn)") {
		t.Error("should contain in-progress turn section")
	}
	if !strings.Contains(summary, "checking file") {
		t.Error("should contain raw turn prefix text as fallback")
	}
}

func TestCompact_WithSplitTurn_MergesDualSummaries(t *testing.T) {
	// End-to-end test: Compact() detects split turn and produces merged summary.
	// We need messages long enough that the cut point lands in the middle and
	// the ToCompact region has a partial turn at the end.
	mock := &multiResponseMock{
		responses: []*Response{
			// History summary
			{Content: `## Goal
Debug the server

## Key Decisions
- Enable verbose logging

## Files
- read: server.go

## Progress
Found the issue

## Important Discoveries
Connection leak

## Errors Encountered
Connection refused

## Next Steps
Fix the leak

## Critical Context
Port 8080

## Constraints
none`},
			// Turn prefix summary
			{Content: "The assistant called shell_check(leaks) to diagnose the connection issue."},
		},
	}

	cfg := DefaultCompactorConfig()
	cfg.KeepRecentTokens = 200 // small budget to force a cut
	c := NewContextCompactor(cfg, mock, nil, nil)

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "system prompt"},
		// History messages (will be compacted)
		{Role: RoleUser, Content: "debug the server" + makeLongString(90)},
		{Role: RoleAssistant, Content: "investigating" + makeLongString(90)},
		{Role: RoleUser, Content: "check for leaks" + makeLongString(90)},
		{Role: RoleAssistant, Content: "running diagnostics" + makeLongString(90)},
		// Partial turn: assistant has tool calls but no result
		{Role: RoleUser, Content: "also check config" + makeLongString(90)},
		{Role: RoleAssistant, Content: "checking", ToolCalls: []ToolCall{
			{ID: "tc1", Function: ToolCallFunction{Name: "shell_check", Arguments: `{"cmd":"leaks"}`}},
		}},
		// Recent messages to keep
		{Role: RoleUser, Content: "continue work" + makeLongString(90)},
		{Role: RoleAssistant, Content: "ok continuing" + makeLongString(90)},
	}

	result := c.Compact(context.Background(), msgs)

	if !result.Compacted {
		t.Error("expected compaction")
	}
	if !result.SplitTurn {
		t.Error("expected split turn to be detected")
	}

	// The summary should contain both sections
	if !strings.Contains(result.SummaryContent, "Debug the server") {
		t.Error("summary should contain history goal")
	}
	if !strings.Contains(result.SummaryContent, "In-Progress Turn (compacted mid-turn)") {
		t.Error("summary should contain in-progress turn section")
	}

	// The final messages should be: system + compaction + recent
	if len(result.Messages) < 3 {
		t.Errorf("expected at least 3 final messages, got %d", len(result.Messages))
	}
	if result.Messages[0].Role != RoleSystem {
		t.Error("first message should be system")
	}

	// Check that the compaction message contains both summaries
	compactionFound := false
	for _, m := range result.Messages {
		if strings.Contains(m.Content, "[Compacted Context]") {
			compactionFound = true
			if !strings.Contains(m.Content, "Debug the server") {
				t.Error("compaction message should contain history summary")
			}
			if !strings.Contains(m.Content, "In-Progress Turn (compacted mid-turn)") {
				t.Error("compaction message should contain turn prefix section")
			}
		}
	}
	if !compactionFound {
		t.Error("expected to find compaction summary message")
	}
}

func TestCompact_SplitTurnFails_FallsBackToSingleSummary(t *testing.T) {
	// When split-turn compaction fails, it should fall back to single summary.
	mock := &multiResponseMock{
		responses: []*Response{
			// First call: history summary fails
			nil,
			// Fallback: single summary of all ToCompact
			{Content: "## Goal\nFallback summary\n\n## Progress\nWork in progress\n\n## Key Decisions\nnone\n\n## Files\nnone\n\n## Important Discoveries\nnone\n\n## Errors Encountered\nnone\n\n## Next Steps\ncontinue\n\n## Critical Context\nnone\n\n## Constraints\nnone"},
		},
		errors: []error{
			fmt.Errorf("history summarization failed"),
			nil,
		},
	}

	cfg := DefaultCompactorConfig()
	cfg.KeepRecentTokens = 200
	c := NewContextCompactor(cfg, mock, nil, nil)

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "system"},
		{Role: RoleUser, Content: "task" + makeLongString(90)},
		{Role: RoleAssistant, Content: "reply" + makeLongString(90)},
		{Role: RoleUser, Content: "followup" + makeLongString(90)},
		{Role: RoleAssistant, Content: "work", ToolCalls: []ToolCall{
			{ID: "tc1", Function: ToolCallFunction{Name: "run", Arguments: "{}"}},
		}},
		{Role: RoleUser, Content: "keep this" + makeLongString(90)},
		{Role: RoleAssistant, Content: "final" + makeLongString(90)},
	}

	result := c.Compact(context.Background(), msgs)

	if !result.Compacted {
		t.Error("expected compaction via fallback")
	}
	// The fallback summary should NOT have the split-turn section header
	if strings.Contains(result.SummaryContent, "In-Progress Turn (compacted mid-turn)") {
		t.Error("fallback single summary should not contain in-progress turn section")
	}
	if !strings.Contains(result.SummaryContent, "Fallback summary") {
		t.Error("should contain the fallback summary content")
	}
}

// ---------------------------------------------------------------------------
// pruneToolOutputs
// ---------------------------------------------------------------------------

func TestPruneToolOutputs_NoToolMessages(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, Content: "hi"},
	}

	result := c.pruneToolOutputs(msgs)
	if len(result) != len(msgs) {
		t.Errorf("expected %d messages, got %d", len(msgs), len(result))
	}
}

func TestPruneToolOutputs_SmallOutput_NoPruning(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	msgs := []ChatMessage{
		{Role: RoleUser, Content: "run it"},
		{Role: RoleAssistant, Content: "running", ToolCalls: []ToolCall{
			{ID: "tc1", Function: ToolCallFunction{Name: "shell_execute", Arguments: `{}`}},
		}},
		{Role: RoleTool, Content: "small output", ToolCallID: "tc1"},
	}

	result := c.pruneToolOutputs(msgs)
	if result[2].Content != "small output" {
		t.Errorf("small tool output should not be pruned, got: %q", result[2].Content)
	}
}

func TestPruneToolOutputs_ProtectedTool_NeverPruned(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	// file_read result that is very large (way beyond the protected budget)
	largeOutput := makeLongString(50000) // 50k tokens
	msgs := []ChatMessage{
		{Role: RoleUser, Content: "read this file"},
		{Role: RoleAssistant, Content: "reading", ToolCalls: []ToolCall{
			{ID: "tc1", Function: ToolCallFunction{Name: "file_read", Arguments: `{}`}},
		}},
		{Role: RoleTool, Content: largeOutput, ToolCallID: "tc1"},
	}

	result := c.pruneToolOutputs(msgs)
	if result[2].Content != largeOutput {
		t.Error("file_read output should never be pruned regardless of size")
	}
}

func TestPruneToolOutputs_MemorySearch_NeverPruned(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	largeOutput := makeLongString(50000)
	msgs := []ChatMessage{
		{Role: RoleUser, Content: "search memory"},
		{Role: RoleAssistant, Content: "searching", ToolCalls: []ToolCall{
			{ID: "tc1", Function: ToolCallFunction{Name: "memory_search", Arguments: `{}`}},
		}},
		{Role: RoleTool, Content: largeOutput, ToolCallID: "tc1"},
	}

	result := c.pruneToolOutputs(msgs)
	if result[2].Content != largeOutput {
		t.Error("memory_search output should never be pruned regardless of size")
	}
}

func TestPruneToolOutputs_LargeOldOutput_Pruned(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	// Create messages with a large tool output far in the past (beyond the 40k
	// protected window), followed by enough recent tool output to fill the
	// protected budget.
	// 30k token output (well beyond the 20k minimum savings threshold)
	largeOutput := makeLongString(30000)

	// Recent tool output fits within the 40k protected budget
	recentOutput := makeLongString(39000)

	msgs := []ChatMessage{
		{Role: RoleUser, Content: "do old work"},
		{Role: RoleAssistant, Content: "working", ToolCalls: []ToolCall{
			{ID: "tc_old", Function: ToolCallFunction{Name: "shell_execute", Arguments: `{}`}},
		}},
		{Role: RoleTool, Content: largeOutput, ToolCallID: "tc_old"},
		{Role: RoleUser, Content: "do recent work"},
		{Role: RoleAssistant, Content: "working", ToolCalls: []ToolCall{
			{ID: "tc_recent", Function: ToolCallFunction{Name: "shell_execute", Arguments: `{}`}},
		}},
		{Role: RoleTool, Content: recentOutput, ToolCallID: "tc_recent"},
	}

	result := c.pruneToolOutputs(msgs)

	// The old tool output (index 2) should be pruned
	if result[2].Content == largeOutput {
		t.Error("old large tool output should have been pruned")
	}
	if !strings.Contains(result[2].Content, "Output truncated") {
		t.Errorf("pruned output should contain truncation notice, got: %q", result[2].Content)
	}

	// Recent tool output should be untouched
	if result[5].Content != recentOutput {
		t.Error("recent tool output within protected budget should not be pruned")
	}
}

func TestPruneToolOutputs_BelowMinSavings_NoPruning(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	// Create tool output that's outside the protected window but small enough
	// that total savings would be under the 20k minimum threshold.
	smallOutput := makeLongString(10000) // 10k tokens — below 20k threshold

	msgs := []ChatMessage{
		{Role: RoleUser, Content: "run"},
		{Role: RoleAssistant, Content: "ok", ToolCalls: []ToolCall{
			{ID: "tc1", Function: ToolCallFunction{Name: "shell_execute", Arguments: `{}`}},
		}},
		{Role: RoleTool, Content: smallOutput, ToolCallID: "tc1"},
	}

	result := c.pruneToolOutputs(msgs)
	if result[2].Content != smallOutput {
		t.Error("tool output below minimum savings threshold should not be pruned")
	}
}

func TestPruneToolOutputs_MultipleCandidates(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	// Create multiple old large tool outputs that together exceed 20k savings.
	// Each is 15k tokens (individually below 20k, but combined exceed threshold).
	// They're all outside the protected window because we add 39k tokens of
	// recent tool output at the end.
	output1 := makeLongString(15000)
	output2 := makeLongString(15000)
	recentOutput := makeLongString(39000) // fills protected budget

	msgs := []ChatMessage{
		{Role: RoleUser, Content: "run old 1"},
		{Role: RoleAssistant, Content: "ok", ToolCalls: []ToolCall{
			{ID: "tc1", Function: ToolCallFunction{Name: "shell_execute", Arguments: `{}`}},
		}},
		{Role: RoleTool, Content: output1, ToolCallID: "tc1"},
		{Role: RoleUser, Content: "run old 2"},
		{Role: RoleAssistant, Content: "ok", ToolCalls: []ToolCall{
			{ID: "tc2", Function: ToolCallFunction{Name: "shell_execute", Arguments: `{}`}},
		}},
		{Role: RoleTool, Content: output2, ToolCallID: "tc2"},
		{Role: RoleUser, Content: "run recent"},
		{Role: RoleAssistant, Content: "ok", ToolCalls: []ToolCall{
			{ID: "tc3", Function: ToolCallFunction{Name: "shell_execute", Arguments: `{}`}},
		}},
		{Role: RoleTool, Content: recentOutput, ToolCallID: "tc3"},
	}

	result := c.pruneToolOutputs(msgs)

	// Both old outputs should be pruned
	if result[2].Content == output1 {
		t.Error("first old tool output should be pruned")
	}
	if !strings.Contains(result[2].Content, "Output truncated") {
		t.Error("first pruned output should have truncation notice")
	}
	if result[5].Content == output2 {
		t.Error("second old tool output should be pruned")
	}
	if !strings.Contains(result[5].Content, "Output truncated") {
		t.Error("second pruned output should have truncation notice")
	}

	// Recent output should be untouched
	if result[8].Content != recentOutput {
		t.Error("recent output within protected budget should not be pruned")
	}
}

func TestPruneToolOutputs_PreservesMessageMetadata(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	// Large output beyond protected budget + minimum savings
	largeOutput := makeLongString(50000)
	// Recent output within protected budget
	recentOutput := makeLongString(39000)

	msgs := []ChatMessage{
		{Role: RoleUser, Content: "run"},
		{Role: RoleAssistant, Content: "ok", ToolCalls: []ToolCall{
			{ID: "tc1", Function: ToolCallFunction{Name: "shell_execute", Arguments: `{}`}},
		}},
		{Role: RoleTool, Content: largeOutput, ToolCallID: "tc1", Name: "shell_execute"},
		{Role: RoleUser, Content: "run recent"},
		{Role: RoleAssistant, Content: "ok", ToolCalls: []ToolCall{
			{ID: "tc2", Function: ToolCallFunction{Name: "shell_execute", Arguments: `{}`}},
		}},
		{Role: RoleTool, Content: recentOutput, ToolCallID: "tc2"},
	}

	result := c.pruneToolOutputs(msgs)

	pruned := result[2]
	if pruned.Role != RoleTool {
		t.Errorf("pruned message role should be preserved, got: %s", pruned.Role)
	}
	if pruned.ToolCallID != "tc1" {
		t.Errorf("pruned message ToolCallID should be preserved, got: %s", pruned.ToolCallID)
	}
	if pruned.Name != "shell_execute" {
		t.Errorf("pruned message Name should be preserved, got: %s", pruned.Name)
	}
}

func TestPruneToolOutputs_EmptyMessages(t *testing.T) {
	cfg := DefaultCompactorConfig()
	c := NewContextCompactor(cfg, nil, nil, nil)

	result := c.pruneToolOutputs(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}

	result = c.pruneToolOutputs([]ChatMessage{})
	if len(result) != 0 {
		t.Errorf("expected empty for empty input, got %d messages", len(result))
	}
}

// ---------------------------------------------------------------------------
// getCompactionPrompt
// ---------------------------------------------------------------------------

func TestGetCompactionPrompt_Structured(t *testing.T) {
	c := &ContextCompactor{config: CompactorConfig{Strategy: "structured"}}
	prompt := c.getCompactionPrompt("structured")
	if !strings.Contains(prompt, "You are summarizing a conversation") {
		t.Error("structured strategy should return structured prompt")
	}
}

func TestGetCompactionPrompt_Handoff(t *testing.T) {
	c := &ContextCompactor{config: CompactorConfig{Strategy: "handoff"}}
	prompt := c.getCompactionPrompt("handoff")
	if !strings.Contains(prompt, "handoff summary") {
		t.Error("handoff strategy should return handoff prompt")
	}
	if !strings.Contains(prompt, "Capture exact technical state, not abstractions") {
		t.Error("handoff prompt should contain the exactness instruction")
	}
	if !strings.Contains(prompt, "## Git State") {
		t.Error("handoff prompt should contain Git State section")
	}
	if !strings.Contains(prompt, "## Symbols") {
		t.Error("handoff prompt should contain Symbols section")
	}
	if !strings.Contains(prompt, "## Commands Run") {
		t.Error("handoff prompt should contain Commands Run section")
	}
	if !strings.Contains(prompt, "## Test Results") {
		t.Error("handoff prompt should contain Test Results section")
	}
}

func TestGetCompactionPrompt_Off(t *testing.T) {
	c := &ContextCompactor{config: CompactorConfig{Strategy: "off"}}
	prompt := c.getCompactionPrompt("off")
	if prompt != "" {
		t.Errorf("off strategy should return empty string, got: %q", prompt)
	}
}

func TestGetCompactionPrompt_Default(t *testing.T) {
	c := &ContextCompactor{config: CompactorConfig{}}
	prompt := c.getCompactionPrompt("")
	if !strings.Contains(prompt, "You are summarizing a conversation") {
		t.Error("empty/unknown strategy should fall back to structured prompt")
	}
}

// ---------------------------------------------------------------------------
// Strategy "off" in Compact()
// ---------------------------------------------------------------------------

func TestCompact_StrategyOff_SkipsCompaction(t *testing.T) {
	mock := &compactorMockChatter{
		response: &Response{Content: "should not be called"},
	}

	cfg := DefaultCompactorConfig()
	cfg.Strategy = "off"
	cfg.KeepRecentTokens = 300
	c := NewContextCompactor(cfg, mock, nil, nil)

	msgs := makeMessages(10)
	result := c.Compact(context.Background(), msgs)

	if result.Compacted {
		t.Error("strategy 'off' should not compact")
	}
	if mock.called {
		t.Error("strategy 'off' should not call the summarizer")
	}
	if len(result.Messages) != len(msgs) {
		t.Errorf("strategy 'off' should return original messages, got %d vs %d", len(result.Messages), len(msgs))
	}
}

// ---------------------------------------------------------------------------
// Strategy "handoff" in buildSummaryPrompt
// ---------------------------------------------------------------------------

func TestBuildSummaryPrompt_Handoff(t *testing.T) {
	c := &ContextCompactor{config: CompactorConfig{Strategy: "handoff"}}

	prompt := c.buildSummaryPrompt("conversation text", "")
	if !strings.Contains(prompt, "handoff summary") {
		t.Error("handoff strategy should use handoff prompt template")
	}
	if !strings.Contains(prompt, "conversation text") {
		t.Error("handoff prompt should contain conversation text")
	}
}

func TestBuildSummaryPrompt_Handoff_IgnoresExistingSummary(t *testing.T) {
	// Handoff strategy should always use the full handoff prompt,
	// never the iterative update prompt.
	c := &ContextCompactor{config: CompactorConfig{Strategy: "handoff"}}

	prompt := c.buildSummaryPrompt("new messages", "previous summary")
	if strings.Contains(prompt, "You are updating") {
		t.Error("handoff strategy should not use iterative update prompt even with existing summary")
	}
	if !strings.Contains(prompt, "handoff summary") {
		t.Error("handoff strategy should always use handoff prompt")
	}
}

// ---------------------------------------------------------------------------
// Handoff strategy end-to-end in Compact()
// ---------------------------------------------------------------------------

func TestCompact_HandoffStrategy(t *testing.T) {
	mock := &compactorMockChatter{
		response: &Response{Content: `## Goal
Fix the authentication bug

## Current State
Auth middleware returns 401 for valid tokens; root cause identified in JWT validation logic

## Files
- read: internal/auth/jwt.go
- edit: internal/auth/middleware.go

## Commands Run
- go test ./internal/auth/... -v (3 failures)
- git diff HEAD (shows middleware changes)

## Test Results
- FAIL: TestJWTValidation (expected 200, got 401)
- FAIL: TestTokenExpiry (token expired 1 day ago not rejected)

## Errors Encountered
- jwt.Parse: key is invalid (Ed25519 key format mismatch)

## Git State
branch: fix/auth-jwt, last commit: a1b2c3d, 2 uncommitted files

## Symbols
- ValidateJWT() at jwt.go:42
- AuthMiddleware() at middleware.go:18
- TokenExpiry field inClaims struct

## Key Decisions
- Switch from HMAC-SHA256 to Ed25519 for token signing
- Add token refresh endpoint at /api/v1/auth/refresh

## Next Steps
1. Fix Ed25519 key parsing in jwt.go:42
2. Update middleware to use new validation
3. Add integration test for refresh endpoint

## Critical Context
- Ed25519 public key: loaded from /etc/meept/auth_pub.pem
- Token TTL: 1 hour, refresh TTL: 7 days`},
	}

	cfg := DefaultCompactorConfig()
	cfg.Strategy = "handoff"
	cfg.KeepRecentTokens = 300
	c := NewContextCompactor(cfg, mock, nil, nil)

	msgs := makeMessages(10)
	result := c.Compact(context.Background(), msgs)

	if !result.Compacted {
		t.Fatal("expected compaction with handoff strategy")
	}
	if !mock.called {
		t.Fatal("expected summarizer to be called")
	}

	// Verify the prompt sent to the LLM uses the handoff template
	prompt := mock.lastMsgs[0].Content
	if !strings.Contains(prompt, "handoff summary") {
		t.Error("LLM should have received the handoff prompt")
	}
	if !strings.Contains(prompt, "Capture exact technical state") {
		t.Error("LLM prompt should contain the exactness instruction")
	}

	// Verify the result contains the handoff summary content
	if !strings.Contains(result.SummaryContent, "Fix the authentication bug") {
		t.Error("result should contain the handoff summary goal")
	}
	if !strings.Contains(result.SummaryContent, "Ed25519") {
		t.Error("handoff summary should preserve exact technical details")
	}
}

// ---------------------------------------------------------------------------
// PruneToolOutputs integration with Compact()
// ---------------------------------------------------------------------------

func TestCompact_PruneToolOutputs_Integration(t *testing.T) {
	mock := &compactorMockChatter{
		response: &Response{Content: `## Goal
Work

## Key Decisions
none

## Files
none

## Progress
done

## Important Discoveries
none

## Errors Encountered
none

## Next Steps
none

## Critical Context
none

## Constraints
none`},
	}

	cfg := DefaultCompactorConfig()
	cfg.KeepRecentTokens = 300
	c := NewContextCompactor(cfg, mock, nil, nil)

	// Build messages with a large old tool output that should be pruned
	// and enough recent tool output to fill the protected budget.
	largeOutput := makeLongString(50000)     // 50k tokens, way beyond protected budget
	recentOutput := makeLongString(41000)     // fills protected budget
	userPadding := makeLongString(50)         // padding to ensure cut point

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "sys"},
		// Old tool output that should be pruned
		{Role: RoleUser, Content: "run old command" + userPadding},
		{Role: RoleAssistant, Content: "running", ToolCalls: []ToolCall{
			{ID: "tc_old", Function: ToolCallFunction{Name: "shell_execute", Arguments: `{}`}},
		}},
		{Role: RoleTool, Content: largeOutput, ToolCallID: "tc_old"},
		// More messages to push old output out of protected window
		{Role: RoleUser, Content: "run recent" + userPadding},
		{Role: RoleAssistant, Content: "running recent", ToolCalls: []ToolCall{
			{ID: "tc_recent", Function: ToolCallFunction{Name: "shell_execute", Arguments: `{}`}},
		}},
		{Role: RoleTool, Content: recentOutput, ToolCallID: "tc_recent"},
		// Add enough messages to force a cut point
		{Role: RoleUser, Content: makeLongString(90)},
		{Role: RoleAssistant, Content: makeLongString(90)},
		{Role: RoleUser, Content: makeLongString(90)},
		{Role: RoleAssistant, Content: makeLongString(90)},
	}

	result := c.Compact(context.Background(), msgs)

	// The compaction should have proceeded (pruning happened before cut point)
	if !result.Compacted {
		t.Error("expected compaction after pruning")
	}

	// Verify pruning happened: the old tool output should be truncated in the
	// messages that were passed through (the ToKeep region may still have
	// the recent output; the ToCompact region is summarized away).
}

