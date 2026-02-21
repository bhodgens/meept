package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory/memvid"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
	"github.com/caimlas/meept/pkg/security"
)

// Default values for the agent loop.
const (
	DefaultMaxIterations = 10
	DefaultTimeout       = 5 * time.Minute
)

// Error types for the agent loop.
var (
	ErrMaxIterationsReached = errors.New("maximum iterations reached")
	ErrContextCancelled     = errors.New("context cancelled")
	ErrNoLLMClient          = errors.New("no LLM client configured")
)

// AgentConfig holds configuration for the agent loop.
type AgentConfig struct {
	MaxIterations       int
	Timeout             time.Duration
	Constitution        string
	Restrictions        string
	Purpose             string
	Personality         string
	SystemPromptOveride string
}

// DefaultAgentConfig returns a configuration with sensible defaults.
func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		MaxIterations: DefaultMaxIterations,
		Timeout:       DefaultTimeout,
		Constitution:  DefaultConstitution,
		Restrictions:  DefaultRestrictions,
		Purpose:       DefaultPurpose,
		Personality:   "",
	}
}

// AgentLoop orchestrates LLM reasoning interleaved with tool execution.
type AgentLoop struct {
	mu sync.RWMutex

	// Core components
	llm      *llm.Client
	executor *Executor
	registry ToolRegistry
	security *security.PermissionChecker
	bus      *bus.MessageBus
	logger   *slog.Logger

	// Memory for context injection
	memvid    *memvid.Client
	taskStore *task.Store

	// Configuration
	config AgentConfig

	// Conversation management
	conversations *ConversationStore

	// Prompt building
	promptBuilder *PromptBuilder

	// Agent identity
	agentID string
}

// LoopOption is a functional option for configuring an AgentLoop.
type LoopOption func(*AgentLoop)

// WithLLMClient sets the LLM client.
func WithLLMClient(client *llm.Client) LoopOption {
	return func(l *AgentLoop) {
		l.llm = client
	}
}

// WithToolRegistry sets the tool registry.
func WithToolRegistry(registry ToolRegistry) LoopOption {
	return func(l *AgentLoop) {
		l.registry = registry
	}
}

// WithSecurityChecker sets the security permission checker.
func WithSecurityChecker(checker *security.PermissionChecker) LoopOption {
	return func(l *AgentLoop) {
		l.security = checker
	}
}

// WithMessageBus sets the message bus for event publishing.
func WithMessageBus(b *bus.MessageBus) LoopOption {
	return func(l *AgentLoop) {
		l.bus = b
	}
}

// WithLoopLogger sets the logger.
func WithLoopLogger(logger *slog.Logger) LoopOption {
	return func(l *AgentLoop) {
		l.logger = logger
	}
}

// WithAgentConfig sets the agent configuration.
func WithAgentConfig(config AgentConfig) LoopOption {
	return func(l *AgentLoop) {
		l.config = config
	}
}

// WithMemvidClient sets the memvid client for memory injection.
func WithMemvidClient(client *memvid.Client) LoopOption {
	return func(l *AgentLoop) {
		l.memvid = client
	}
}

// WithAgentID sets the agent identifier.
func WithAgentID(id string) LoopOption {
	return func(l *AgentLoop) {
		l.agentID = id
	}
}

// WithTaskStore sets the task store for inherited memory fetching.
func WithTaskStore(store *task.Store) LoopOption {
	return func(l *AgentLoop) {
		l.taskStore = store
	}
}

// NewAgentLoop creates a new agent loop.
func NewAgentLoop(opts ...LoopOption) *AgentLoop {
	loop := &AgentLoop{
		config:        DefaultAgentConfig(),
		conversations: NewConversationStore(100),
		logger:        slog.Default(),
	}

	for _, opt := range opts {
		opt(loop)
	}

	// Create executor if we have a registry
	if loop.registry != nil {
		loop.executor = NewExecutor(
			loop.registry,
			loop.security,
			WithExecutorLogger(loop.logger),
		)
	}

	// Build prompt builder from config
	loop.promptBuilder = NewPromptBuilderFromConfig(PromptConfig{
		Constitution: loop.config.Constitution,
		Restrictions: loop.config.Restrictions,
		Purpose:      loop.config.Purpose,
		Personality:  loop.config.Personality,
	})

	return loop
}

