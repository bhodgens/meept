package session

import (
	"encoding/json"

	"github.com/caimlas/meept/internal/llm"
)

// AssembleBranch converts session Messages into llm.ChatMessages for the agent loop.
// It walks the message list (already ordered by GetMessagePath) and builds ChatMessages.
// Compaction entries are included as system messages with a "[Compacted Context]" prefix,
// providing summarized context for messages that have been removed. Messages whose IDs
// appear in a compaction entry's compressed_ids list are skipped.
// Summary entries (from abandoned sibling branches) are included as system messages with
// a "[Branch Summary]" prefix.
// toolCallsMap maps message_id -> []ToolCall for the messages being assembled.
func AssembleBranch(messages []Message, toolCallsMap map[int64][]ToolCall) []llm.ChatMessage {
	// First pass: collect all compressed IDs from compaction entries so we can
	// skip those messages when assembling.
	compressedIDs := make(map[int64]bool)
	for _, msg := range messages {
		if msg.EntryType == "compaction" {
			var content CompactionContent
			if err := json.Unmarshal([]byte(msg.Content), &content); err == nil {
				for _, id := range content.CompressedIDs {
					compressedIDs[id] = true
				}
			}
		}
	}

	var result []llm.ChatMessage
	for _, msg := range messages {
		// Skip messages that have been replaced by a compaction entry.
		if compressedIDs[msg.ID] {
			continue
		}

		switch msg.EntryType {
		case "compaction":
			// Compaction entries may store JSON content (from InsertCompaction)
			// or plain text. Try JSON first; fall back to raw content.
			content := msg.Content
			var cc CompactionContent
			if err := json.Unmarshal([]byte(msg.Content), &cc); err == nil && cc.Summary != "" {
				content = "[Compacted Context] " + cc.Summary
			} else if !isCompactedPrefix(content) {
				content = "[Compacted Context] " + content
			}
			result = append(result, llm.ChatMessage{
				Role:    llm.RoleSystem,
				Content: content,
			})
		case "summary":
			// Include summary entries from sibling branches
			result = append(result, llm.ChatMessage{
				Role:    llm.RoleSystem,
				Content: "[Branch Summary] " + msg.Content,
			})
		case KeyMessage, "branch_point", "":
			// Regular messages (and branch points, which behave like messages in context)
			chatMsg := llm.ChatMessage{
				Role:    llm.Role(msg.Role),
				Content: msg.Content,
			}
			if msg.Name != "" {
				chatMsg.Name = msg.Name
			}
			if msg.ToolCallID != "" {
				chatMsg.ToolCallID = msg.ToolCallID
			}
			// Attach tool calls if present
			if tc, ok := toolCallsMap[msg.ID]; ok && len(tc) > 0 {
				chatMsg.ToolCalls = convertToolCalls(tc)
			}
			result = append(result, chatMsg)
		}
	}
	return result
}

// isCompactedPrefix returns true if the content already has a compaction prefix.
func isCompactedPrefix(content string) bool {
	return len(content) >= 19 && content[:19] == "[Compacted Context]"
}

// convertToolCalls converts session.ToolCall records to llm.ToolCall format.
func convertToolCalls(toolCalls []ToolCall) []llm.ToolCall {
	result := make([]llm.ToolCall, len(toolCalls))
	for i, tc := range toolCalls {
		result[i] = llm.ToolCall{
			ID:   tc.ToolCallID,
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      tc.ToolName,
				Arguments: tc.Arguments,
			},
		}
	}
	return result
}

// LoadToolCallsForMessages loads tool calls for a set of messages from the store.
// Returns a map from message ID to the slice of tool calls for that message.
func LoadToolCallsForMessages(store Store, messages []Message) (map[int64][]ToolCall, error) {
	ids := make([]int64, 0, len(messages))
	for _, m := range messages {
		ids = append(ids, m.ID)
	}
	if len(ids) == 0 {
		return nil, nil //nolint:nilnil // empty message set has no tool calls; callers handle nil map
	}
	return store.GetToolCallsForMessages(ids)
}
