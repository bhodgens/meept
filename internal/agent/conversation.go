// Package agent provides the main reasoning/action loop for the Meept agent.
package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/llm"
)

const (
	// DefaultMaxMessages is the default maximum number of messages per conversation.
	DefaultMaxMessages = 200
	// DefaultContextLimit is the default context token limit for truncation.
	DefaultContextLimit = 100000
)

const (
	// MaxMemoryContextTokens is the maximum tokens allowed for memory context injection.
	MaxMemoryContextTokens = 2000
)

// TurnBudgetTracker tracks token usage across multiple conversation turns.
// This enables multi-turn budget allocation and graceful wrap-up when depleted.
type TurnBudgetTracker struct {
	mu              sync.Mutex
	totalBudget     int  // Total tokens allocated for the session
	usedBudget      int  // Tokens used so far
	tokensPerTurn   int  // Expected tokens per turn (for estimation)
	maxTurns        int  // Maximum turns before wrap-up
	currentTurn     int  // Current turn number
	warningZone     bool // Set when budget is nearly depleted
	wrapUpRequested bool // Set when wrap-up is requested
}

// NewTurnBudgetTracker creates a new budget tracker.
func NewTurnBudgetTracker(totalBudget, tokensPerTurn, maxTurns int) *TurnBudgetTracker {
	return &TurnBudgetTracker{
		totalBudget:   totalBudget,
		tokensPerTurn: tokensPerTurn,
		maxTurns:      maxTurns,
	}
}

// RecordUsage records token usage for the current turn.
func (t *TurnBudgetTracker) RecordUsage(tokensUsed int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.usedBudget += tokensUsed
	t.currentTurn++

	// Check if entering warning zone (80% depleted)
	remainingRatio := float64(t.totalBudget-t.usedBudget) / float64(t.totalBudget)
	if remainingRatio < 0.2 {
		t.warningZone = true
	}

	// Check if max turns reached
	if t.currentTurn >= t.maxTurns {
		t.wrapUpRequested = true
	}
}

// RemainingBudget returns tokens remaining in the budget.
func (t *TurnBudgetTracker) RemainingBudget() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	remaining := t.totalBudget - t.usedBudget
	if remaining < 0 {
		return 0
	}
	return remaining
}

// AvailableBudgetForTurn returns the budget available for the current turn.
func (t *TurnBudgetTracker) AvailableBudgetForTurn() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	remaining := t.totalBudget - t.usedBudget
	remainingTurns := t.maxTurns - t.currentTurn
	if remainingTurns <= 0 {
		// Wrap-up turn: use all remaining budget
		return remaining
	}
	// Allocate remaining budget across remaining turns
	perTurn := remaining / remainingTurns
	if perTurn < 1000 {
		return 1000 // minimum budget
	}
	if perTurn > t.tokensPerTurn {
		return t.tokensPerTurn // cap at expected per-turn usage
	}
	return perTurn
}

// IsWarningZone returns true if budget is nearly depleted (80%+ used).
func (t *TurnBudgetTracker) IsWarningZone() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.warningZone
}

// IsWrapUpRequested returns true if wrap-up is requested due to budget exhaustion.
func (t *TurnBudgetTracker) IsWrapUpRequested() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.wrapUpRequested
}

// GetTurnInfo returns current turn, max turns, and budget status.
func (t *TurnBudgetTracker) GetTurnInfo() (current, maxTurns, used, total int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.currentTurn, t.maxTurns, t.usedBudget, t.totalBudget
}

// MessageClassification classifies the semantic type of a message for importance-based retention.
type MessageClassification int

const (
	// MessageUnknown is the default for unclassified messages.
	MessageUnknown MessageClassification = iota
	// MessageUserInput is the original user request or follow-up questions.
	MessageUserInput
	// MessageAssistantPlan is assistant output containing plans, task decomposition, or step-by-step thinking.
	MessageAssistantPlan
	// MessageAssistantConclusion is assistant output with final answers, summaries, or conclusions.
	MessageAssistantConclusion
	// MessageToolResult is the output from tool execution.
	MessageToolResult
	// MessageToolResultKey is a tool result containing key findings (e.g., file contents, search results).
	MessageToolResultKey
	// MessageReasoningStep is intermediate reasoning or exploration (lowest priority).
	MessageReasoningStep
)

// MessageImportance is the priority level for message retention during truncation.
// Higher values = more important = retained longer.
type MessageImportance int

const (
	ImportanceLow MessageImportance = iota
	ImportanceMedium
	ImportanceHigh
	ImportanceCritical
)

// Conversation manages chat message history with LRU eviction and truncation.
type Conversation struct {
	mu           sync.RWMutex
	messages     []llm.ChatMessage
	messageTypes []MessageClassification // Parallel array tracking semantic type of each message
	systemPrompt string
	maxMessages  int
	contextLimit int

	// Prefix cache optimization (Hermes pattern)
	// memoryContext is live-updated memory context (deprecated for prompt building)
	memoryContext string
	// memorySnapshot is frozen at session start for API prefix caching
	memorySnapshot string
	// cachePrefixHash tracks SHA256 of system prompt + stabilized tools for cache invalidation.
	cachePrefixHash string
	// cachePrefixChanged is set to true when StabilizeToolPrefix detects a hash change.
	cachePrefixChanged bool

	// Anchor messages are exempt from truncation (validation instructions, escalation triggers)
	anchorMessages map[string]bool // message content hash -> isAnchor
}

// ConversationOption is a functional option for configuring a Conversation.
type ConversationOption func(*Conversation)

// WithMaxMessages sets the maximum number of messages before truncation.
func WithMaxMessages(maxMessages int) ConversationOption {
	return func(c *Conversation) {
		c.maxMessages = maxMessages
	}
}

// WithContextLimit sets the context token limit for truncation.
func WithContextLimit(limit int) ConversationOption {
	return func(c *Conversation) {
		c.contextLimit = limit
	}
}

// WithSystemPrompt sets the system prompt for the conversation.
func WithSystemPrompt(prompt string) ConversationOption {
	return func(c *Conversation) {
		c.systemPrompt = prompt
	}
}

