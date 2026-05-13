package agent

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// ---------------------------------------------------------------------------
// End-to-end compaction tests exercising the Conversation layer's
// GetCompactionCandidates / RemoveCompactedMessages / CompressByImportance
// to simulate the full compaction workflow an agent loop would perform.
//
// These tests do NOT use the LLM compactor directly (that lives in the llm
// package). Instead, they verify that the Conversation layer correctly:
// 1. Identifies compaction candidates based on importance
// 2. Removes compacted messages while preserving critical ones
// 3. Tracks token savings accurately
// 4. Works correctly when combined with CompressByImportance
// ---------------------------------------------------------------------------

// TestE2E_Compaction_ShortConversationNoCandidates verifies that a short
// conversation does not produce any compaction candidates when the target
// ratio is high enough that no compression is needed.
func TestE2E_Compaction_ShortConversationNoCandidates(t *testing.T) {
	conv := NewConversation()
	conv.AddUserMessage("What is the capital of France?")
	conv.AddAssistantMessage("The capital of France is Paris.")

	// Use 1.0 ratio (keep everything) -- no compression needed
	candidates, report := conv.GetCompactionCandidates(1.0)

	if len(candidates) != 0 {
		t.Errorf("expected no candidates for short conversation at 1.0 ratio, got %d", len(candidates))
	}
	if report.TokensRemoved != 0 {
		t.Errorf("expected TokensRemoved=0, got %d", report.TokensRemoved)
	}
	if report.MessagesBefore != report.MessagesAfter {
		t.Errorf("MessagesBefore=%d should equal MessagesAfter=%d for short conversation",
			report.MessagesBefore, report.MessagesAfter)
	}
}

// TestE2E_Compaction_LongConversationProducesCandidates verifies that a long
// conversation with many messages produces compaction candidates at a target ratio.
func TestE2E_Compaction_LongConversationProducesCandidates(t *testing.T) {
	conv := NewConversation()
	conv.AddUserMessage("Debug the authentication bug")

	// Add many messages to simulate a long conversation
	for i := range 30 {
		conv.AddAssistantMessage("let me check the code in step " + strings.Repeat("x ", 20) + " " + string(rune('a'+i%26)))
		conv.AddToolResult("call_"+strings.Repeat("x", 20), `{"status": "ok", "data": "`+strings.Repeat("result ", 15)+`"}`)
	}
	conv.AddAssistantMessage("The issue is in middleware.go line 42.")

	candidates, report := conv.GetCompactionCandidates(0.3)

	if len(candidates) == 0 {
		t.Error("expected compaction candidates for long conversation")
	}
	if report.TokensBefore == 0 {
		t.Error("expected TokensBefore > 0")
	}
	if report.TokensRemoved == 0 {
		t.Error("expected TokensRemoved > 0")
	}
	if report.MessagesAfter >= report.MessagesBefore {
		t.Errorf("expected MessagesAfter < MessagesBefore, got after=%d before=%d",
			report.MessagesAfter, report.MessagesBefore)
	}
}

// TestE2E_Compaction_RemoveCompactedMessagesPreservesCritical verifies that
// removing compacted messages preserves anchor and user messages.
func TestE2E_Compaction_RemoveCompactedMessagesPreservesCritical(t *testing.T) {
	conv := NewConversation()

	// Add anchor (must survive)
	conv.AddAnchorMessage(llm.RoleSystem, "Validation: always check JWT expiry")
	// Add user message (must survive)
	conv.AddUserMessage("Fix the auth bug")

	// Add reasoning/tool messages that should be candidates for removal
	for i := range 20 {
		conv.AddAssistantMessage("analyzing file " + strings.Repeat("data ", 30) + string(rune('A'+i%26)))
	}
	conv.AddAssistantMessage("Root cause identified: expired tokens not checked.")

	candidates, _ := conv.GetCompactionCandidates(0.3)
	if len(candidates) == 0 {
		t.Fatal("expected compaction candidates")
	}

	tokensSaved := conv.RemoveCompactedMessages(candidates)
	if tokensSaved <= 0 {
		t.Error("expected positive tokensSaved")
	}

	// Verify anchor message survives
	msgs := conv.GetMessages()
	hasAnchor := false
	for _, msg := range msgs {
		if msg.Content == "Validation: always check JWT expiry" {
			hasAnchor = true
			break
		}
	}
	if !hasAnchor {
		t.Error("anchor message should be preserved after compaction")
	}

	// Verify user message survives
	hasUser := false
	for _, msg := range msgs {
		if msg.Content == "Fix the auth bug" && msg.Role == llm.RoleUser {
			hasUser = true
			break
		}
	}
	if !hasUser {
		t.Error("user message should be preserved after compaction")
	}
}

