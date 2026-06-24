package llm

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

// mockStructuredChatter returns a pre-canned structured summary response that
// mimics what an LLM would produce given structuredSummaryPromptTemplate.
type mockStructuredChatter struct {
	response string
	err      error
}

// sequencedMockChatter returns a sequence of pre-canned responses, one per call.
type sequencedMockChatter struct {
	responses []string
	calls     atomic.Int32
	err       error
}

func (s *sequencedMockChatter) Chat(_ context.Context, _ []ChatMessage, _ ...ChatOption) (*Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	idx := int(s.calls.Add(1)) - 1
	if idx >= len(s.responses) {
		idx = len(s.responses) - 1
	}
	return &Response{Content: s.responses[idx]}, nil
}

func (s *sequencedMockChatter) ChatWithProgress(_ context.Context, messages []ChatMessage, _ ProgressCallback, _ ...ChatOption) (*Response, error) {
	return s.Chat(context.Background(), messages)
}

func (s *sequencedMockChatter) Config() *ModelConfig {
	return &ModelConfig{}
}

func (m *mockStructuredChatter) Chat(_ context.Context, messages []ChatMessage, _ ...ChatOption) (*Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &Response{Content: m.response}, nil
}

func (m *mockStructuredChatter) ChatWithProgress(_ context.Context, messages []ChatMessage, _ ProgressCallback, _ ...ChatOption) (*Response, error) {
	return m.Chat(context.Background(), messages)
}

func (m *mockStructuredChatter) Config() *ModelConfig {
	return &ModelConfig{}
}

// codeHeavyStructuredResponse is a realistic structured response from a
// code-heavy conversation.
const codeHeavyStructuredResponse = `
DECISIONS:
- Use SQLite for persistent storage instead of flat files
- Switch from REST to gRPC for internal service communication
- Adopt repository pattern for data access layer

FILES:
- internal/storage/sqlite.go
- internal/storage/sqlite_test.go
- internal/api/handler.go
- config/models.json5
- cmd/meept-daemon/main.go

QUESTIONS:
- Should we add connection pooling to the SQLite layer?
- What is the optimal batch size for bulk inserts?

STATUS:
Refactoring storage layer to use SQLite; core CRUD operations implemented, tests pending.

FINDINGS:
- SQLite WAL mode improves concurrent read performance by 40%
- The existing flat-file format has no schema validation
- Current migration script handles only forward migrations

SUMMARY:
The conversation covered refactoring the storage layer from flat files to SQLite. The team decided on SQLite with WAL mode for better concurrency. Key files were identified in internal/storage/ and internal/api/. Two open questions remain around connection pooling and batch sizing.
`

func TestParseStructuredSummary_CodeHeavyConversation(t *testing.T) {
	ext := parseStructuredSummary(codeHeavyStructuredResponse)

	if len(ext.Decisions) != 3 {
		t.Errorf("expected 3 decisions, got %d: %v", len(ext.Decisions), ext.Decisions)
	}
	if len(ext.FilePaths) != 5 {
		t.Errorf("expected 5 file paths, got %d: %v", len(ext.FilePaths), ext.FilePaths)
	}
	if len(ext.UnresolvedQuestions) != 2 {
		t.Errorf("expected 2 unresolved questions, got %d: %v", len(ext.UnresolvedQuestions), ext.UnresolvedQuestions)
	}
	if len(ext.KeyFindings) != 3 {
		t.Errorf("expected 3 findings, got %d: %v", len(ext.KeyFindings), ext.KeyFindings)
	}
	if ext.TaskState == "" {
		t.Error("expected non-empty task state")
	}
}

func TestParseStructuredSummary_FilePathExtraction(t *testing.T) {
	response := `
DECISIONS:
- Add new authentication middleware

FILES:
- internal/middleware/auth.go
- internal/middleware/auth_test.go
- internal/config/routes.go
- /etc/app/config.yaml

QUESTIONS:
- What OAuth provider should we use?

STATUS:
Auth middleware implementation in progress.

FINDINGS:
- JWT tokens need rotation every 24 hours

SUMMARY:
Adding authentication middleware to the API.
`
	ext := parseStructuredSummary(response)

	expectedPaths := []string{
		"internal/middleware/auth.go",
		"internal/middleware/auth_test.go",
		"internal/config/routes.go",
		"/etc/app/config.yaml",
	}
	if len(ext.FilePaths) != len(expectedPaths) {
		t.Fatalf("expected %d file paths, got %d: %v", len(expectedPaths), len(ext.FilePaths), ext.FilePaths)
	}
	for i, expected := range expectedPaths {
		if ext.FilePaths[i] != expected {
			t.Errorf("file path[%d]: expected %q, got %q", i, expected, ext.FilePaths[i])
		}
	}
}

