package session

import (
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// AssembleBranch converts session Messages into llm.ChatMessages for the agent loop.
// It walks the message list (already ordered by GetMessagePath) and builds ChatMessages.
// Compaction entries are included as system messages with a "[Compacted Context]" prefix,
// providing summarized context for messages that have been removed.
// Summary entries (from abandoned sibling branches) are included as system messages with
// a "[Branch Summary]" prefix.
// toolCallsMap maps message_id -> []ToolCall for the messages being assembled.
func AssembleBranch(messages []Message, toolCallsMap map[int64][]ToolCall) []llm.ChatMessage {
	var result []llm.ChatMessage
	for _, msg := range messages {
		switch msg.EntryType {
		case "compaction":
			// Compaction entries already have the "[Compacted Context]" prefix from the compactor.
			result = append(result, llm.ChatMessage{
				Role:    llm.RoleSystem,
				Content: msg.Content,
			})
		case "summary":
			// Include summary entries from sibling branches
			result = append(result, llm.ChatMessage{
				Role:    llm.RoleSystem,
				Content: "[Branch Summary] " + msg.Content,
			})
		case "message", "branch_point", "":
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

// ConvertChatMessagesToSessionMessages converts llm.ChatMessages to session Messages for persistence.
// Skips system prompt messages since those are managed by the Conversation separately.
func ConvertChatMessagesToSessionMessages(conversationID string, messages []llm.ChatMessage) []Message {
	var result []Message
	now := time.Now()
	for i, msg := range messages {
		// Skip system prompt messages - they are managed by Conversation.systemPrompt
		if msg.Role == llm.RoleSystem {
			continue
		}
		sm := Message{
			SessionID:  conversationID,
			Role:       string(msg.Role),
			Content:    msg.Content,
			Timestamp:  now.Add(time.Duration(i) * time.Microsecond), // Ensure ordering
			EntryType:  "message",
			BranchID:   "main",
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
		}
		result = append(result, sm)
	}
	return result
}

// ChatMessagesToToolCalls extracts tool calls from ChatMessages into ToolCall records.
// It pairs messages with their source session.Messages by index alignment.
// Returns tool calls keyed by the index of the corresponding session.Message.
// The seq parameter within each ToolCall is set based on position within each message.
func ChatMessagesToToolCalls(messages []Message, chatMsgs []llm.ChatMessage) map[int][]ToolCall {
	result := make(map[int][]ToolCall)

	// Walk chat messages and session messages in parallel.
	// Session messages exclude system prompts, chat messages include them,
	// so we track a separate chatMsg index.
	chatIdx := 0
	for sessIdx, msg := range messages {
		// Skip past system messages in chatMsgs to find the matching chat message
		for chatIdx < len(chatMsgs) && chatMsgs[chatIdx].Role == llm.RoleSystem {
			chatIdx++
		}
		if chatIdx >= len(chatMsgs) {
			break
		}

		cm := chatMsgs[chatIdx]
		if len(cm.ToolCalls) > 0 {
			tcs := make([]ToolCall, len(cm.ToolCalls))
			for j, tc := range cm.ToolCalls {
				tcs[j] = ToolCall{
					MessageID:  msg.ID,
					ToolName:   tc.Function.Name,
					ToolCallID: tc.ID,
					Arguments:  tc.Function.Arguments,
					Seq:        j,
				}
			}
			result[sessIdx] = tcs
		}
		chatIdx++
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
		return nil, nil
	}
	return store.GetToolCallsForMessages(ids)
}

// RestoreConversationFromStore restores conversation messages from a session store.
// It looks up the session for the given conversationID, retrieves the message path
// (either via leaf pointer or flat list), loads tool calls, and assembles the branch.
// Returns the assembled ChatMessages and the session, or an error.
func RestoreConversationFromStore(store Store, conversationID string, messageLimit int) ([]llm.ChatMessage, *Session, error) {
	sess := store.GetByConversationID(conversationID)
	if sess == nil {
		return nil, nil, fmt.Errorf("no session found for conversation %s", conversationID)
	}

	var messages []Message
	var err error

	leafID, leafErr := store.GetLeafMessageID(sess.ID)
	if leafErr != nil {
		return nil, nil, fmt.Errorf("failed to get leaf message ID: %w", leafErr)
	}

	if leafID > 0 {
		messages, err = store.GetMessagePath(sess.ID, leafID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get message path: %w", err)
		}
	} else {
		// No leaf pointer, get all messages (use large limit, then truncate)
		messages, err = store.GetMessages(sess.ID, 0, 10000)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get messages: %w", err)
		}
	}

	// Apply message limit if configured
	if messageLimit > 0 && len(messages) > messageLimit {
		messages = messages[len(messages)-messageLimit:]
	}

	// Load tool calls
	toolCallsMap, err := LoadToolCallsForMessages(store, messages)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load tool calls: %w", err)
	}

	// Assemble into ChatMessages
	chatMsgs := AssembleBranch(messages, toolCallsMap)
	return chatMsgs, sess, nil
}
