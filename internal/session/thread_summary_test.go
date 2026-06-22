package session

import (
	"strings"
	"testing"
	"time"
)

func TestAssembleThreadContext_Empty(t *testing.T) {
	t.Parallel()
	got := AssembleThreadContext(nil, "")
	if got != "" {
		t.Errorf("expected empty string for nil threads, got %q", got)
	}
	got = AssembleThreadContext([]*Thread{}, "")
	if got != "" {
		t.Errorf("expected empty string for no threads, got %q", got)
	}
}

func TestAssembleThreadContext_SingleActiveThread(t *testing.T) {
	t.Parallel()
	threads := []*Thread{
		{
			ID:         "thread-active",
			TopicLabel: "work",
			Summary:    "this should not appear because it's active",
			IsActive:   true,
		},
	}
	got := AssembleThreadContext(threads, "thread-active")
	if got != "" {
		t.Errorf("expected empty context when only the active thread exists, got %q", got)
	}
}

func TestAssembleThreadContext_SingleActiveThread_OmitsEmptySummary(t *testing.T) {
	t.Parallel()
	// Even an inactive thread with empty summary should be skipped.
	threads := []*Thread{
		{
			ID:         "thread-inactive",
			TopicLabel: "side",
			Summary:    "",
			IsActive:   false,
		},
	}
	got := AssembleThreadContext(threads, "thread-active")
	if got != "" {
		t.Errorf("expected empty context when only empty-summary threads exist, got %q", got)
	}
}

func TestAssembleThreadContext_MultipleInactiveThreads(t *testing.T) {
	t.Parallel()
	threads := []*Thread{
		{
			ID:         "thread-active",
			TopicLabel: "current",
			Summary:    "should not appear",
			IsActive:   true,
		},
		{
			ID:         "thread-work",
			TopicLabel: "work",
			Summary:    "discussed project architecture",
			IsActive:   false,
		},
		{
			ID:         "thread-food",
			TopicLabel: "food",
			Summary:    "talked about pizza",
			IsActive:   false,
		},
	}
	got := AssembleThreadContext(threads, "thread-active")
	if got == "" {
		t.Fatal("expected non-empty context")
	}
	if strings.Contains(got, "should not appear") {
		t.Error("active thread summary should not be included")
	}
	if !strings.Contains(got, "[Context from work thread]") {
		t.Errorf("expected work thread context header, got %q", got)
	}
	if !strings.Contains(got, "discussed project architecture") {
		t.Errorf("expected work summary in context, got %q", got)
	}
	if !strings.Contains(got, "[Context from food thread]") {
		t.Errorf("expected food thread context header, got %q", got)
	}
	if !strings.Contains(got, "talked about pizza") {
		t.Errorf("expected food summary in context, got %q", got)
	}
}

func TestAssembleThreadContext_AllInactiveExcludedIfNoSummary(t *testing.T) {
	t.Parallel()
	threads := []*Thread{
		{ID: "t1", TopicLabel: "a", Summary: ""},
		{ID: "t2", TopicLabel: "b", Summary: ""},
	}
	got := AssembleThreadContext(threads, "missing-active")
	if got != "" {
		t.Errorf("expected empty when all threads have empty summaries, got %q", got)
	}
}

func TestGenerateThreadSummary_Empty(t *testing.T) {
	t.Parallel()
	got := GenerateThreadSummary(nil)
	if got != "" {
		t.Errorf("expected empty summary for nil messages, got %q", got)
	}
	got = GenerateThreadSummary([]Message{})
	if got != "" {
		t.Errorf("expected empty summary for empty messages, got %q", got)
	}
}

func TestGenerateThreadSummary_ShortMessages(t *testing.T) {
	t.Parallel()
	messages := []Message{
		{
			Role:    "user",
			Content: "hello",
		},
		{
			Role:    "assistant",
			Content: "hi there",
		},
	}
	got := GenerateThreadSummary(messages)
	if got == "" {
		t.Fatal("expected non-empty summary")
	}
	if !strings.Contains(got, "Discussion from user") {
		t.Errorf("expected 'Discussion from user' prefix, got %q", got)
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("expected first message content, got %q", got)
	}
	if !strings.Contains(got, "latest") {
		t.Errorf("expected 'latest' marker, got %q", got)
	}
	if !strings.Contains(got, "hi there") {
		t.Errorf("expected last message content, got %q", got)
	}
}

