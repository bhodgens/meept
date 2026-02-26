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
	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/internal/shadow"
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
	llm          llm.Chatter // Interface for LLM operations (Client or ProviderManager)
	llmClient    *llm.Client // Concrete client for config access (may be nil if using ProviderManager)
	executor     *Executor
	registry     ToolRegistry
	security     *security.PermissionChecker
	securityOrch *intsecurity.Orchestrator
	bus          *bus.MessageBus
	logger       *slog.Logger

	// Memory for context injection
	memvid    *memvid.Client
	taskStore *task.Store

	// Shadow training for few-shot example injection
	shadowMgr *shadow.Manager

	// Learning pipeline for JUDGE/DISTILL/CONSOLIDATE
	learningPipeline LearningPipeline

	// Configuration
	config AgentConfig

	// Conversation management
	conversations *ConversationStore

	// Prompt building
	promptBuilder *PromptBuilder

	// Agent identity
	agentID string
}

// LearningPipeline is the interface for the learning pipeline.
type LearningPipeline interface {
	Judge(ctx context.Context, trajectory Trajectory) (*JudgmentResult, error)
	Distill(ctx context.Context, trajectory Trajectory, judgment *JudgmentResult) ([]*LearnedPattern, error)
	StorePattern(ctx context.Context, pattern *LearnedPattern) error
	Retrieve(ctx context.Context, query string, domain string, k int) ([]*LearnedPattern, error)
}

// Trajectory represents a sequence of actions and their outcome (for learning).
type Trajectory struct {
	ID        string
	SessionID string
	Domain    string
	Steps     []TrajectoryStep
	Outcome   TrajectoryOutcome
}

// TrajectoryStep represents a single step in a trajectory.
type TrajectoryStep struct {
	Action    string
	Input     string
	Output    string
	Success   bool
}

// TrajectoryOutcome represents the outcome of a trajectory.
type TrajectoryOutcome struct {
	Success       bool
	Quality       float64
	Feedback      string
	TaskCompleted bool
}

// JudgmentResult represents the result of evaluating a trajectory.
type JudgmentResult struct {
	Quality     float64
	ShouldLearn bool
	Reason      string
}

// LearnedPattern represents a pattern extracted from successful trajectories.
type LearnedPattern struct {
	ID          string
	Type        string
	Domain      string
	Description string
	Pattern     string
	Confidence  float64
}

// LoopOption is a functional option for configuring an AgentLoop.
type LoopOption func(*AgentLoop)

// WithLLMClient sets the LLM client (concrete type for backward compatibility).
func WithLLMClient(client *llm.Client) LoopOption {
	return func(l *AgentLoop) {
		l.llm = client
		l.llmClient = client
	}
}

// WithLLMChatter sets the LLM chatter interface (supports Client or ProviderManager).
func WithLLMChatter(chatter llm.Chatter) LoopOption {
	return func(l *AgentLoop) {
		l.llm = chatter
		// Try to extract concrete client for config access
		if client, ok := chatter.(*llm.Client); ok {
			l.llmClient = client
		}
	}
}

