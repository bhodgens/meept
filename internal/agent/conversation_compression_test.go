package agent

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

func TestCompressByImportance_RemovesLowImportanceFirst(t *testing.T) {
	conv := NewConversation()

	// Add a user message (ImportanceCritical)
	conv.AddUserMessage("What is the weather?")

	// Add reasoning steps (ImportanceLow) - make them large to be removal candidates
	for i := 0; i < 5; i++ {
		conv.AddAssistantMessage("let me think about this carefully. Hmm, considering the data and analyzing the situation thoroughly. This suggests that we need to explore more options.")
	}

	// Add a conclusion (ImportanceHigh)
	conv.AddAssistantMessage("In conclusion, the weather is sunny.")

	messagesBefore := conv.Len()
	report := conv.CompressByImportance(0.5)

	if report.MessagesBefore != messagesBefore {
		t.Errorf("MessagesBefore = %d, want %d", report.MessagesBefore, messagesBefore)
	}

	if report.MessagesAfter >= messagesBefore {
		t.Errorf("MessagesAfter = %d, should be less than MessagesBefore = %d", report.MessagesAfter, messagesBefore)
	}

	if report.TokensRemoved <= 0 {
		t.Error("TokensRemoved should be positive when compression occurs")
	}

	if report.TokensAfter >= report.TokensBefore {
		t.Errorf("TokensAfter (%d) should be less than TokensBefore (%d)", report.TokensAfter, report.TokensBefore)
	}
}

func TestCompressByImportance_PreservesAnchorMessages(t *testing.T) {
	conv := NewConversation()

	// Add anchor message
	conv.AddAnchorMessage(llm.RoleSystem, "Critical validation instruction that must never be removed")

	// Add regular messages
	conv.AddUserMessage("Do something")
	for i := 0; i < 10; i++ {
		conv.AddAssistantMessage("let me think about step " + strings.Repeat("x", 100))
	}
	conv.AddAssistantMessage("Done with the task.")

	// Compress aggressively to 10% of tokens
	report := conv.CompressByImportance(0.1)

	// Check that anchor message content is still present
	messages := conv.GetMessages()
	hasAnchor := false
	for _, msg := range messages {
		if msg.Content == "Critical validation instruction that must never be removed" {
			hasAnchor = true
			break
		}
	}

	if !hasAnchor {
		t.Error("anchor message should be preserved after aggressive compression")
	}

	if report.MessagesAfter == 0 {
		t.Error("should retain at least the anchor message")
	}
}

func TestCompressByImportance_PreservesUserInput(t *testing.T) {
	conv := NewConversation()

	userContent := "This is the original user request"
	conv.AddUserMessage(userContent)

	// Add many low-importance reasoning messages
	for i := 0; i < 20; i++ {
		conv.AddAssistantMessage("let me think about this. Hmm, considering the options. " + strings.Repeat("data ", 20))
	}

	// Compress aggressively
	report := conv.CompressByImportance(0.1)

	// User message should still be present
	messages := conv.GetMessages()
	hasUser := false
	for _, msg := range messages {
		if msg.Content == userContent && msg.Role == llm.RoleUser {
			hasUser = true
			break
		}
	}

	if !hasUser {
		t.Error("user input message should be preserved after compression")
	}

	_ = report // just verifying no panic
}