// NewConversation creates a new conversation with optional configuration.
func NewConversation(opts ...ConversationOption) *Conversation {
	c := &Conversation{
		messages:     make([]llm.ChatMessage, 0, 32),
		messageTypes: make([]MessageClassification, 0, 32),
		maxMessages:  DefaultMaxMessages,
		contextLimit: DefaultContextLimit,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// AddAnchorMessage adds a message that is exempt from truncation.
// Anchor messages are treated as ImportanceCritical and preserved during context management.
// This is used for validation instructions, escalation triggers, and other critical context.
func (c *Conversation) AddAnchorMessage(role llm.Role, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Add the message
	c.messages = append(c.messages, llm.ChatMessage{
		Role:    role,
		Content: content,
	})

	// Mark as anchor
	if c.anchorMessages == nil {
		c.anchorMessages = make(map[string]bool)
	}
	hash := c.hashContent(content)
	c.anchorMessages[hash] = true
}

// IsAnchorMessage checks if a message content is anchored.
func (c *Conversation) IsAnchorMessage(content string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.anchorMessages == nil {
		return false
	}
	return c.anchorMessages[c.hashContent(content)]
}

// ClearAnchors removes all anchor markings (used when context is reset).
func (c *Conversation) ClearAnchors() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.anchorMessages = nil
}

// hashContent creates a simple hash of content for anchor tracking.
func (c *Conversation) hashContent(content string) string {
	if len(content) > 200 {
		return content[:200] // Use prefix as key for long content
	}
	return content
}

// classifyMessageClassification determines the semantic type of a message based on role and content.
func classifyMessageClassification(msg llm.ChatMessage, _ bool) MessageClassification {
	switch msg.Role {
	case llm.RoleUser:
		return MessageUserInput
	case llm.RoleTool:
		// Check if this looks like a key finding
		content := msg.Content
		if isKeyFindingContent(content) {
			return MessageToolResultKey
		}
		return MessageToolResult
	case llm.RoleAssistant:
		// Classify based on content patterns
		content := msg.Content
		if isConclusionContent(content) {
			return MessageAssistantConclusion
		}
		if isPlanContent(content) {
			return MessageAssistantPlan
		}
		if isReasoningContent(content) {
			return MessageReasoningStep
		}
		// Default to conclusion for general assistant responses
		return MessageAssistantConclusion
	case llm.RoleSystem:
		return MessageUnknown
	default:
		return MessageUnknown
	}
}

// isKeyFindingContent checks if content looks like key findings from tool execution.
func isKeyFindingContent(content string) bool {
	lower := strings.ToLower(content)
	// Key findings often contain file contents, search results, or structured data
	keyIndicators := []string{
		"file:", "path:", "result:", "found:", "matches:",
		"```", "{", "[", // Code/JSON/arrays
		"package ", "func ", "type ", // Go code
		"import ", "export ", "class ", // Other languages
	}
	for _, indicator := range keyIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

// isConclusionContent checks if assistant output is a conclusion or summary.
func isConclusionContent(content string) bool {
	lower := strings.ToLower(content)
	conclusionIndicators := []string{
		"in conclusion", "in summary", "to summarize",
		"final answer", "final result", "final code",
		"completed", "finished", "done",
		"here's the", "here is the", "here's how",
		"summary:", "answer:", "solution:",
	}
	for _, indicator := range conclusionIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

// isPlanContent checks if assistant output is a plan or task decomposition.
func isPlanContent(content string) bool {
	lower := strings.ToLower(content)
	planIndicators := []string{
		"plan:", "step 1", "step 2", "step 3",
		"first,", "second,", "third,", "finally,",
		"1.", "2.", "3.", // Numbered list
		"- [ ]", "- [x]", // Task list
		"we need to", "we should", "i will",
		"approach:", "strategy:", "breakdown:",
	}
	for _, indicator := range planIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

// isReasoningContent checks if assistant output is intermediate reasoning.
func isReasoningContent(content string) bool {
	lower := strings.ToLower(content)
	reasoningIndicators := []string{
		"let me think", "let's see", "hmm",
		"considering", "analyzing", "exploring",
		"note:", "observation:", "interesting",
		"this suggests", "this means", "this implies",
		"wait,", "actually,", "on second thought",
	}
	for _, indicator := range reasoningIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

// getMessageImportance returns the importance level for a message type.
func getMessageImportance(msgType MessageClassification) MessageImportance {
	switch msgType {
	case MessageUserInput:
		return ImportanceCritical
	case MessageAssistantConclusion:
		return ImportanceHigh
	case MessageToolResultKey:
		return ImportanceHigh
	case MessageAssistantPlan:
		return ImportanceMedium
	case MessageToolResult:
		return ImportanceMedium
	case MessageReasoningStep:
		return ImportanceLow
	default:
		return ImportanceLow
	}
}

// AddMessage appends a message to the conversation history.
func (c *Conversation) AddMessage(msg llm.ChatMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.messages = append(c.messages, msg)

	// Classify and track message type for importance-based retention
	isFirstUserMsg := len(c.messages) == 1 && msg.Role == llm.RoleUser
	msgType := classifyMessageClassification(msg, isFirstUserMsg)
	c.messageTypes = append(c.messageTypes, msgType)
}

// AddUserMessage is a convenience method to add a user message.
func (c *Conversation) AddUserMessage(content string) {
	c.AddMessage(llm.ChatMessage{
		Role:    llm.RoleUser,
		Content: content,
	})
}

// AddUserMessageWithParts adds a user message with multimodal content parts.
// When parts is non-empty the LLM serializer uses Parts in place of Content.
// The text content is still stored for FTS indexing, summarization, and
// context compaction fallback.
func (c *Conversation) AddUserMessageWithParts(content string, parts []llm.ContentPart) {
	c.AddMessage(llm.ChatMessage{
		Role:    llm.RoleUser,
		Content: content,
		Parts:   parts,
	})
}

// AddAssistantMessage is a convenience method to add an assistant message.
func (c *Conversation) AddAssistantMessage(content string) {
	c.AddMessage(llm.ChatMessage{
		Role:    llm.RoleAssistant,
		Content: content,
	})
}

// AddAssistantMessageWithToolCalls adds an assistant message with tool calls.
func (c *Conversation) AddAssistantMessageWithToolCalls(content string, toolCalls []llm.ToolCall) {
	c.AddMessage(llm.ChatMessage{
		Role:      llm.RoleAssistant,
		Content:   content,
		ToolCalls: toolCalls,
	})
}

// AddToolResult adds a tool result message.
func (c *Conversation) AddToolResult(toolCallID, content string) {
	c.AddMessage(llm.ChatMessage{
		Role:       llm.RoleTool,
		Content:    content,
		ToolCallID: toolCallID,
	})
}

// AddSystemMessage adds a system message.
func (c *Conversation) AddSystemMessage(content string) {
	c.AddMessage(llm.ChatMessage{
		Role:    llm.RoleSystem,
		Content: content,
	})
}

// GetMessages returns all messages in the conversation.
// The first message is always the system prompt if set.
func (c *Conversation) GetMessages() []llm.ChatMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.buildMessageList()
}

// buildMessageList constructs the message list with system prompt.
// Must be called with at least a read lock held.
func (c *Conversation) buildMessageList() []llm.ChatMessage {
	var result []llm.ChatMessage

	// Prepend system prompt if set
	if c.systemPrompt != "" {
		result = append(result, llm.ChatMessage{
			Role:    llm.RoleSystem,
			Content: c.systemPrompt,
		})
	}

	result = append(result, c.messages...)
	return result
}

// Clear removes all messages except the system prompt.
func (c *Conversation) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.messages = make([]llm.ChatMessage, 0, 32)
	c.messageTypes = make([]MessageClassification, 0, 32)
}

// RestoreFromMessages replaces the conversation's message history with the provided messages.
// Preserves system prompt and other config. Re-classifies all messages for importance tracking.
func (c *Conversation) RestoreFromMessages(messages []llm.ChatMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = make([]llm.ChatMessage, len(messages))
	copy(c.messages, messages)
	c.messageTypes = make([]MessageClassification, len(messages))
	for i, msg := range messages {
		isFirstUserMsg := i == 0 && msg.Role == llm.RoleUser
		c.messageTypes[i] = classifyMessageClassification(msg, isFirstUserMsg)
	}
}

// SetSystemPrompt sets or updates the system prompt.
func (c *Conversation) SetSystemPrompt(prompt string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.systemPrompt = prompt
}

// GetSystemPrompt returns the current system prompt.
func (c *Conversation) GetSystemPrompt() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.systemPrompt
}

// Len returns the number of messages (excluding system prompt).
func (c *Conversation) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.messages)
}