// RunOnce processes a single user turn through the full reasoning loop.
func (l *AgentLoop) RunOnce(ctx context.Context, userMessage, conversationID string) (string, error) {
	if l.llm == nil {
		return "", ErrNoLLMClient
	}

	// Get or create conversation
	conv := l.conversations.Get(conversationID)

	// Build and set system prompt
	systemPrompt := l.buildSystemPrompt()
	conv.SetSystemPrompt(systemPrompt)

	// Add user message
	conv.AddUserMessage(userMessage)

	// Truncate if needed
	conv.Truncate()

	// Run reasoning cycle
	response, err := l.reasoningCycle(ctx, conv, conversationID)
	if err != nil {
		l.logger.Error("Reasoning cycle failed",
			"conversation", conversationID,
			"error", err,
		)
		// Add error message to conversation
		errorMsg := "I encountered an error during processing. Please try again."
		conv.AddAssistantMessage(errorMsg)
		return errorMsg, err
	}

	// Add final response to conversation
	conv.AddAssistantMessage(response)
	return response, nil
}

// reasoningCycle runs the main reasoning loop with tool execution.
func (l *AgentLoop) reasoningCycle(ctx context.Context, conv *Conversation, conversationID string) (string, error) {
	for iteration := 1; iteration <= l.config.MaxIterations; iteration++ {
		select {
		case <-ctx.Done():
			return "", ErrContextCancelled
		default:
		}

		l.logger.Debug("Agent loop iteration",
			"iteration", iteration,
			"max", l.config.MaxIterations,
			"conversation", conversationID,
		)

		// Get tool definitions
		var tools []llm.ToolDefinition
		if l.registry != nil {
			tools = l.registry.GetDefinitions()
		}

		// Call LLM
		messages := conv.GetMessages()
		var chatOpts []llm.ChatOption
		if len(tools) > 0 {
			chatOpts = append(chatOpts, llm.WithTools(tools))
		}

		response, err := l.llm.Chat(ctx, messages, chatOpts...)
		if err != nil {
			l.logger.Error("LLM call failed",
				"iteration", iteration,
				"error", err,
			)
			return "", fmt.Errorf("LLM call failed: %w", err)
		}

		// Case 1: LLM returned tool calls
		if response.HasToolCalls() {
			// Add assistant message with tool calls
			conv.AddAssistantMessageWithToolCalls(response.Content, response.ToolCalls)

			// Publish agent action event
			l.publishAction(conversationID, iteration, response.ToolCalls)

			// Execute tools
			results := l.executeToolCalls(ctx, response.ToolCalls)

			// Add tool results to conversation
			for _, result := range results {
				conv.AddToolResult(result.ToolCallID, result.ToJSON())
			}

			// Publish agent result event
			l.publishResult(conversationID, iteration, results)

			// Continue loop for LLM to process tool results
			continue
		}

		// Case 2: LLM returned text response (no tool calls) - done
		l.logger.Info("Agent loop complete",
			"iterations", iteration,
			"conversation", conversationID,
		)
		return response.Content, nil
	}

	// Max iterations reached
	l.logger.Warn("Max iterations reached",
		"max", l.config.MaxIterations,
		"conversation", conversationID,
	)

	exhaustMsg := "I've reached the maximum number of reasoning steps for this turn. " +
		"Here is what I have so far -- please let me know if you'd like me to continue."
	return exhaustMsg, ErrMaxIterationsReached
}

// HandleMessage processes a single message without conversation context.
func (l *AgentLoop) HandleMessage(ctx context.Context, message string) (string, error) {
	return l.RunOnce(ctx, message, generateConversationID())
}

// RunWithTask processes a task through the agent loop with memory context injection.
func (l *AgentLoop) RunWithTask(ctx context.Context, t *task.Task) (string, error) {
	if l.llm == nil {
		return "", ErrNoLLMClient
	}

	// Use first linked session or task ID as conversation ID
	conversationID := t.ID
	if len(t.LinkedSessions) > 0 {
		conversationID = t.LinkedSessions[0]
	}

	// Get or create conversation
	conv := l.conversations.Get(conversationID)

	// Build context parts from memory
	contextParts := l.buildMemoryContext(ctx, t)

	// Build system prompt with injected context
	systemPrompt := l.buildSystemPromptWithContext(contextParts)
	conv.SetSystemPrompt(systemPrompt)

	// Build user message from task
	userMessage := l.buildTaskMessage(t)
	conv.AddUserMessage(userMessage)

	// Truncate if needed
	conv.Truncate()

	// Run reasoning cycle
	response, err := l.reasoningCycle(ctx, conv, conversationID)
	if err != nil {
		l.logger.Error("Task reasoning cycle failed",
			"task", t.ID,
			"error", err,
		)
		errorMsg := "I encountered an error during processing. Please try again."
		conv.AddAssistantMessage(errorMsg)
		return errorMsg, err
	}

	// Add final response to conversation
	conv.AddAssistantMessage(response)

	// Record memory of this task execution
	if l.memvid != nil {
		go l.recordTaskExecution(context.Background(), t, response)
	}

	return response, nil
}

