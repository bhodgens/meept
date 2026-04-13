// Package agent provides the main reasoning/action loop for the Meept agent.
package agent

import (
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
	totalBudget     int   // Total tokens allocated for the session
	usedBudget      int   // Tokens used so far
	tokensPerTurn   int   // Expected tokens per turn (for estimation)
	maxTurns        int   // Maximum turns before wrap-up
	currentTurn     int   // Current turn number
	warningZone     bool  // Set when budget is nearly depleted
	wrapUpRequested bool  // Set when wrap-up is requested
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
	return t.totalBudget - t.usedBudget
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
func (t *TurnBudgetTracker) GetTurnInfo() (current, max, used, total int) {
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
}

// ConversationOption is a functional option for configuring a Conversation.
type ConversationOption func(*Conversation)

// WithMaxMessages sets the maximum number of messages before truncation.
func WithMaxMessages(max int) ConversationOption {
	return func(c *Conversation) {
		c.maxMessages = max
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

// classifyMessageClassification determines the semantic type of a message based on role and content.
func classifyMessageClassification(msg llm.ChatMessage, isFirstUserMsg bool) MessageClassification {
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
	keep := c.maxMessages
	if keep < 1 {
		keep = 1
	}

	removed := len(c.messages) - keep
	if removed <= 0 {
		return 0
	}

	// Keep the most recent messages
	c.messages = c.messages[removed:]
	return removed
}

// TruncateByTokens removes old messages to fit within a token budget.
// It uses a rough estimate of 3 characters per token (appropriate for JSON/code-heavy content).
// It counts both Content and ToolCalls fields for accurate estimation.
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
		if len(c.messages) > 1 {
			removed := len(c.messages) - 1
			c.messages = c.messages[removed:]
			return removed
		}
		return 0
	}

	// Count tokens from the end (most recent) until we exceed budget
	totalTokens := 0
	keepFrom := 0

	for i := len(c.messages) - 1; i >= 0; i-- {
		msgTokens := len(c.messages[i].Content) / charsPerToken
		// Count tool calls (assistant messages requesting tools)
		for _, tc := range c.messages[i].ToolCalls {
			msgTokens += len(tc.Function.Name) / charsPerToken
			msgTokens += len(tc.Function.Arguments) / charsPerToken
			msgTokens += 20 // structural overhead per tool call
		}
		// Count tool result overhead
		if c.messages[i].ToolCallID != "" {
			msgTokens += 15 // tool_call_id structural overhead
		}
		if totalTokens+msgTokens > availableBudget {
			keepFrom = i + 1
			break
		}
		totalTokens += msgTokens
	}

	if keepFrom == 0 {
		return 0
	}

	removed := keepFrom
	c.messages = c.messages[keepFrom:]
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
		msgTokens := len(msg.Content) / charsPerToken
		indices = append(indices, msgIndex{
			idx:        i,
			importance: getMessageImportance(msgType),
			tokens:     msgTokens,
		})
	}

	// Sort by importance (lowest first), then by token count (highest first)
	for i := 0; i < len(indices)-1; i++ {
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

	keepMask := make([]bool, len(c.messages))
	tokensRemoved := 0
	for _, mi := range indices {
		if tokensRemoved >= removedTokens {
			break
		}
		keepMask[mi.idx] = false
		tokensRemoved += mi.tokens
	}

	// Always keep the last few messages
	minKeep := 4
	if len(c.messages) < minKeep {
		minKeep = len(c.messages)
	}
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
// It preserves: (1) system prompt always, (2) original user message, (3) most recent messages.
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
		// System prompt alone exceeds budget, return just system + last message
		result := make([]llm.ChatMessage, 0, 2)
		if c.systemPrompt != "" {
			result = append(result, llm.ChatMessage{
				Role:    llm.RoleSystem,
				Content: c.systemPrompt,
			})
		}
		if len(c.messages) > 0 {
			result = append(result, c.messages[len(c.messages)-1])
		}
		return result
	}

	// Find the original user message (first user message after any system messages)
	var originalUserIdx int = -1
	for i, msg := range c.messages {
		if msg.Role == llm.RoleUser {
			originalUserIdx = i
			break
		}
	}

	// Build result with system prompt first
	result := make([]llm.ChatMessage, 0, len(c.messages)+1)
	if c.systemPrompt != "" {
		result = append(result, llm.ChatMessage{
			Role:    llm.RoleSystem,
			Content: c.systemPrompt,
		})
	}

	// If we have an original user message, always include it
	originalUserTokens := 0
	if originalUserIdx >= 0 {
		originalUserTokens = len(c.messages[originalUserIdx].Content) / charsPerToken
		availableBudget -= originalUserTokens
	}

	// Count tokens from the end (most recent) until we exceed remaining budget
	totalTokens := 0
	keepFromIdx := len(c.messages)

	for i := len(c.messages) - 1; i >= 0; i-- {
		// Skip original user message in this pass, we'll add it separately
		if i == originalUserIdx {
			continue
		}

		msgTokens := len(c.messages[i].Content) / charsPerToken
		// Also count tool calls if present
		for _, tc := range c.messages[i].ToolCalls {
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

	// Add remaining messages within budget
	for i := keepFromIdx; i < len(c.messages); i++ {
		// Skip original user if we already added it
		if i == originalUserIdx && originalUserIdx < keepFromIdx {
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
		systemPrompt: c.systemPrompt,
		maxMessages:  c.maxMessages,
		contextLimit: c.contextLimit,
	}

	copy(clone.messages, c.messages)

	// Deep copy tool calls for each message
	for i, msg := range clone.messages {
		if len(msg.ToolCalls) > 0 {
			clone.messages[i].ToolCalls = make([]llm.ToolCall, len(msg.ToolCalls))
			copy(clone.messages[i].ToolCalls, msg.ToolCalls)
		}
	}

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
	return &msg
}

// InjectContext inserts a context message after the system prompt.

// InjectContextBounded inserts context with a token budget limit.
// This is used for memory injection to prevent memory from dominating the context.
// If the context exceeds the budget, it is truncated proportionally.
func (c *Conversation) InjectContextBounded(context string, maxTokens int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove any previous context messages (marked with a specific pattern)
	newMessages := make([]llm.ChatMessage, 0, len(c.messages))
	for _, msg := range c.messages {
		if msg.Role == llm.RoleSystem && isContextMessage(msg.Content) {
			continue
		}
		newMessages = append(newMessages, msg)
	}

	// Estimate token count and truncate if necessary
	contextContent := context
	estimatedTokens := llm.EstimateTokenCountHeuristic(context)
	
	if estimatedTokens > maxTokens {
		// Truncate proportionally
		ratio := float64(maxTokens) / float64(estimatedTokens)
		truncateLen := int(float64(len(context)) * ratio)
		if truncateLen > 0 {
			contextContent = context[:truncateLen] + "\n\n...[memory truncated due to token budget]..."
		}
	}

	// Insert new context at the beginning
	contextMsg := llm.ChatMessage{
		Role:    llm.RoleSystem,
		Content: "# Relevant Context from Memory\n" + contextContent,
	}

	c.messages = append([]llm.ChatMessage{contextMsg}, newMessages...)
}
// This is used for memory injection before LLM calls.
func (c *Conversation) InjectContext(context string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove any previous context messages (marked with a specific pattern)
	newMessages := make([]llm.ChatMessage, 0, len(c.messages))
	for _, msg := range c.messages {
		if msg.Role == llm.RoleSystem && isContextMessage(msg.Content) {
			continue
		}
		newMessages = append(newMessages, msg)
	}

	// Insert new context at the beginning
	contextMsg := llm.ChatMessage{
		Role:    llm.RoleSystem,
		Content: "# Relevant Context from Memory\n" + context,
	}

	c.messages = append([]llm.ChatMessage{contextMsg}, newMessages...)
}

// isContextMessage checks if a message is a memory context message.
func isContextMessage(content string) bool {
	return len(content) > 30 && content[:30] == "# Relevant Context from Memory"
}

// ConversationStore manages multiple conversations with LRU eviction.
type ConversationStore struct {
	mu            sync.RWMutex
	conversations map[string]*Conversation
	order         []string // LRU order, most recent at end
	maxSize       int
}

// NewConversationStore creates a new conversation store.
func NewConversationStore(maxSize int) *ConversationStore {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &ConversationStore{
		conversations: make(map[string]*Conversation),
		order:         make([]string, 0, maxSize),
		maxSize:       maxSize,
	}
}

// Get retrieves a conversation by ID, creating a new one if it doesn't exist.
func (s *ConversationStore) Get(id string) *Conversation {
	s.mu.Lock()
	defer s.mu.Unlock()

	if conv, ok := s.conversations[id]; ok {
		// Move to end (most recently used)
		s.moveToEnd(id)
		return conv
	}

	// Create new conversation
	conv := NewConversation()
	s.conversations[id] = conv

	// Add to order tracking
	s.order = append(s.order, id)

	// Evict oldest if over capacity
	if len(s.order) > s.maxSize {
		oldest := s.order[0]
		delete(s.conversations, oldest)
		s.order = s.order[1:]
	}

	return conv
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