// SetBranchPoint truncates the conversation to the message at the given index.
// All messages after the index are removed. Used when branching back to a prior message.
func (c *Conversation) SetBranchPoint(index int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index < 0 || index >= len(c.messages) {
		return
	}
	c.messages = c.messages[:index+1]
	if len(c.messageTypes) > index+1 {
		c.messageTypes = c.messageTypes[:index+1]
	}
}

// FindMessageByContent finds the index of a message matching the given content prefix.
// Returns -1 if not found.
func (c *Conversation) FindMessageByContent(contentPrefix string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i, msg := range c.messages {
		if len(msg.Content) >= len(contentPrefix) && msg.Content[:len(contentPrefix)] == contentPrefix {
			return i
		}
	}
	return -1
}

// Truncate removes old messages when the conversation exceeds the maximum.
// It preserves the system prompt and keeps the most recent messages.
// This implements LRU-style eviction for conversation history.
func (c *Conversation) Truncate() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.messages) <= c.maxMessages {
		return 0
	}

	// Calculate how many messages to remove
	// Keep the system prompt separate, so we only count regular messages
	keep := max(c.maxMessages, 1)

	removed := len(c.messages) - keep
	if removed <= 0 {
		return 0
	}

	// Keep the most recent messages
	c.messages = c.messages[removed:]
	// Sync messageTypes slice to match messages (AGENT-3 fix)
	if len(c.messageTypes) > 0 {
		c.messageTypes = c.messageTypes[removed:]
	}
	return removed
}

// isAnchorMessageUnsafe checks if a message is anchored without acquiring lock.
// Must only be called when c.mu is already held.
func (c *Conversation) isAnchorMessageUnsafe(content string) bool {
	if c.anchorMessages == nil {
		return false
	}
	return c.anchorMessages[c.hashContent(content)]
}

// TruncateByTokens removes old messages to fit within a token budget.
// It uses a rough estimate of 3 characters per token (appropriate for JSON/code-heavy content).
// It counts both Content and ToolCalls fields for accurate estimation.
// Anchor messages are preserved and never removed.
// Returns the number of messages removed.
func (c *Conversation) TruncateByTokens(tokenBudget int) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if tokenBudget <= 0 || len(c.messages) == 0 {
		return 0
	}

	const charsPerToken = 3

	// Calculate system prompt tokens
	systemTokens := len(c.systemPrompt) / charsPerToken

	// Calculate available token budget for messages
	availableBudget := tokenBudget - systemTokens
	if availableBudget <= 0 {
		// System prompt alone exceeds budget, keep at least last message
		// but preserve anchors. Always keep the last message (which should
		// be the most recent user input) so the LLM has something to respond to.
		newMessages := make([]llm.ChatMessage, 0, 2)
		lastIdx := len(c.messages) - 1
		for i, msg := range c.messages {
			if c.isAnchorMessageUnsafe(msg.Content) || i == lastIdx {
				newMessages = append(newMessages, msg)
			}
		}
		removed := len(c.messages) - len(newMessages)
		c.messages = newMessages
		return removed
	}

	// Build list of messages to keep (from end), skipping anchors
	keepMask := make([]bool, len(c.messages))
	totalTokens := 0

	for i, v := range slices.Backward(c.messages) {
		// Always keep anchor messages
		if c.isAnchorMessageUnsafe(v.Content) {
			keepMask[i] = true
			continue
		}

		msgTokens := len(v.Content) / charsPerToken
		// Count tool calls
		for _, tc := range v.ToolCalls {
			msgTokens += len(tc.Function.Name) / charsPerToken
			msgTokens += len(tc.Function.Arguments) / charsPerToken
			msgTokens += 20
		}
		if v.ToolCallID != "" {
			msgTokens += 15
		}

		if totalTokens+msgTokens > availableBudget {
			break
		}
		totalTokens += msgTokens
		keepMask[i] = true
	}

	// Build new message list and synced types
	newMessages := make([]llm.ChatMessage, 0, len(c.messages))
	newTypes := make([]MessageClassification, 0, len(c.messageTypes))
	removed := 0
	for i, msg := range c.messages {
		if keepMask[i] {
			newMessages = append(newMessages, msg)
			if i < len(c.messageTypes) {
				newTypes = append(newTypes, c.messageTypes[i])
			}
		} else {
			removed++
		}
	}

	c.messages = newMessages
	c.messageTypes = newTypes
	return removed
}