func TestCompressByImportance_ReportAccuracy(t *testing.T) {
	conv := NewConversation()

	// Add messages with known token counts
	// 3 chars per token heuristic
	conv.AddUserMessage("1234567890")          // 10 chars = 3 tokens
	conv.AddAssistantMessage("123456789012345") // 15 chars = 5 tokens
	conv.AddAssistantMessage("123456789012345") // 15 chars = 5 tokens
	conv.AddAssistantMessage("123456789012345") // 15 chars = 5 tokens

	messagesBefore := conv.Len()
	report := conv.CompressByImportance(0.5)

	if report.MessagesBefore != messagesBefore {
		t.Errorf("MessagesBefore = %d, want %d", report.MessagesBefore, messagesBefore)
	}

	if report.TokensBefore <= 0 {
		t.Errorf("TokensBefore = %d, should be positive", report.TokensBefore)
	}

	// TokensBefore + TokensRemoved should not exceed TokensBefore (sanity)
	if report.TokensRemoved > report.TokensBefore {
		t.Errorf("TokensRemoved (%d) > TokensBefore (%d)", report.TokensRemoved, report.TokensBefore)
	}

	// TokensAfter should equal TokensBefore - TokensRemoved
	expectedAfter := report.TokensBefore - report.TokensRemoved
	if report.TokensAfter != expectedAfter {
		t.Errorf("TokensAfter = %d, want %d (TokensBefore - TokensRemoved)", report.TokensAfter, expectedAfter)
	}

	// MessagesAfter should be <= MessagesBefore
	if report.MessagesAfter > report.MessagesBefore {
		t.Errorf("MessagesAfter (%d) > MessagesBefore (%d)", report.MessagesAfter, report.MessagesBefore)
	}
}

func TestCompressByImportance_EmptyConversation(t *testing.T) {
	conv := NewConversation()

	report := conv.CompressByImportance(0.5)

	if report.MessagesBefore != 0 {
		t.Errorf("MessagesBefore = %d, want 0", report.MessagesBefore)
	}
	if report.MessagesAfter != 0 {
		t.Errorf("MessagesAfter = %d, want 0", report.MessagesAfter)
	}
	if report.TokensBefore != 0 {
		t.Errorf("TokensBefore = %d, want 0", report.TokensBefore)
	}
	if report.TokensAfter != 0 {
		t.Errorf("TokensAfter = %d, want 0", report.TokensAfter)
	}
	if report.TokensRemoved != 0 {
		t.Errorf("TokensRemoved = %d, want 0", report.TokensRemoved)
	}
}

func TestCompressByImportance_NoCompressionNeeded(t *testing.T) {
	conv := NewConversation()

	conv.AddUserMessage("Hello")
	conv.AddAssistantMessage("Hi there")

	report := conv.CompressByImportance(1.0)

	// At 1.0 ratio, no compression should occur
	if report.TokensRemoved != 0 {
		t.Errorf("TokensRemoved = %d, want 0 when targetRatio=1.0", report.TokensRemoved)
	}
	if report.MessagesAfter != report.MessagesBefore {
		t.Errorf("MessagesAfter (%d) != MessagesBefore (%d) when no compression needed", report.MessagesAfter, report.MessagesBefore)
	}
}

func TestCompressByImportance_HighImportancePreservedOverLow(t *testing.T) {
	conv := NewConversation()

	// Add conclusion message (ImportanceHigh) - small
	conclusionContent := "In summary, the answer is 42."
	conv.AddAssistantMessage(conclusionContent)

	// Add many reasoning messages (ImportanceLow) - larger
	for i := 0; i < 10; i++ {
		conv.AddAssistantMessage("let me think about this step carefully. Hmm, " + strings.Repeat("analysis ", 30))
	}

	report := conv.CompressByImportance(0.3)

	// The conclusion should survive since reasoning messages are ImportanceLow
	// and have more tokens
	messages := conv.GetMessages()
	hasConclusion := false
	for _, msg := range messages {
		if msg.Content == conclusionContent {
			hasConclusion = true
			break
		}
	}

	if !hasConclusion && report.MessagesAfter > 0 {
		t.Error("conclusion message (ImportanceHigh) should be preserved over reasoning messages (ImportanceLow)")
	}
}

func TestCompressByImportance_WithToolCalls(t *testing.T) {
	conv := NewConversation()

	conv.AddUserMessage("Run the tool")

	// Add messages with tool calls
	toolCalls := []llm.ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "test_tool",
				Arguments: `{"param": "value"}`,
			},
		},
	}
	conv.AddAssistantMessageWithToolCalls("let me think", toolCalls)
	conv.AddToolResult("call_1", `{"result": "success"}`)

	// This should not panic even with tool calls
	report := conv.CompressByImportance(0.5)

	// Basic sanity: report fields should be populated
	if report.MessagesBefore < 3 {
		t.Errorf("MessagesBefore = %d, expected at least 3", report.MessagesBefore)
	}
}
