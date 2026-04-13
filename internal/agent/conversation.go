// Package agent provides the main reasoning/action loop for the Meept agent.
package agent

import (
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
// Conversation manages chat message history with LRU eviction and truncation.
type Conversation struct {
	mu           sync.RWMutex
	messages     []llm.ChatMessage
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
		maxMessages:  DefaultMaxMessages,
		contextLimit: DefaultContextLimit,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// AddMessage appends a message to the conversation history.
func (c *Conversation) AddMessage(msg llm.ChatMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.messages = append(c.messages, msg)
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