func TestParseStructuredSummary_DecisionDetection(t *testing.T) {
	response := `
DECISIONS:
- Adopt microservices architecture
- Use event-driven communication between services
- Implement circuit breaker pattern for fault tolerance
- Choose Go as primary backend language

FILES:

QUESTIONS:

STATUS:
Architecture design phase completed.

FINDINGS:
- Monolith cannot scale beyond current load

SUMMARY:
Architecture decisions finalized.
`
	ext := parseStructuredSummary(response)

	if len(ext.Decisions) != 4 {
		t.Errorf("expected 4 decisions, got %d: %v", len(ext.Decisions), ext.Decisions)
	}

	// Verify specific decisions are present
	found := false
	for _, d := range ext.Decisions {
		if strings.Contains(d, "circuit breaker") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find circuit breaker decision")
	}

	// Empty sections should produce nil slices
	if ext.FilePaths != nil {
		t.Errorf("expected nil FilePaths for empty section, got %v", ext.FilePaths)
	}
	if ext.UnresolvedQuestions != nil {
		t.Errorf("expected nil UnresolvedQuestions for empty section, got %v", ext.UnresolvedQuestions)
	}
}

func TestParseStructuredSummary_EmptyInput(t *testing.T) {
	ext := parseStructuredSummary("")
	if len(ext.Decisions) != 0 {
		t.Errorf("expected 0 decisions for empty input, got %d", len(ext.Decisions))
	}
	if len(ext.FilePaths) != 0 {
		t.Errorf("expected 0 file paths for empty input, got %d", len(ext.FilePaths))
	}
	if ext.TaskState != "" {
		t.Errorf("expected empty task state for empty input, got %q", ext.TaskState)
	}
}

func TestParseStructuredSummary_PlainTextFallback(t *testing.T) {
	// When the LLM returns unstructured text (no section headers),
	// all structured fields should be empty/nil.
	ext := parseStructuredSummary("This is just a plain text response with no structure.")
	if len(ext.Decisions) != 0 {
		t.Errorf("expected 0 decisions for unstructured input, got %d", len(ext.Decisions))
	}
	if ext.TaskState != "" {
		t.Errorf("expected empty task state for unstructured input, got %q", ext.TaskState)
	}
}

func TestSplitStructuredSections(t *testing.T) {
	raw := `DECISIONS:
- decision one
- decision two

FILES:
- file_a.go
- file_b.go

STATUS:
in progress

SUMMARY:
some narrative here
`
	sections := splitStructuredSections(raw)

	if len(sections) != 4 {
		t.Fatalf("expected 4 sections, got %d: %v", len(sections), sections)
	}
	if _, ok := sections["DECISIONS"]; !ok {
		t.Error("missing DECISIONS section")
	}
	if _, ok := sections["FILES"]; !ok {
		t.Error("missing FILES section")
	}
	if _, ok := sections["STATUS"]; !ok {
		t.Error("missing STATUS section")
	}
	if _, ok := sections["SUMMARY"]; !ok {
		t.Error("missing SUMMARY section")
	}
}

func TestParseBulletItems(t *testing.T) {
	section := `
- first item
- second item
- third item with more detail
`
	items := parseBulletItems(section)
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d: %v", len(items), items)
	}
	if items[0] != "first item" {
		t.Errorf("item[0]: expected %q, got %q", "first item", items[0])
	}
	if items[2] != "third item with more detail" {
		t.Errorf("item[2]: expected %q, got %q", "third item with more detail", items[2])
	}
}

func TestParseBulletItems_EmptySection(t *testing.T) {
	items := parseBulletItems("")
	if items != nil {
		t.Errorf("expected nil for empty section, got %v", items)
	}
}

func TestParseBulletItems_NoBullets(t *testing.T) {
	items := parseBulletItems("just some text\nno bullets here")
	if items != nil {
		t.Errorf("expected nil for no-bullet section, got %v", items)
	}
}