// buildMemoryContext fetches and formats memory context for the task.
func (l *AgentLoop) buildMemoryContext(ctx context.Context, t *task.Task) []string {
	var parts []string

	// Fetch inherited memories from parent task
	if l.memvid != nil && l.taskStore != nil && t.InheritedFrom != "" {
		parentTask, err := l.taskStore.GetByID(t.InheritedFrom)
		if err != nil {
			l.logger.Warn("Failed to fetch parent task", "parent", t.InheritedFrom, "error", err)
		} else if parentTask != nil && len(parentTask.CreatedMemories) > 0 {
			inherited, err := l.memvid.GetByIDs(ctx, parentTask.CreatedMemories)
			if err != nil {
				l.logger.Warn("Failed to fetch inherited memories", "error", err)
			} else {
				for _, m := range inherited {
					parts = append(parts, formatMemoryForPrompt(m))
				}
			}
		}
	}

	// Fetch explicit memory refs
	if l.memvid != nil && len(t.MemoryRefs) > 0 {
		memories, err := l.memvid.GetByIDs(ctx, t.MemoryRefs)
		if err != nil {
			l.logger.Warn("Failed to fetch memory refs", "error", err)
		} else {
			for _, m := range memories {
				parts = append(parts, formatMemoryForPrompt(m))
			}
		}
	}

	// Auto-search additional context
	if l.memvid != nil && t.HasContextQuery() {
		results, err := l.memvid.Search(ctx, t.ContextQuery, 5)
		if err != nil {
			l.logger.Warn("Failed to search memory context", "error", err)
		} else {
			for _, r := range results {
				parts = append(parts, formatMemoryForPrompt(r.Memory))
			}
		}
	}

	return parts
}

// buildSystemPromptWithContext constructs system prompt with injected memory context.
func (l *AgentLoop) buildSystemPromptWithContext(contextParts []string) string {
	// Use override if set
	if l.config.SystemPromptOveride != "" {
		return l.buildSystemPromptWithOverride()
	}

	// Build from components
	builder := NewPromptBuilderFromConfig(PromptConfig{
		Constitution: l.config.Constitution,
		Restrictions: l.config.Restrictions,
		Purpose:      l.config.Purpose,
		Personality:  l.config.Personality,
	})

	// Add memory context section if present
	if len(contextParts) > 0 {
		contextSection := "## Relevant Context\n\n"
		for _, part := range contextParts {
			contextSection += "- " + part + "\n"
		}
		contextSection += "\n---\n"
		builder.AddSection("context", contextSection)
	}

	// Add tool descriptions if registry is available
	if l.registry != nil {
		tools := l.registry.List()
		for _, tool := range tools {
			builder.AddTool(ToolDescription{
				Name:        tool.Name(),
				Description: tool.Description(),
			})
		}
	}

	return builder.Build()
}

// buildTaskMessage constructs the user message from a task.
func (l *AgentLoop) buildTaskMessage(t *task.Task) string {
	var sb strings.Builder

	// Add task ID reference
	sb.WriteString(fmt.Sprintf("[Task: %s]\n\n", t.ID))

	// Add task name and description
	sb.WriteString(t.Name)
	if t.Description != "" {
		sb.WriteString("\n\n")
		sb.WriteString(t.Description)
	}

	return sb.String()
}