// TestE2E_Compaction_WithToolCallChains verifies that conversations containing
// tool call/result pairs produce correct compaction candidates and that
// removal respects the tool call structure without panicking.
func TestE2E_Compaction_WithToolCallChains(t *testing.T) {
	conv := NewConversation()
	conv.AddUserMessage("Read all Go files and fix the lint errors")

	toolCalls := []llm.ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "internal/auth/handler.go"}`,
			},
		},
	}
	conv.AddAssistantMessageWithToolCalls("I'll read the handler file.", toolCalls)
	conv.AddToolResult("call_1", "package auth\nfunc HandleAuth() { ... }")

	toolCalls2 := []llm.ToolCall{
		{
			ID:   "call_2",
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "internal/auth/middleware.go"}`,
			},
		},
	}
	conv.AddAssistantMessageWithToolCalls("Now reading middleware.", toolCalls2)
	conv.AddToolResult("call_2", "package auth\nfunc Middleware() { ... }")

	// Add enough additional messages to trigger compaction
	for range 15 {
		conv.AddAssistantMessage("analyzing code structure " + strings.Repeat("detail ", 25))
	}
	conv.AddAssistantMessage("Fixed all lint errors in handler.go and middleware.go.")

	candidates, report := conv.GetCompactionCandidates(0.4)
	if len(candidates) == 0 {
		t.Fatal("expected compaction candidates for conversation with tool calls")
	}

	// Remove candidates and verify no panic
	tokensSaved := conv.RemoveCompactedMessages(candidates)
	if tokensSaved <= 0 {
		t.Error("expected positive tokensSaved")
	}

	// Conversation should still be consistent
	msgs := conv.GetMessages()
	if len(msgs) == 0 {
		t.Error("conversation should not be empty after compaction")
	}

	// Verify user message survives
	hasUser := false
	for _, msg := range msgs {
		if msg.Content == "Read all Go files and fix the lint errors" && msg.Role == llm.RoleUser {
			hasUser = true
			break
		}
	}
	if !hasUser {
		t.Error("user message should survive compaction with tool call chains")
	}

	_ = report
}

// TestE2E_Compaction_CompressThenCompact verifies that CompressByImportance
// and GetCompactionCandidates/RemoveCompactedMessages can work together
// in sequence.
func TestE2E_Compaction_CompressThenCompact(t *testing.T) {
	conv := NewConversation()
	conv.AddUserMessage("Build the authentication system")

	// Add many messages to create context pressure
	for i := range 25 {
		conv.AddAssistantMessage("working on step " + strings.Repeat("analysis ", 20) + string(rune('A'+i%26)))
	}

	msgsBefore := conv.Len()

	// Step 1: Compress by importance (removes low-importance messages)
	report := conv.CompressByImportance(0.5)
	if report.MessagesAfter >= msgsBefore {
		t.Errorf("compression should reduce messages: before=%d after=%d",
			msgsBefore, report.MessagesAfter)
	}

	// Step 2: Get compaction candidates on the remaining messages
	candidates, compReport := conv.GetCompactionCandidates(0.4)
	if len(candidates) > 0 {
		// Remove compacted messages
		tokensSaved := conv.RemoveCompactedMessages(candidates)
		if tokensSaved <= 0 {
			t.Error("expected positive tokensSaved from RemoveCompactedMessages")
		}
	}

	// Final state: user message should still be present
	msgs := conv.GetMessages()
	hasUser := false
	for _, msg := range msgs {
		if msg.Content == "Build the authentication system" && msg.Role == llm.RoleUser {
			hasUser = true
			break
		}
	}
	if !hasUser {
		t.Error("user message should survive combined compression + compaction")
	}

	_ = compReport
}