func TestExtractNarrative(t *testing.T) {
	raw := `
DECISIONS:
- something

SUMMARY:
This is the narrative part of the summary that should be extracted cleanly.
`
	narrative := extractNarrative(raw)
	expected := "This is the narrative part of the summary that should be extracted cleanly."
	if narrative != expected {
		t.Errorf("expected %q, got %q", expected, narrative)
	}
}

func TestFormatStructuredSummary(t *testing.T) {
	ext := SummaryExtract{
		Decisions:           []string{"use SQLite", "add caching"},
		FilePaths:           []string{"internal/db.go"},
		UnresolvedQuestions: []string{"pool size?"},
		TaskState:           "in progress",
		KeyFindings:         []string{"WAL mode is fast"},
	}
	narrative := "The team decided on SQLite."
	result := formatStructuredSummary(1, ext, narrative)

	if !strings.HasPrefix(result, "[Conversation summary level 1]:") {
		t.Errorf("expected level prefix with colon, got: %s", result)
	}
	if !strings.Contains(result, "status: in progress.") {
		t.Errorf("expected task state in output, got: %s", result)
	}
	if !strings.Contains(result, "use SQLite") {
		t.Errorf("expected decision in output, got: %s", result)
	}
	if !strings.Contains(result, "internal/db.go") {
		t.Errorf("expected file path in output, got: %s", result)
	}
	if !strings.Contains(result, "pool size?") {
		t.Errorf("expected unresolved question in output, got: %s", result)
	}
	if !strings.Contains(result, "WAL mode is fast") {
		t.Errorf("expected finding in output, got: %s", result)
	}
	if !strings.Contains(result, narrative) {
		t.Errorf("expected narrative in output, got: %s", result)
	}
}

func TestFormatStructuredSummary_EmptyExtract(t *testing.T) {
	ext := SummaryExtract{}
	result := formatStructuredSummary(2, ext, "")
	if !strings.HasPrefix(result, "[Conversation summary level 2]:") {
		t.Errorf("expected level prefix with colon, got: %s", result)
	}
	// Should not contain section labels when fields are empty
	if strings.Contains(result, "decisions:") {
		t.Errorf("expected no decisions label when empty, got: %s", result)
	}
}

func TestMarshalExtractAsJSON(t *testing.T) {
	ext := SummaryExtract{
		Decisions: []string{"decide"},
		FilePaths: []string{"file.go"},
		TaskState: "active",
	}
	jsonStr := marshalExtractAsJSON(ext)
	if !strings.Contains(jsonStr, `"decide"`) {
		t.Errorf("expected decision in JSON, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"file.go"`) {
		t.Errorf("expected file path in JSON, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"active"`) {
		t.Errorf("expected task state in JSON, got: %s", jsonStr)
	}
}

// --- Integration-style tests with the firewall ---

func TestStructuredSummarization_IntegrationWithFirewall(t *testing.T) {
	// Verify that the full pipeline (firewall -> structured summarizer) works
	// end-to-end and produces a summary message with structured content.
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		SummarizeHistory:       true,
		DropContextOnHardLimit: false,
		HardLimit:              0.30,
		WrapUpThreshold:        0.10,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	summarizer := &mockStructuredChatter{response: codeHeavyStructuredResponse}
	firewall := NewContextFirewall(inner, model, cfg, summarizer, nil, &HeuristicTokenizer{})

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: "latest"},
	}

	result, err := firewall.summarizeOldHistory(context.Background(), msgs)
	if err != nil {
		t.Fatalf("summarizeOldHistory: unexpected error: %v", err)
	}

	// Find the summary message
	var summary *ChatMessage
	for i := range result {
		if result[i].SummaryLevel > 0 {
			summary = &result[i]
			break
		}
	}

	if summary == nil {
		t.Fatal("expected to find a summary message with SummaryLevel > 0")
	}

	// The summary content should contain structured data wrapped in boundary markers
	if !strings.HasPrefix(summary.Content, "<<<CONTEXT_SUMMARY") {
		t.Errorf("expected summary to start with boundary marker, got: %s", summary.Content[:min(60, len(summary.Content))])
	}
	if !strings.Contains(summary.Content, "[Conversation summary level 1]:") {
		t.Errorf("expected structured level prefix inside boundary, got: %s", summary.Content[:min(100, len(summary.Content))])
	}
	// Should contain file paths from the structured response
	if !strings.Contains(summary.Content, "internal/storage/sqlite.go") {
		t.Errorf("expected file path in structured summary, got: %s", summary.Content)
	}
	// Should contain decisions
	if !strings.Contains(summary.Content, "SQLite") {
		t.Errorf("expected decision mention in structured summary, got: %s", summary.Content)
	}
}