// TruncateByImportance removes messages based on semantic importance rather than recency.
// It preserves messages in priority order:
// 1. System prompt (always)
// 2. Original user message (critical)
// 3. Assistant conclusions/summaries (high)
// 4. Tool results with key findings (high)
// 5. Assistant plans (medium)
// 6. Regular tool results (medium)
// 7. Intermediate reasoning steps (low - removed first)
// Anchor messages are always treated as ImportanceCritical and preserved.
// Returns the number of messages removed.
func (c *Conversation) TruncateByImportance(tokenBudget int) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if tokenBudget <= 0 || len(c.messages) == 0 {
		return 0
	}

	const charsPerToken = 3

	// Calculate system prompt tokens
	systemTokens := len(c.systemPrompt) / charsPerToken

	// Calculate available token budget for messages
	availableBudget := tokenBudget - systemTokens
	if availableBudget <= 0 {
		if len(c.messages) > 1 {
			removed := len(c.messages) - 1
			c.messages = c.messages[removed:]
			c.messageTypes = c.messageTypes[removed:]
			return removed
		}
		return 0
	}

	// Calculate current token usage
	currentTokens := 0
	for _, msg := range c.messages {
		msgTokens := len(msg.Content) / charsPerToken
		for _, tc := range msg.ToolCalls {
			msgTokens += len(tc.Function.Arguments) / charsPerToken
		}
		currentTokens += msgTokens
	}

	if currentTokens <= availableBudget {
		return 0
	}

	type msgIndex struct {
		idx        int
		importance MessageImportance
		tokens     int
	}

	var indices []msgIndex
	for i, msg := range c.messages {
		msgType := MessageUnknown
		if i < len(c.messageTypes) {
			msgType = c.messageTypes[i]
		}
		// Anchor messages are treated as ImportanceCritical
		importance := getMessageImportance(msgType)
		if c.isAnchorMessageUnsafe(msg.Content) {
			importance = ImportanceCritical
		}
		msgTokens := len(msg.Content) / charsPerToken
		indices = append(indices, msgIndex{
			idx:        i,
			importance: importance,
			tokens:     msgTokens,
		})
	}

	// Sort by importance (lowest first), then by token count (highest first)
	for i := range len(indices) - 1 {
		for j := i + 1; j < len(indices); j++ {
			shouldSwap := false
			if indices[i].importance > indices[j].importance {
				shouldSwap = true
			} else if indices[i].importance == indices[j].importance && indices[i].tokens < indices[j].tokens {
				shouldSwap = true
			}
			if shouldSwap {
				indices[i], indices[j] = indices[j], indices[i]
			}
		}
	}

	removedTokens := 0
	for _, mi := range indices {
		if currentTokens-removedTokens <= availableBudget {
			break
		}
		removedTokens += mi.tokens
	}

	// Start with all messages kept, then mark low-importance ones for removal.
	keepMask := make([]bool, len(c.messages))
	for i := range keepMask {
		keepMask[i] = true
	}
	tokensRemoved := 0
	for _, mi := range indices {
		if tokensRemoved >= removedTokens {
			break
		}
		keepMask[mi.idx] = false
		tokensRemoved += mi.tokens
	}

	// Always keep the last few messages
	minKeep := min(4, len(c.messages))
	for i := len(c.messages) - minKeep; i < len(c.messages); i++ {
		if i >= 0 {
			keepMask[i] = true
		}
	}

	newMessages := make([]llm.ChatMessage, 0, len(c.messages))
	newTypes := make([]MessageClassification, 0, len(c.messageTypes))
	removedCount := 0

	for i, msg := range c.messages {
		if i < len(keepMask) && keepMask[i] {
			newMessages = append(newMessages, msg)
			if i < len(c.messageTypes) {
				newTypes = append(newTypes, c.messageTypes[i])
			}
		} else {
			removedCount++
		}
	}

	c.messages = newMessages
	c.messageTypes = newTypes
	return removedCount
}

// GetWindowedMessages returns messages within a token budget with smart context selection.
// It preserves: (1) system prompt always, (2) original user message, (3) anchor messages, (4) most recent messages.
// Anchor messages are always included regardless of token budget.
// Returns messages that fit within the token budget.
func (c *Conversation) GetWindowedMessages(tokenBudget int) []llm.ChatMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if tokenBudget <= 0 {
		return c.buildMessageList()
	}

	const charsPerToken = 3

	// Calculate system prompt tokens
	systemTokens := len(c.systemPrompt) / charsPerToken

	// Reserve tokens for system prompt
	availableBudget := tokenBudget - systemTokens
	if availableBudget <= 0 {
		// System prompt alone exceeds budget, return just system + anchors + last message
		result := make([]llm.ChatMessage, 0, 4)
		if c.systemPrompt != "" {
			result = append(result, llm.ChatMessage{
				Role:    llm.RoleSystem,
				Content: c.systemPrompt,
			})
		}
		// Always include anchor messages
		for _, msg := range c.messages {
			if c.isAnchorMessageUnsafe(msg.Content) {
				result = append(result, msg)
			}
		}
		if len(c.messages) > 0 {
			result = append(result, c.messages[len(c.messages)-1])
		}
		return result
	}

	// Find the original user message (first user message after any system messages)
	var originalUserIdx = -1
	for i, msg := range c.messages {
		if msg.Role == llm.RoleUser {
			originalUserIdx = i
			break
		}
	}

	// Identify anchor message indices
	anchorIndices := make(map[int]bool)
	for i, msg := range c.messages {
		if c.isAnchorMessageUnsafe(msg.Content) {
			anchorIndices[i] = true
		}
	}

	// Calculate anchor token overhead
	anchorTokens := 0
	for i := range anchorIndices {
		anchorTokens += len(c.messages[i].Content) / charsPerToken
	}
	availableBudget -= anchorTokens

	// Build result with system prompt first
	result := make([]llm.ChatMessage, 0, len(c.messages)+1)
	if c.systemPrompt != "" {
		result = append(result, llm.ChatMessage{
			Role:    llm.RoleSystem,
			Content: c.systemPrompt,
		})
	}

	// If we have an original user message, always include it
	if originalUserIdx >= 0 {
		originalUserTokens := len(c.messages[originalUserIdx].Content) / charsPerToken
		availableBudget -= originalUserTokens
	}

	// Add anchor messages first (they're always included)
	for i, msg := range c.messages {
		if anchorIndices[i] {
			result = append(result, msg)
		}
	}

	// Count tokens from the end (most recent) until we exceed remaining budget
	totalTokens := 0
	keepFromIdx := len(c.messages)

	for i, v := range slices.Backward(c.messages) {
		// Skip original user and anchors in this pass
		if i == originalUserIdx || anchorIndices[i] {
			continue
		}

		msgTokens := len(v.Content) / charsPerToken
		// Also count tool calls if present
		for _, tc := range v.ToolCalls {
			msgTokens += len(tc.Function.Arguments) / charsPerToken
		}

		if totalTokens+msgTokens > availableBudget {
			keepFromIdx = i + 1
			break
		}
		totalTokens += msgTokens
		keepFromIdx = i
	}

	// Add original user message if it exists and is before keepFromIdx
	if originalUserIdx >= 0 && originalUserIdx < keepFromIdx {
		result = append(result, c.messages[originalUserIdx])
	}

	// Add remaining messages within budget (excluding anchors already added)
	for i := keepFromIdx; i < len(c.messages); i++ {
		// Skip original user if we already added it
		if i == originalUserIdx && originalUserIdx < keepFromIdx {
			continue
		}
		// Skip anchors already added
		if anchorIndices[i] {
			continue
		}
		result = append(result, c.messages[i])
	}

	return result
}