// TestE2E_Compaction_MultipleCyclesWithAccumulatingMessages simulates
// multiple compaction cycles on the same conversation as messages accumulate.
func TestE2E_Compaction_MultipleCyclesWithAccumulatingMessages(t *testing.T) {
	conv := NewConversation()
	conv.AddUserMessage("Implement the full API")

	for cycle := range 3 {
		// Add more messages each cycle
		for i := range 10 {
			conv.AddAssistantMessage("cycle " + string(rune('0'+cycle)) +
				" step " + string(rune('0'+i)) +
				" " + strings.Repeat("working ", 15))
		}

		candidates, _ := conv.GetCompactionCandidates(0.4)
		if len(candidates) > 0 {
			conv.RemoveCompactedMessages(candidates)
		}
	}

	// User message should survive all cycles
	msgs := conv.GetMessages()
	hasUser := false
	for _, msg := range msgs {
		if msg.Content == "Implement the full API" && msg.Role == llm.RoleUser {
			hasUser = true
			break
		}
	}
	if !hasUser {
		t.Error("user message should survive multiple compaction cycles")
	}

	// Conversation should not be empty
	if len(msgs) == 0 {
		t.Error("conversation should not be empty after multiple cycles")
	}
}

// TestE2E_Compaction_RemoveWithEmptyIndices verifies RemoveCompactedMessages
// handles edge cases gracefully.
func TestE2E_Compaction_RemoveWithEmptyIndices(t *testing.T) {
	conv := NewConversation()
	conv.AddUserMessage("Hello")
	conv.AddAssistantMessage("Hi")

	tokensSaved := conv.RemoveCompactedMessages(nil)
	if tokensSaved != 0 {
		t.Errorf("expected 0 tokensSaved for nil indices, got %d", tokensSaved)
	}

	tokensSaved = conv.RemoveCompactedMessages([]int{})
	if tokensSaved != 0 {
		t.Errorf("expected 0 tokensSaved for empty indices, got %d", tokensSaved)
	}

	// Messages should be unchanged
	if conv.Len() != 2 {
		t.Errorf("expected 2 messages, got %d", conv.Len())
	}
}

// TestE2E_Compaction_RemoveWithInvalidIndices verifies that invalid indices
// are safely ignored without panicking.
func TestE2E_Compaction_RemoveWithInvalidIndices(t *testing.T) {
	conv := NewConversation()
	conv.AddUserMessage("Hello")
	conv.AddAssistantMessage("Hi")

	// Negative index should be ignored
	tokensSaved := conv.RemoveCompactedMessages([]int{-1, -5})
	if tokensSaved != 0 {
		t.Errorf("expected 0 tokensSaved for negative indices, got %d", tokensSaved)
	}

	// Out-of-range index should be ignored
	tokensSaved = conv.RemoveCompactedMessages([]int{100, 999})
	if tokensSaved != 0 {
		t.Errorf("expected 0 tokensSaved for out-of-range indices, got %d", tokensSaved)
	}

	if conv.Len() != 2 {
		t.Errorf("expected 2 messages after invalid removal, got %d", conv.Len())
	}
}

// TestE2E_Compaction_RestoreFromMessagesAfterCompaction verifies that a
// conversation can be restored from saved messages after compaction, and
// that the restored state is consistent.
func TestE2E_Compaction_RestoreFromMessagesAfterCompaction(t *testing.T) {
	conv := NewConversation()
	conv.AddUserMessage("Build feature X")
	for range 10 {
		conv.AddAssistantMessage("working " + strings.Repeat("step ", 20))
	}
	conv.AddAssistantMessage("Feature X is complete.")

	// Compact
	candidates, _ := conv.GetCompactionCandidates(0.4)
	if len(candidates) > 0 {
		conv.RemoveCompactedMessages(candidates)
	}

	// Save messages
	savedMsgs := conv.GetMessages()

	// Restore into a new conversation
	conv2 := NewConversation()
	conv2.RestoreFromMessages(savedMsgs)

	// Verify restored conversation has same messages
	restoredMsgs := conv2.GetMessages()
	if len(restoredMsgs) != len(savedMsgs) {
		t.Errorf("restored message count mismatch: got %d, want %d",
			len(restoredMsgs), len(savedMsgs))
	}

	// Verify user message survived round-trip
	hasUser := false
	for _, msg := range restoredMsgs {
		if msg.Content == "Build feature X" && msg.Role == llm.RoleUser {
			hasUser = true
			break
		}
	}
	if !hasUser {
		t.Error("user message should survive restore after compaction")
	}
}