// recordTaskExecution stores the task execution result in memory.
func (l *AgentLoop) recordTaskExecution(ctx context.Context, t *task.Task, response string) {
	if l.memvid == nil {
		return
	}

	content := fmt.Sprintf("Task: %s\nAgent: %s\nOutcome: %s",
		t.Name,
		l.agentID,
		truncateForMemory(response, 500),
	)

	metadata := map[string]any{
		"task_id":   t.ID,
		"agent_id":  l.agentID,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	// Store in task-specific zone
	zone := "task"
	if t.MemvidZone != "" {
		zone = t.MemvidZone
	}

	taskClient := l.memvid.WithZone(zone)
	memoryID, err := taskClient.Store(ctx, content, metadata)
	if err != nil {
		l.logger.Warn("Failed to record task execution", "error", err)
		return
	}

	// Record the created memory ID
	t.AddCreatedMemory(memoryID)
	l.logger.Debug("Recorded task execution", "task", t.ID, "memory", memoryID)
}

// formatMemoryForPrompt formats a memory for inclusion in the prompt.
func formatMemoryForPrompt(m memvid.Memory) string {
	content := m.Content
	if len(content) > 300 {
		content = content[:297] + "..."
	}
	return content
}

// truncateForMemory truncates content for memory storage.
func truncateForMemory(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// executeToolCalls executes tool calls using the executor.
func (l *AgentLoop) executeToolCalls(ctx context.Context, toolCalls []llm.ToolCall) []*ExecutionResult {
	if l.executor == nil {
		// No executor configured - return errors for all tool calls
		results := make([]*ExecutionResult, len(toolCalls))
		for i, tc := range toolCalls {
			results[i] = &ExecutionResult{
				ToolCallID: tc.ID,
				Success:    false,
				Error:      "tool execution not configured",
			}
		}
		return results
	}

	return l.executor.ExecuteAll(ctx, toolCalls)
}

// buildSystemPrompt constructs the system prompt.
func (l *AgentLoop) buildSystemPrompt() string {
	// Use override if set
	if l.config.SystemPromptOveride != "" {
		return l.buildSystemPromptWithOverride()
	}

	// Build from components
	builder := NewPromptBuilderFromConfig(PromptConfig{
		Constitution: l.config.Constitution,
		Restrictions: l.config.Restrictions,
		Purpose:      l.config.Purpose,
		Personality:  l.config.Personality,
	})

	// Add tool descriptions if registry is available
	if l.registry != nil {
		tools := l.registry.List()
		for _, tool := range tools {
			builder.AddTool(ToolDescription{
				Name:        tool.Name(),
				Description: tool.Description(),
			})
		}
	}

	return builder.Build()
}

// buildSystemPromptWithOverride builds system prompt with an override.
func (l *AgentLoop) buildSystemPromptWithOverride() string {
	if l.registry == nil {
		return l.config.SystemPromptOveride
	}

	// Append tool descriptions to override
	var tools []ToolDescription
	for _, tool := range l.registry.List() {
		tools = append(tools, ToolDescription{
			Name:        tool.Name(),
			Description: tool.Description(),
		})
	}

	return BuildSystemPromptWithOverride(l.config.SystemPromptOveride, tools)
}

// publishAction publishes an agent action event.
func (l *AgentLoop) publishAction(conversationID string, iteration int, toolCalls []llm.ToolCall) {
	if l.bus == nil {
		return
	}

	calls := make([]map[string]any, len(toolCalls))
	for i, tc := range toolCalls {
		calls[i] = map[string]any{
			"name":      tc.Function.Name,
			"arguments": tc.Function.Arguments,
		}
	}

	payload := map[string]any{
		"conversation_id": conversationID,
		"iteration":       iteration,
		"tool_calls":      calls,
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "agent", payload)
	if err != nil {
		l.logger.Warn("Failed to create bus message", "error", err)
		return
	}

	l.bus.Publish("agent.action", msg)
}

// publishResult publishes an agent result event.
func (l *AgentLoop) publishResult(conversationID string, iteration int, results []*ExecutionResult) {
	if l.bus == nil {
		return
	}

	resultsData := make([]map[string]any, len(results))
	for i, r := range results {
		resultsData[i] = map[string]any{
			"tool_call_id": r.ToolCallID,
			"success":      r.Success,
			"content":      r.ToJSON(),
		}
	}

	payload := map[string]any{
		"conversation_id": conversationID,
		"iteration":       iteration,
		"results":         resultsData,
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "agent", payload)
	if err != nil {
		l.logger.Warn("Failed to create bus message", "error", err)
		return
	}

	l.bus.Publish("agent.result", msg)
}

// GetConversation returns a conversation by ID.
func (l *AgentLoop) GetConversation(id string) *Conversation {
	return l.conversations.GetIfExists(id)
}

// ClearConversation removes a conversation.
func (l *AgentLoop) ClearConversation(id string) {
	l.conversations.Delete(id)
}

// SetConfig updates the agent configuration.
func (l *AgentLoop) SetConfig(config AgentConfig) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.config = config
}

// GetConfig returns the current configuration.
func (l *AgentLoop) GetConfig() AgentConfig {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.config
}

// generateConversationID creates a new conversation ID.
func generateConversationID() string {
	return fmt.Sprintf("conv-%d", time.Now().UnixNano())
}

// Run starts the agent loop in a continuous mode, processing messages from a channel.
// This is useful for daemon mode where messages arrive asynchronously.
func (l *AgentLoop) Run(ctx context.Context, messages <-chan *AgentMessage, responses chan<- *AgentResponse) error {
	l.logger.Info("Agent loop started")
	defer l.logger.Info("Agent loop stopped")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-messages:
			if !ok {
				return nil // Channel closed
			}

			// Process the message
			response, err := l.RunOnce(ctx, msg.Content, msg.ConversationID)

			// Send response
			select {
			case responses <- &AgentResponse{
				ConversationID: msg.ConversationID,
				Content:        response,
				Error:          err,
				ReplyTo:        msg.ID,
			}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

// AgentMessage represents an incoming message to the agent.
type AgentMessage struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	Content        string `json:"content"`
	Source         string `json:"source"`
}

// AgentResponse represents the agent's response.
type AgentResponse struct {
	ConversationID string `json:"conversation_id"`
	Content        string `json:"content"`
	Error          error  `json:"error,omitempty"`
	ReplyTo        string `json:"reply_to,omitempty"`
}