// Clone creates a deep copy of the conversation.
func (c *Conversation) Clone() *Conversation {
	c.mu.RLock()
	defer c.mu.RUnlock()

	clone := &Conversation{
		messages:     make([]llm.ChatMessage, len(c.messages)),
		messageTypes: make([]MessageClassification, len(c.messageTypes)),
		systemPrompt: c.systemPrompt,
		maxMessages:  c.maxMessages,
		contextLimit: c.contextLimit,
	}

	copy(clone.messages, c.messages)
	copy(clone.messageTypes, c.messageTypes)

	// Deep copy tool calls for each message
	for i, msg := range clone.messages {
		if len(msg.ToolCalls) > 0 {
			clone.messages[i].ToolCalls = make([]llm.ToolCall, len(msg.ToolCalls))
			copy(clone.messages[i].ToolCalls, msg.ToolCalls)
		}
	}

	// Clone anchorMessages map (AGENT-4 fix)
	if c.anchorMessages != nil {
		clone.anchorMessages = make(map[string]bool, len(c.anchorMessages))
		maps.Copy(clone.anchorMessages, c.anchorMessages)
	}

	// Preserve cache-related fields so prefix-cache invalidation and
	// snapshot-based prompt building behave consistently on a cloned
	// conversation (used by branching, pair sessions, and snapshots).
	clone.memoryContext = c.memoryContext
	clone.memorySnapshot = c.memorySnapshot
	clone.cachePrefixHash = c.cachePrefixHash
	clone.cachePrefixChanged = c.cachePrefixChanged

	return clone
}

// LastMessage returns the most recent message, or nil if empty.
func (c *Conversation) LastMessage() *llm.ChatMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.messages) == 0 {
		return nil
	}

	msg := c.messages[len(c.messages)-1]
	return &msg
}

// RemoveLast removes and returns the last message.
func (c *Conversation) RemoveLast() *llm.ChatMessage {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.messages) == 0 {
		return nil
	}

	msg := c.messages[len(c.messages)-1]
	c.messages = c.messages[:len(c.messages)-1]
	if len(c.messageTypes) > 0 {
		c.messageTypes = c.messageTypes[:len(c.messageTypes)-1]
	}
	return &msg
}

// FreezeMemorySnapshot captures the current memory context for prefix caching.
// This implements the Hermes pattern where memory is frozen at session start
// to enable API prefix caching. The snapshot remains stable for the session,
// allowing the LLM API to cache the prefix and reduce token costs.
// Returns an error if memory context is empty (nothing to freeze).
func (c *Conversation) FreezeMemorySnapshot(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.memoryContext == "" {
		return nil // Nothing to freeze
	}

	c.memorySnapshot = c.memoryContext
	return nil
}

// GetMemorySnapshot returns the frozen memory snapshot for prefix caching.
// Returns empty string if no snapshot has been frozen.
func (c *Conversation) GetMemorySnapshot() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.memorySnapshot
}

// HasMemorySnapshot returns true if a memory snapshot has been frozen.
func (c *Conversation) HasMemorySnapshot() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.memorySnapshot != ""
}

// ClearMemorySnapshot clears the frozen snapshot (e.g., at session end).
func (c *Conversation) ClearMemorySnapshot() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.memorySnapshot = ""
}

// GetMemoryContext returns the live memory context (deprecated, use snapshot for prompts).
func (c *Conversation) GetMemoryContext() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.memoryContext
}

// SetMemoryContext sets the live memory context (for internal use).
func (c *Conversation) SetMemoryContext(ctxStr string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.memoryContext = ctxStr
}

// FetchAndFreezeMemory fetches memory context and freezes it for prefix caching.
// The fetchFn should fetch the relevant memory context and return it as a string.
// Returns an error if the fetch fails.
func (c *Conversation) FetchAndFreezeMemory(ctx context.Context, fetchFn func(ctx context.Context) (string, error)) error {
	ctxStr, err := fetchFn(ctx)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.memoryContext = ctxStr
	c.memorySnapshot = ctxStr
	return nil
}

// BuildPromptWithSnapshot returns the memory context for prompt building,
// using the frozen snapshot if available for prefix cache efficiency.
func (c *Conversation) BuildPromptWithSnapshot() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Use frozen snapshot if available, otherwise live context
	if c.memorySnapshot != "" {
		return c.memorySnapshot
	}
	return c.memoryContext
}

// ClearMemoryContext clears both live context and frozen snapshot.
func (c *Conversation) ClearMemoryContext() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.memoryContext = ""
	c.memorySnapshot = ""
}