// TestE2E_Compaction_CandidatesSortedByImportance verifies that the
// compaction candidates are identified by importance (lowest first).
func TestE2E_Compaction_CandidatesSortedByImportance(t *testing.T) {
	conv := NewConversation()

	// User message (ImportanceCritical - never removed)
	conv.AddUserMessage("Do the task")

	// Reasoning messages (ImportanceLow - removed first)
	for i := range 10 {
		conv.AddAssistantMessage("let me think " + strings.Repeat("analysis ", 20) + string(rune('A'+i)))
	}

	// Conclusion message (ImportanceHigh)
	conv.AddAssistantMessage("In conclusion, the task is complete.")

	candidates, report := conv.GetCompactionCandidates(0.3)
	if len(candidates) == 0 {
		t.Fatal("expected compaction candidates")
	}

	// All candidates should be reasoning messages (indices 1-10),
	// not the user message (index 0) or conclusion (index 11)
	for _, idx := range candidates {
		if idx == 0 {
			t.Error("user message (index 0) should not be a compaction candidate")
		}
		if idx == 11 {
			t.Error("conclusion message (index 11) should not be a compaction candidate (it's ImportanceHigh)")
		}
	}

	_ = report
}

// TestE2E_Compaction_AnchorMessagesNeverCompacted verifies that anchor
// messages are never identified as compaction candidates.
func TestE2E_Compaction_AnchorMessagesNeverCompacted(t *testing.T) {
	conv := NewConversation()

	// Add an anchor message
	conv.AddAnchorMessage(llm.RoleSystem, "CRITICAL: Always validate user input before processing")

	conv.AddUserMessage("Process some data")
	for i := range 15 {
		conv.AddAssistantMessage("processing " + strings.Repeat("data ", 30) + string(rune('A'+i)))
	}

	candidates, _ := conv.GetCompactionCandidates(0.2)
	for _, idx := range candidates {
		msgs := conv.GetMessages()
		if idx < len(msgs) && msgs[idx].Content == "CRITICAL: Always validate user input before processing" {
			t.Error("anchor message should never be a compaction candidate")
		}
	}

	// Remove candidates and verify anchor survives
	if len(candidates) > 0 {
		conv.RemoveCompactedMessages(candidates)
	}

	msgs := conv.GetMessages()
	hasAnchor := false
	for _, msg := range msgs {
		if msg.Content == "CRITICAL: Always validate user input before processing" {
			hasAnchor = true
			break
		}
	}
	if !hasAnchor {
		t.Error("anchor message should survive compaction")
	}
}

// TestE2E_Compaction_ClonePreservesCompactionState verifies that cloning
// a compacted conversation preserves the remaining messages.
func TestE2E_Compaction_ClonePreservesCompactionState(t *testing.T) {
	conv := NewConversation()
	conv.AddUserMessage("Original task")
	for range 10 {
		conv.AddAssistantMessage("step " + strings.Repeat("work ", 20))
	}

	candidates, _ := conv.GetCompactionCandidates(0.4)
	if len(candidates) > 0 {
		conv.RemoveCompactedMessages(candidates)
	}

	// Clone the conversation
	clone := conv.Clone()
	if clone == nil {
		t.Fatal("Clone returned nil")
	}

	// Verify clone has same messages
	origMsgs := conv.GetMessages()
	cloneMsgs := clone.GetMessages()
	if len(origMsgs) != len(cloneMsgs) {
		t.Errorf("clone message count mismatch: orig=%d clone=%d",
			len(origMsgs), len(cloneMsgs))
	}
}

// TestE2E_Compaction_TokenTrackingAccuracy verifies that the token counts
// reported by GetCompactionCandidates are consistent with actual removal.
func TestE2E_Compaction_TokenTrackingAccuracy(t *testing.T) {
	conv := NewConversation()

	// Add messages with known sizes
	conv.AddUserMessage(strings.Repeat("a", 300)) // 100 tokens
	for range 5 {
		conv.AddAssistantMessage(strings.Repeat("b", 300)) // 100 tokens each
	}

	candidates, report := conv.GetCompactionCandidates(0.4)

	// Verify tokens are tracked
	if report.TokensBefore == 0 {
		t.Error("TokensBefore should not be zero")
	}

	if len(candidates) > 0 {
		tokensSaved := conv.RemoveCompactedMessages(candidates)
		if tokensSaved <= 0 {
			t.Error("expected positive tokensSaved")
		}
		// tokensSaved should be approximately equal to report.TokensRemoved
		diff := tokensSaved - report.TokensRemoved
		if diff < 0 {
			diff = -diff
		}
		// Allow some variance since both calculate independently
		if diff > report.TokensBefore/2 {
			t.Errorf("large discrepancy between tokensSaved (%d) and TokensRemoved (%d)",
				tokensSaved, report.TokensRemoved)
		}
	}
}