func TestStructuredSummarization_WithHierarchicalRecursion(t *testing.T) {
	// The structured summarizer returns a very long response that exceeds
	// the SummaryLevelThreshold, triggering hierarchical re-summarization.
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:                   true,
		SummarizeHistory:          true,
		DropContextOnHardLimit:    false,
		HardLimit:                 0.30,
		WrapUpThreshold:           0.10,
		HierarchicalSummarization: true,
		MaxSummaryLevel:           3,
		SummaryLevelThreshold:     50,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	// First call returns a long structured response (exceeds 50-token threshold),
	// second call returns a short structured response (under threshold).
	longResponse := "DECISIONS:\n" + strings.Repeat("- this is a long decision item that takes many tokens\n", 20) +
		"FILES:\n- internal/a.go\n- internal/b.go\n" +
		"STATUS:\nin progress\n" +
		"SUMMARY:\nlong narrative summary here\n"
	shortResponse := "DECISIONS:\n- short decision\nFILES:\nSTATUS:\ndone\nSUMMARY:\nshort.\n"
	summarizer := &sequencedMockChatter{responses: []string{longResponse, shortResponse}}
	firewall := NewContextFirewall(inner, model, cfg, summarizer, nil, &HeuristicTokenizer{})

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: "latest"},
	}

	result, err := firewall.summarizeOldHistory(context.Background(), msgs)
	if err != nil {
		t.Fatalf("summarizeOldHistory: unexpected error: %v", err)
	}

	// Find the summary message
	var summaries []ChatMessage
	for _, m := range result {
		if m.SummaryLevel > 0 {
			summaries = append(summaries, m)
		}
	}

	if len(summaries) != 1 {
		t.Fatalf("expected exactly 1 summary message, got %d", len(summaries))
	}

	// Should have recursed to level 2
	if summaries[0].SummaryLevel != 2 {
		t.Errorf("expected summary level 2 after hierarchical recursion, got %d", summaries[0].SummaryLevel)
	}

	// Should have made 2 summarizer calls
	if got := summarizer.calls.Load(); got != 2 {
		t.Errorf("expected 2 summarizer calls, got %d", got)
	}
}

func TestStructuredSummarization_FallbackOnUnstructuredResponse(t *testing.T) {
	// When the summarizer returns unstructured text, the parse functions
	// should gracefully handle it without panicking.
	model := &ModelConfig{ContextLimit: 1000}
	cfg := ContextFirewallConfig{
		Enabled:                true,
		SummarizeHistory:       true,
		DropContextOnHardLimit: false,
		HardLimit:              0.30,
		WrapUpThreshold:        0.10,
	}

	inner := &stubChatter{resp: &Response{Content: "ok"}}
	summarizer := &mockStructuredChatter{response: "Just a plain summary without any structured sections."}
	firewall := NewContextFirewall(inner, model, cfg, summarizer, nil, &HeuristicTokenizer{})

	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: makeFiller(80)},
		{Role: RoleAssistant, Content: makeFiller(80)},
		{Role: RoleUser, Content: "latest"},
	}

	result, err := firewall.summarizeOldHistory(context.Background(), msgs)
	if err != nil {
		t.Fatalf("summarizeOldHistory: unexpected error: %v", err)
	}

	// Should still produce a summary message
	var summary *ChatMessage
	for i := range result {
		if result[i].SummaryLevel > 0 {
			summary = &result[i]
			break
		}
	}

	if summary == nil {
		t.Fatal("expected a summary message even with unstructured response")
	}

	// The content should still have the level prefix, now wrapped in boundary markers
	if !strings.HasPrefix(summary.Content, "<<<CONTEXT_SUMMARY") {
		t.Errorf("expected boundary marker prefix even on fallback, got: %s", summary.Content[:min(60, len(summary.Content))])
	}
	if !strings.Contains(summary.Content, "[Conversation summary level 1]") {
		t.Errorf("expected level prefix inside boundary even on fallback, got: %s", summary.Content[:min(100, len(summary.Content))])
	}
}