// StabilizeToolPrefix creates a deterministic byte layout for tool definitions
// and updates the cache prefix hash. Tools are sorted alphabetically by name,
// and each tool is serialized as name + description + parameters sorted by key.
// The resulting SHA256 hash enables detection of prefix changes for cache
// invalidation. Returns the stabilized tool list sorted by name.
func (c *Conversation) StabilizeToolPrefix(tools []llm.ToolDefinition) []llm.ToolDefinition {
	// Sort tools by name for deterministic ordering
	sorted := make([]llm.ToolDefinition, len(tools))
	copy(sorted, tools)
	slices.SortFunc(sorted, func(a, b llm.ToolDefinition) int {
		return strings.Compare(a.Function.Name, b.Function.Name)
	})

	// Build deterministic serialization
	c.mu.RLock()
	prompt := c.systemPrompt
	c.mu.RUnlock()

	var buf strings.Builder
	buf.WriteString(prompt)
	buf.WriteByte(0) // separator

	for _, tool := range sorted {
		buf.WriteString(tool.Function.Name)
		buf.WriteByte(0)
		buf.WriteString(tool.Function.Description)
		buf.WriteByte(0)

		// Hash Required array for this tool
		for _, r := range tool.Function.Parameters.Required {
			buf.WriteString(r)
			buf.WriteByte(0)
		}

		// Sort parameter keys for deterministic output
		paramKeys := make([]string, 0, len(tool.Function.Parameters.Properties))
		for k := range tool.Function.Parameters.Properties {
			paramKeys = append(paramKeys, k)
		}
		sort.Strings(paramKeys)

		for _, k := range paramKeys {
			prop := tool.Function.Parameters.Properties[k]
			buf.WriteString(k)
			buf.WriteByte(0)
			buf.WriteString(prop.Type)
			buf.WriteByte(0)
			buf.WriteString(prop.Description)
			buf.WriteByte(0)
			// Hash Enum values
			for _, e := range prop.Enum {
				buf.WriteString(e)
				buf.WriteByte(0)
			}
		}
		buf.WriteByte(0xFF) // tool boundary
	}

	// Compute SHA256
	hash := sha256.Sum256([]byte(buf.String()))
	newHash := hex.EncodeToString(hash[:])

	c.mu.Lock()
	c.cachePrefixChanged = (c.cachePrefixHash != "" && c.cachePrefixHash != newHash)
	c.cachePrefixHash = newHash
	c.mu.Unlock()

	return sorted
}

// PrefixChanged returns true if the system prompt + tools hash has changed
// since the last call to StabilizeToolPrefix. Returns true on the first call
// (when no hash has been computed yet).
func (c *Conversation) PrefixChanged() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.cachePrefixHash == "" {
		return true
	}
	return c.cachePrefixChanged
}

// GetCachePrefixHash returns the current cache prefix hash for external
// cache invalidation checks.
func (c *Conversation) GetCachePrefixHash() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cachePrefixHash
}

// InjectContextBounded inserts context with a token budget limit.
// This is used for memory injection to prevent memory from dominating the context.
// If the context exceeds the budget, it is truncated proportionally.
func (c *Conversation) InjectContextBounded(ctxStr string, maxTokens int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove any previous context messages (marked with a specific pattern)
	newMessages := make([]llm.ChatMessage, 0, len(c.messages))
	newTypes := make([]MessageClassification, 0, len(c.messageTypes))
	for i, msg := range c.messages {
		if msg.Role == llm.RoleSystem && isContextMessage(msg.Content) {
			continue
		}
		newMessages = append(newMessages, msg)
		if i < len(c.messageTypes) {
			newTypes = append(newTypes, c.messageTypes[i])
		}
	}

	// Estimate token count and truncate if necessary
	contextContent := ctxStr
	estimatedTokens := llm.EstimateTokenCountHeuristic(ctxStr)

	if estimatedTokens > maxTokens {
		// Truncate proportionally
		ratio := float64(maxTokens) / float64(estimatedTokens)
		truncateLen := int(float64(len(ctxStr)) * ratio)
		if truncateLen > 0 {
			contextContent = ctxStr[:truncateLen] + "\n\n...[memory truncated due to token budget]..."
		}
	}

	// Insert new context at the beginning
	contextMsg := llm.ChatMessage{
		Role:    llm.RoleSystem,
		Content: "# Relevant Context from Memory\n" + contextContent,
	}

	c.messages = append([]llm.ChatMessage{contextMsg}, newMessages...)
	c.messageTypes = append([]MessageClassification{MessageUnknown}, newTypes...)
}

// This is used for memory injection before LLM calls.
func (c *Conversation) InjectContext(ctxStr string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove any previous context messages (marked with a specific pattern)
	newMessages := make([]llm.ChatMessage, 0, len(c.messages))
	newTypes := make([]MessageClassification, 0, len(c.messageTypes))
	for i, msg := range c.messages {
		if msg.Role == llm.RoleSystem && isContextMessage(msg.Content) {
			continue
		}
		newMessages = append(newMessages, msg)
		if i < len(c.messageTypes) {
			newTypes = append(newTypes, c.messageTypes[i])
		}
	}

	// Insert new context at the beginning
	contextMsg := llm.ChatMessage{
		Role:    llm.RoleSystem,
		Content: "# Relevant Context from Memory\n" + ctxStr,
	}

	c.messages = append([]llm.ChatMessage{contextMsg}, newMessages...)
	c.messageTypes = append([]MessageClassification{MessageUnknown}, newTypes...)
}

// isContextMessage checks if a message is a memory context message.
func isContextMessage(content string) bool {
	return len(content) > 30 && content[:30] == "# Relevant Context from Memory"
}

// CompressionReport contains statistics about a compression operation.
type CompressionReport struct {
	TokensBefore   int
	TokensAfter    int
	TokensRemoved  int
	MessagesBefore int
	MessagesAfter  int
}