func TestGenerateThreadSummary_LongMessages(t *testing.T) {
	t.Parallel()
	// Build a message longer than the 100-char preview cutoff.
	longFirst := strings.Repeat("a", 200)
	longLast := strings.Repeat("b", 200)
	messages := []Message{
		{
			Role:    "user",
			Content: longFirst,
		},
		{
			Role:    "assistant",
			Content: longLast,
		},
	}
	got := GenerateThreadSummary(messages)
	if got == "" {
		t.Fatal("expected non-empty summary")
	}
	// The truncateString helper caps at 100 chars including the trailing "...",
	// so the full 200-char content should NOT appear.
	if strings.Contains(got, strings.Repeat("a", 200)) {
		t.Error("expected first message to be truncated in summary")
	}
	if strings.Contains(got, strings.Repeat("b", 200)) {
		t.Error("expected last message to be truncated in summary")
	}
	// The truncation marker should be present.
	if !strings.Contains(got, "...") {
		t.Errorf("expected '...' truncation marker, got %q", got)
	}
	// Sanity-check the exact truncated form.
	expectedFirstPreview := strings.Repeat("a", 97) + "..."
	if !strings.Contains(got, expectedFirstPreview) {
		t.Errorf("expected first preview %q in summary, got %q", expectedFirstPreview, got)
	}
}

func TestGenerateThreadSummary_SingleMessage(t *testing.T) {
	t.Parallel()
	messages := []Message{
		{
			Role:    "user",
			Content: "only one message",
		},
	}
	got := GenerateThreadSummary(messages)
	if got == "" {
		t.Fatal("expected non-empty summary")
	}
	if !strings.Contains(got, "only one message") {
		t.Errorf("expected the single message content, got %q", got)
	}
}

func TestTruncateString_ShorterThanMax(t *testing.T) {
	t.Parallel()
	got := truncateString("abc", 10)
	if got != "abc" {
		t.Errorf("expected %q, got %q", "abc", got)
	}
}

func TestTruncateString_ExactMax(t *testing.T) {
	t.Parallel()
	got := truncateString("12345", 5)
	if got != "12345" {
		t.Errorf("expected %q, got %q", "12345", got)
	}
}

func TestTruncateString_LongerThanMax(t *testing.T) {
	t.Parallel()
	got := truncateString("abcdefghij", 8)
	if got != "abcde..." {
		t.Errorf("expected %q, got %q", "abcde...", got)
	}
}

func TestThread_TouchNilSafe(t *testing.T) {
	t.Parallel()
	var th *Thread
	th.Touch() // must not panic
}

// TestAssembleThreadContext_MixedSummaryState exercises the filter logic:
// active thread (skip), inactive with summary (include), inactive empty (skip).
func TestAssembleThreadContext_MixedSummaryState(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	threads := []*Thread{
		{
			ID:         "active",
			TopicLabel: "active",
			Summary:    "active summary",
			IsActive:   true,
			CreatedAt:  now,
		},
		{
			ID:         "inactive-summary",
			TopicLabel: "context",
			Summary:    "this should appear",
			IsActive:   false,
			CreatedAt:  now,
		},
		{
			ID:         "inactive-empty",
			TopicLabel: "nocontext",
			Summary:    "",
			IsActive:   false,
			CreatedAt:  now,
		},
	}
	got := AssembleThreadContext(threads, "active")
	if !strings.Contains(got, "this should appear") {
		t.Errorf("expected inactive-with-summary content, got %q", got)
	}
	if strings.Contains(got, "active summary") {
		t.Errorf("active thread summary leaked: %q", got)
	}
	if !strings.HasPrefix(strings.TrimSpace(got), "[Context from context thread]") {
		t.Errorf("expected leading context header, got %q", got)
	}
	// inactive-empty should not contribute a header.
	if strings.Contains(got, "[Context from nocontext thread]") {
		t.Errorf("empty-summary thread should not produce context header: %q", got)
	}
}