// WithLearningPipeline sets the learning pipeline for pattern extraction.
func WithLearningPipeline(lp LearningPipeline) LoopOption {
	return func(l *AgentLoop) {
		l.learningPipeline = lp
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

// WithShadowManager sets the shadow manager for few-shot example injection.
func WithShadowManager(mgr *shadow.Manager) LoopOption {
	return func(l *AgentLoop) {
		l.shadowMgr = mgr
	}
}

// WithSecurityOrchestrator sets the security orchestrator for input/output processing.
func WithSecurityOrchestrator(orch *intsecurity.Orchestrator) LoopOption {
	return func(l *AgentLoop) {
		l.securityOrch = orch
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
		executorOpts := []ExecutorOption{
			WithExecutorLogger(loop.logger),
		}
		if loop.agentID != "" {
			executorOpts = append(executorOpts, WithExecutorAgentID(loop.agentID))
		}
		loop.executor = NewExecutor(
			loop.registry,
			loop.security,
			executorOpts...,
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

	// Sanitize user input through security orchestrator
	sanitizedMessage := userMessage
	if l.securityOrch != nil {
		cleanText, blocked, warnings := l.securityOrch.SanitizeInput(userMessage)
		if blocked {
			l.logger.Warn("User input blocked by security",
				"conversation", conversationID,
				"warnings", len(warnings),
			)
			return "I cannot process that request due to security concerns.", nil
		}
		if len(warnings) > 0 {
			l.logger.Info("User input sanitized",
				"conversation", conversationID,
				"warnings", len(warnings),
			)
		}
		sanitizedMessage = cleanText
	}

	// Get or create conversation
	conv := l.conversations.Get(conversationID)

	// Build and set system prompt
	systemPrompt := l.buildSystemPrompt()
	conv.SetSystemPrompt(systemPrompt)

	// Add user message (sanitized)
	conv.AddUserMessage(sanitizedMessage)

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

	// Scan output through security orchestrator before returning
	finalResponse := response
	if l.securityOrch != nil {
		scannedText, hasCredentials, warnings := l.securityOrch.ScanOutput(response)
		if hasCredentials {
			l.logger.Warn("Credentials detected in output",
				"conversation", conversationID,
				"warnings", len(warnings),
			)
			finalResponse = scannedText
		}
	}

	// Trigger learning pipeline if available and response was successful
	if l.learningPipeline != nil && err == nil {
		go l.triggerLearning(context.Background(), conv, conversationID, finalResponse)
	}

	// Add final response to conversation
	conv.AddAssistantMessage(finalResponse)
	return finalResponse, nil
}

// triggerLearning runs the JUDGE/DISTILL learning pipeline asynchronously.
func (l *AgentLoop) triggerLearning(ctx context.Context, conv *Conversation, conversationID string, response string) {
	// Build trajectory from conversation
	trajectory := l.buildTrajectory(conv, conversationID, response)
	if len(trajectory.Steps) == 0 {
		return // Nothing to learn from
	}

	// Judge the trajectory
	judgment, err := l.learningPipeline.Judge(ctx, trajectory)
	if err != nil {
		l.logger.Debug("Learning judgment failed", "error", err)
		return
	}

	// Only distill if the judgment indicates we should learn
	if !judgment.ShouldLearn {
		l.logger.Debug("Trajectory not suitable for learning",
			"reason", judgment.Reason,
			"quality", judgment.Quality,
		)
		return
	}

	// Distill patterns
	patterns, err := l.learningPipeline.Distill(ctx, trajectory, judgment)
	if err != nil {
		l.logger.Debug("Learning distillation failed", "error", err)
		return
	}

	// Store learned patterns
	for _, pattern := range patterns {
		if err := l.learningPipeline.StorePattern(ctx, pattern); err != nil {
			l.logger.Debug("Failed to store pattern", "error", err)
		}
	}

	if len(patterns) > 0 {
		l.logger.Info("Learned patterns from conversation",
			"conversation", conversationID,
			"patterns", len(patterns),
		)
	}
}

// buildTrajectory constructs a trajectory from the conversation history.
func (l *AgentLoop) buildTrajectory(conv *Conversation, conversationID string, response string) Trajectory {
	messages := conv.GetMessages()

	trajectory := Trajectory{
		ID:        conversationID,
		SessionID: conversationID,
		Domain:    l.classifyDomain(messages),
		Steps:     make([]TrajectoryStep, 0),
		Outcome: TrajectoryOutcome{
			Success:       true, // We only trigger learning on success
			Quality:       0.7,  // Default quality, may be refined by Judge
			TaskCompleted: true,
		},
	}

	// Extract steps from messages
	for _, msg := range messages {
		if msg.Role == llm.RoleUser {
			trajectory.Steps = append(trajectory.Steps, TrajectoryStep{
				Action:  "user_input",
				Input:   msg.Content,
				Success: true,
			})
		} else if msg.Role == llm.RoleAssistant {
			trajectory.Steps = append(trajectory.Steps, TrajectoryStep{
				Action:  "assistant_response",
				Output:  msg.Content,
				Success: true,
			})
		}
	}

	return trajectory
}

// classifyDomain determines the domain of a conversation based on content.
func (l *AgentLoop) classifyDomain(messages []llm.ChatMessage) string {
	var text string
	for _, msg := range messages {
		text += " " + msg.Content
	}

	// Simple keyword-based classification
	codeKeywords := []string{"code", "function", "class", "variable", "bug", "compile", "syntax"}
	planningKeywords := []string{"plan", "step", "strategy", "approach", "design"}
	debuggingKeywords := []string{"debug", "fix", "issue", "problem", "crash", "error"}

	if containsAnyKeyword(text, codeKeywords) {
		return "code"
	} else if containsAnyKeyword(text, debuggingKeywords) {
		return "debugging"
	} else if containsAnyKeyword(text, planningKeywords) {
		return "planning"
	}
	return "general"
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

		// Get messages for LLM
		messages := conv.GetMessages()

		// Inject few-shot examples from shadow training (only on first iteration)
		if iteration == 1 && l.shadowMgr != nil && l.shadowMgr.IsEnabled() {
			messages = l.injectFewShotExamples(ctx, messages, conversationID)
		}

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

			// Capture tool-use interaction for shadow training
			if l.shadowMgr != nil && l.shadowMgr.IsEnabled() {
				modelID := ""
				if l.llmClient != nil {
					modelID = l.llmClient.Config().ModelID
				}
				go l.shadowMgr.CaptureToolInteraction(
					context.Background(),
					conversationID,
					messages,
					response,
					modelID,
				)
			}

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

		// Capture interaction for shadow training
		if l.shadowMgr != nil && l.shadowMgr.IsEnabled() {
			modelID := ""
			if l.llmClient != nil {
				modelID = l.llmClient.Config().ModelID
			}
			go l.shadowMgr.CaptureInteraction(
				context.Background(),
				conversationID,
				messages,
				response,
				modelID,
			)
		}

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

// injectFewShotExamples retrieves and injects relevant few-shot examples into messages.
func (l *AgentLoop) injectFewShotExamples(ctx context.Context, messages []llm.ChatMessage, conversationID string) []llm.ChatMessage {
	if l.shadowMgr == nil {
		return messages
	}

	// Extract query from the last user message
	var query string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llm.RoleUser {
			query = messages[i].Content
			break
		}
	}
	if query == "" {
		return messages
	}

	// Classify domain and task type based on message content
	domain, taskType := l.classifyForShadow(messages)

	// Get relevant few-shot examples
	examples, err := l.shadowMgr.GetFewShotExamples(ctx, domain, taskType, query, 3)
	if err != nil {
		l.logger.Warn("Failed to get few-shot examples", "error", err)
		return messages
	}
	if len(examples) == 0 {
		return messages
	}

	// Format examples for injection
	exampleMessages := l.shadowMgr.FormatExamplesForInjection(examples)
	if len(exampleMessages) == 0 {
		return messages
	}

	// Convert shadow.Message to llm.ChatMessage
	exampleChatMessages := make([]llm.ChatMessage, len(exampleMessages))
	for i, msg := range exampleMessages {
		exampleChatMessages[i] = llm.ChatMessage{
			Role:    llm.Role(msg.Role),
			Content: msg.Content,
		}
	}

	// Inject after system prompt
	// Find position after system messages
	insertPos := 0
	for i, msg := range messages {
		if msg.Role == llm.RoleSystem {
			insertPos = i + 1
		} else {
			break
		}
	}

	// Build new messages slice with examples injected
	result := make([]llm.ChatMessage, 0, len(messages)+len(exampleChatMessages))
	result = append(result, messages[:insertPos]...)
	result = append(result, exampleChatMessages...)
	result = append(result, messages[insertPos:]...)

	l.logger.Debug("Injected few-shot examples",
		"count", len(examples),
		"conversation", conversationID,
	)

	return result
}

// classifyForShadow classifies messages for shadow training example retrieval.
func (l *AgentLoop) classifyForShadow(messages []llm.ChatMessage) (shadow.Domain, shadow.TaskType) {
	var text string
	for _, msg := range messages {
		text += " " + msg.Content
	}

	// Simple keyword-based classification
	codeKeywords := []string{"code", "function", "class", "variable", "bug", "error", "compile", "syntax", "import", "package"}
	planningKeywords := []string{"plan", "step", "first", "then", "next", "strategy", "approach", "design", "architecture"}
	debuggingKeywords := []string{"debug", "fix", "issue", "problem", "crash", "stack trace", "exception", "traceback"}
	analysisKeywords := []string{"analyze", "explain", "why", "how does", "what is", "understand", "review"}

	domain := shadow.DomainGeneral
	if containsAnyKeyword(text, codeKeywords) {
		domain = shadow.DomainCode
	} else if containsAnyKeyword(text, debuggingKeywords) {
		domain = shadow.DomainDebugging
	} else if containsAnyKeyword(text, planningKeywords) {
		domain = shadow.DomainPlanning
	} else if containsAnyKeyword(text, analysisKeywords) {
		domain = shadow.DomainAnalysis
	}

	taskType := shadow.TaskTypeChat
	multiStepKeywords := []string{"step by step", "first", "second", "then", "finally", "multiple steps"}
	reasoningKeywords := []string{"think", "reason", "consider", "analyze", "evaluate", "compare"}

	if containsAnyKeyword(text, multiStepKeywords) {
		taskType = shadow.TaskTypeMultiStep
	} else if containsAnyKeyword(text, reasoningKeywords) {
		taskType = shadow.TaskTypeReasoning
	}

	return domain, taskType
}

// containsAnyKeyword checks if text contains any of the keywords.
func containsAnyKeyword(text string, keywords []string) bool {
	lower := strings.ToLower(text)
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
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

// SetMemvidClient sets the memvid client after construction.
// This allows wiring the client after the loop is created when
// dependencies are initialized in a specific order.
func (l *AgentLoop) SetMemvidClient(client *memvid.Client) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.memvid = client
}

// SetTaskStore sets the task store after construction.
// This allows wiring the store after the loop is created when
// dependencies are initialized in a specific order.
func (l *AgentLoop) SetTaskStore(store *task.Store) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.taskStore = store
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