// CompressByImportance removes messages based on semantic importance to reach a target
// token ratio. The targetRatio parameter (0.0-1.0) specifies what fraction of the current
// token count to retain. For example, 0.5 means compress to 50% of current tokens.
//
// Messages are sorted by importance (lowest first), then by token count (highest first),
// and removed in that order until the target is reached. Anchor messages are never removed.
// User input messages are treated as ImportanceCritical and preserved.
func (c *Conversation) CompressByImportance(targetRatio float64) CompressionReport {
	c.mu.Lock()
	defer c.mu.Unlock()

	const charsPerToken = 3

	report := CompressionReport{
		MessagesBefore: len(c.messages),
	}

	if len(c.messages) == 0 {
		return report
	}

	// Calculate current token usage across all messages
	currentTokens := 0
	msgTokens := make([]int, len(c.messages))
	for i, msg := range c.messages {
		tokens := len(msg.Content) / charsPerToken
		// Count tool calls
		for _, tc := range msg.ToolCalls {
			tokens += len(tc.Function.Name) / charsPerToken
			tokens += len(tc.Function.Arguments) / charsPerToken
			tokens += 20
		}
		if msg.ToolCallID != "" {
			tokens += 15
		}
		msgTokens[i] = tokens
		currentTokens += tokens
	}

	report.TokensBefore = currentTokens

	if currentTokens == 0 {
		report.TokensAfter = 0
		return report
	}

	targetTokens := int(float64(currentTokens) * targetRatio)

	if currentTokens <= targetTokens {
		report.TokensAfter = currentTokens
		report.MessagesAfter = len(c.messages)
		return report
	}

	// Build index list with importance info
	type msgIndex struct {
		idx        int
		importance MessageImportance
		tokens     int
	}

	indices := make([]msgIndex, len(c.messages))
	for i := range c.messages {
		msgType := MessageUnknown
		if i < len(c.messageTypes) {
			msgType = c.messageTypes[i]
		}
		importance := getMessageImportance(msgType)
		// Anchor messages are treated as ImportanceCritical
		if c.isAnchorMessageUnsafe(c.messages[i].Content) {
			importance = ImportanceCritical
		}
		indices[i] = msgIndex{
			idx:        i,
			importance: importance,
			tokens:     msgTokens[i],
		}
	}

	// Sort by importance (lowest first), then by token count (highest first)
	// This means low-importance, high-token messages are candidates for removal first.
	for i := range len(indices) - 1 {
		for j := i + 1; j < len(indices); j++ {
			shouldSwap := false
			if indices[i].importance > indices[j].importance {
				shouldSwap = true
			} else if indices[i].importance == indices[j].importance && indices[i].tokens < indices[j].tokens {
				shouldSwap = true
			}
			if shouldSwap {
				indices[i], indices[j] = indices[j], indices[i]
			}
		}
	}

	// Mark messages for removal: walk sorted list and remove until target reached.
	// Never remove ImportanceCritical (anchor messages and user input).
	keepMask := make([]bool, len(c.messages))
	for i := range keepMask {
		keepMask[i] = true
	}

	tokensRemoved := 0
	for _, mi := range indices {
		if currentTokens-tokensRemoved <= targetTokens {
			break
		}
		// Never remove critical messages
		if mi.importance == ImportanceCritical {
			continue
		}
		keepMask[mi.idx] = false
		tokensRemoved += mi.tokens
	}

	// Build new message and type slices
	newMessages := make([]llm.ChatMessage, 0, len(c.messages))
	newTypes := make([]MessageClassification, 0, len(c.messageTypes))
	for i, msg := range c.messages {
		if keepMask[i] {
			newMessages = append(newMessages, msg)
			if i < len(c.messageTypes) {
				newTypes = append(newTypes, c.messageTypes[i])
			}
		}
	}

	c.messages = newMessages
	c.messageTypes = newTypes

	report.MessagesAfter = len(c.messages)
	report.TokensAfter = currentTokens - tokensRemoved
	report.TokensRemoved = tokensRemoved

	return report
}

// GetCompactionCandidates returns the indices of messages that would be compressed
// at the given target ratio, without actually removing them. The caller uses this
// to decide whether to trigger compaction and to know which messages are affected.
// Returns indices sorted by importance (lowest importance first among the candidates)
// and a CompressionReport describing what the compaction would achieve.
func (c *Conversation) GetCompactionCandidates(targetRatio float64) (candidates []int, report CompressionReport) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	const charsPerToken = 3

	report = CompressionReport{
		MessagesBefore: len(c.messages),
	}

	if len(c.messages) == 0 {
		return nil, report
	}

	// Calculate current token usage
	currentTokens := 0
	msgTokens := make([]int, len(c.messages))
	for i, msg := range c.messages {
		tokens := len(msg.Content) / charsPerToken
		for _, tc := range msg.ToolCalls {
			tokens += len(tc.Function.Name) / charsPerToken
			tokens += len(tc.Function.Arguments) / charsPerToken
			tokens += 20
		}
		if msg.ToolCallID != "" {
			tokens += 15
		}
		msgTokens[i] = tokens
		currentTokens += tokens
	}

	report.TokensBefore = currentTokens

	if currentTokens == 0 {
		report.TokensAfter = 0
		return nil, report
	}

	targetTokens := int(float64(currentTokens) * targetRatio)

	if currentTokens <= targetTokens {
		report.TokensAfter = currentTokens
		report.MessagesAfter = len(c.messages)
		return nil, report
	}

	// Build index list with importance info
	type msgIndex struct {
		idx        int
		importance MessageImportance
		tokens     int
	}

	indices := make([]msgIndex, len(c.messages))
	for i := range c.messages {
		msgType := MessageUnknown
		if i < len(c.messageTypes) {
			msgType = c.messageTypes[i]
		}
		importance := getMessageImportance(msgType)
		if c.isAnchorMessageUnsafe(c.messages[i].Content) {
			importance = ImportanceCritical
		}
		indices[i] = msgIndex{
			idx:        i,
			importance: importance,
			tokens:     msgTokens[i],
		}
	}

	// Sort by importance (lowest first), then by token count (highest first)
	for i := range len(indices) - 1 {
		for j := i + 1; j < len(indices); j++ {
			shouldSwap := false
			if indices[i].importance > indices[j].importance {
				shouldSwap = true
			} else if indices[i].importance == indices[j].importance && indices[i].tokens < indices[j].tokens {
				shouldSwap = true
			}
			if shouldSwap {
				indices[i], indices[j] = indices[j], indices[i]
			}
		}
	}

	// Identify candidates for removal, never removing ImportanceCritical
	tokensRemoved := 0
	var removedIndices []int
	for _, mi := range indices {
		if currentTokens-tokensRemoved <= targetTokens {
			break
		}
		if mi.importance == ImportanceCritical {
			continue
		}
		removedIndices = append(removedIndices, mi.idx)
		tokensRemoved += mi.tokens
	}

	report.TokensAfter = currentTokens - tokensRemoved
	report.TokensRemoved = tokensRemoved
	report.MessagesAfter = len(c.messages) - len(removedIndices)

	return removedIndices, report
}

// RemoveCompactedMessages removes the messages at the given indices from the
// in-memory conversation. The indices must be valid positions in the current
// message slice. After removal, the messageTypes slice is kept in sync.
// Returns the approximate tokens saved by the removal.
func (c *Conversation) RemoveCompactedMessages(indices []int) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(indices) == 0 || len(c.messages) == 0 {
		return 0
	}

	// Build a set of indices to remove for O(1) lookup
	removeSet := make(map[int]bool, len(indices))
	for _, idx := range indices {
		if idx >= 0 && idx < len(c.messages) {
			removeSet[idx] = true
		}
	}

	if len(removeSet) == 0 {
		return 0
	}

	const charsPerToken = 3
	tokensSaved := 0

	newMessages := make([]llm.ChatMessage, 0, len(c.messages)-len(removeSet))
	newTypes := make([]MessageClassification, 0, len(c.messageTypes)-len(removeSet))

	for i, msg := range c.messages {
		if removeSet[i] {
			// Calculate tokens saved
			tokens := len(msg.Content) / charsPerToken
			for _, tc := range msg.ToolCalls {
				tokens += len(tc.Function.Name) / charsPerToken
				tokens += len(tc.Function.Arguments) / charsPerToken
				tokens += 20
			}
			if msg.ToolCallID != "" {
				tokens += 15
			}
			tokensSaved += tokens
			continue
		}
		newMessages = append(newMessages, msg)
		if i < len(c.messageTypes) {
			newTypes = append(newTypes, c.messageTypes[i])
		}
	}

	c.messages = newMessages
	c.messageTypes = newTypes

	return tokensSaved
}

// ConversationStore manages multiple conversations with LRU eviction.
type ConversationStore struct {
	mu            sync.RWMutex
	conversations map[string]*Conversation
	order         []string // LRU order, most recent at end
	maxSize       int
	persistFn     PersistenceFunc
}

// PersistenceFunc is called to persist messages after they're added.
type PersistenceFunc func(conversationID string, messages []llm.ChatMessage) error

// ConversationStoreOption is a functional option for ConversationStore.
type ConversationStoreOption func(*ConversationStore)

// WithPersistence sets the persistence callback.
func WithPersistence(fn PersistenceFunc) ConversationStoreOption {
	return func(s *ConversationStore) {
		if fn != nil {
			s.persistFn = fn
		}
	}
}

// NewConversationStore creates a new conversation store.
func NewConversationStore(maxSize int, opts ...ConversationStoreOption) *ConversationStore {
	if maxSize <= 0 {
		maxSize = 100
	}
	s := &ConversationStore{
		conversations: make(map[string]*Conversation),
		order:         make([]string, 0, maxSize),
		maxSize:       maxSize,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// SetPersistence sets the persistence callback after creation.
func (s *ConversationStore) SetPersistence(fn PersistenceFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fn != nil {
		s.persistFn = fn
	}
}

// GetOrRestore retrieves or restores a conversation from persistence.
// If the conversation is already in the store, it is returned directly.
// Otherwise, restoreFn is called to load messages from the backing store.
// On restore failure, an empty conversation is created and the error is returned.
func (s *ConversationStore) GetOrRestore(id string, restoreFn func() ([]llm.ChatMessage, error)) (*Conversation, error) {
	// Fast path: check cache under RLock.
	s.mu.RLock()
	if conv, ok := s.conversations[id]; ok {
		s.mu.RUnlock()
		s.mu.Lock()
		s.moveToEnd(id)
		s.mu.Unlock()
		return conv, nil
	}
	s.mu.RUnlock()

	// Slow path: perform I/O outside the lock.
	messages, err := restoreFn()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check: another goroutine may have restored the same ID.
	if conv, ok := s.conversations[id]; ok {
		s.moveToEnd(id)
		return conv, nil
	}

	if err != nil {
		conv := NewConversation()
		s.conversations[id] = conv
		s.order = append(s.order, id)
		s.evictOldest()
		return conv, fmt.Errorf("failed to restore conversation: %w", err)
	}

	conv := NewConversation()
	if len(messages) > 0 {
		conv.RestoreFromMessages(messages)
	}
	s.conversations[id] = conv
	s.order = append(s.order, id)
	s.evictOldest()
	return conv, nil
}

// Get retrieves a conversation by ID, creating a new one if it doesn't exist.
func (s *ConversationStore) Get(id string) *Conversation {
	s.mu.RLock()
	if conv, ok := s.conversations[id]; ok {
		s.mu.RUnlock()
		s.mu.Lock()
		s.moveToEnd(id)
		s.mu.Unlock()
		return conv
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock.
	if conv, ok := s.conversations[id]; ok {
		s.moveToEnd(id)
		return conv
	}

	// Create new conversation
	conv := NewConversation()
	s.conversations[id] = conv

	// Add to order tracking
	s.order = append(s.order, id)

	// Evict oldest if over capacity
	s.evictOldest()

	return conv
}

// evictOldest removes the oldest conversation when over capacity.
// Must be called with lock held.
func (s *ConversationStore) evictOldest() {
	if len(s.order) > s.maxSize {
		oldest := s.order[0]
		delete(s.conversations, oldest)
		s.order = s.order[1:]
	}
}

// GetIfExists retrieves a conversation by ID, returning nil if not found.
func (s *ConversationStore) GetIfExists(id string) *Conversation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.conversations[id]
}

// Delete removes a conversation by ID.
func (s *ConversationStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.conversations[id]; !ok {
		return
	}

	delete(s.conversations, id)

	// Remove from order
	for i, oid := range s.order {
		if oid == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

// Size returns the number of conversations.
func (s *ConversationStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.conversations)
}

// moveToEnd moves an ID to the end of the order slice.
// Must be called with lock held.
func (s *ConversationStore) moveToEnd(id string) {
	for i, oid := range s.order {
		if oid == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			s.order = append(s.order, id)
			return
		}
	}
}
