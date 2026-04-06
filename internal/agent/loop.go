package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/agent/prompts"
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
	DefaultMaxIterations = 25
	DefaultTimeout       = 5 * time.Minute
)

// Error types for the agent loop.
var (
	ErrMaxIterationsReached          = errors.New("maximum iterations reached")
	ErrContextCancelled              = errors.New("context cancelled")
	ErrNoLLMClient                   = errors.New("no LLM client configured")
	ErrCycleDetected                 = errors.New("agent detected a cycle in tool calls")
	ErrConvergenceDetected           = errors.New("agent responses converged without progress")
	ErrConversationBudgetExhausted   = errors.New("conversation token budget exhausted")
)

// DetectionConfig holds configuration for cycle and convergence detection.
type DetectionConfig struct {
	// CycleDetection: minimum consecutive similar tool calls to trigger
	CycleThreshold int

	// ConvergenceDetection: minimum consecutive similar responses to trigger
	ConvergenceThreshold int

	// HistorySize: how many iterations to keep in history
	HistorySize int
}

// DefaultDetectionConfig returns sensible detection defaults.
func DefaultDetectionConfig() DetectionConfig {
	return DetectionConfig{
		CycleThreshold:       3, // 3 similar tool calls in a row
		ConvergenceThreshold: 3, // 3 similar responses in a row
		HistorySize:          10,
	}
}

// cycleDetector tracks tool calls to detect repeated patterns.
type cycleDetector struct {
	mu       sync.Mutex
	history  []toolCallSignature
	config   DetectionConfig
	logger   *slog.Logger
	lastWarn time.Time
}

// toolCallSignature represents a simplified tool call for cycle detection.
type toolCallSignature struct {
	tool       string
	argHash    string // hash of arguments
	timestamp  time.Time
}

// newCycleDetector creates a new cycle detector.
func newCycleDetector(config DetectionConfig, logger *slog.Logger) *cycleDetector {
	return &cycleDetector{
		history: make([]toolCallSignature, 0, config.HistorySize),
		config:  config,
		logger:  logger,
	}
}

// recordCall records a tool call and checks for cycles.
// Returns true if a cycle was detected.
func (cd *cycleDetector) recordCall(tool string, argsJSON string) bool {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	// Create argument signature
	argHash := hashArgs(argsJSON)
	sig := toolCallSignature{
		tool:      tool,
		argHash:   argHash,
		timestamp: time.Now(),
	}

	// Add to history
	cd.history = append(cd.history, sig)
	if len(cd.history) > cd.config.HistorySize {
		cd.history = cd.history[1:]
	}

	// Check for cycles: look for consecutive similar calls
	return cd.detectCycle()
}

// detectCycle checks if we have consecutive similar tool calls.
func (cd *cycleDetector) detectCycle() bool {
	if len(cd.history) < cd.config.CycleThreshold {
		return false
	}

	// Check last N calls for similarity
	recent := cd.history[len(cd.history)-cd.config.CycleThreshold:]

	// All must be same tool with same args
	firstTool := recent[0].tool
	firstArgs := recent[0].argHash

	for i := 1; i < len(recent); i++ {
		if recent[i].tool != firstTool || recent[i].argHash != firstArgs {
			return false
		}
	}

	// Rate limit warnings
	if time.Since(cd.lastWarn) > 30*time.Second {
		cd.logger.Warn("Cycle detected in tool calls",
			"tool", firstTool,
			"args_hash", firstArgs[:8],
			"count", len(recent),
		)
		cd.lastWarn = time.Now()
	}

	return true
}

// convergenceDetector tracks LLM responses to detect stagnation.
type convergenceDetector struct {
	mu       sync.Mutex
	history  []responseSignature
	config   DetectionConfig
	logger   *slog.Logger
	lastWarn time.Time
}

// responseSignature represents a simplified LLM response for convergence detection.
type responseSignature struct {
	contentHash string // hash of trimmed, lowercased content
	hasTools    bool
	timestamp   time.Time
}

// newConvergenceDetector creates a new convergence detector.
func newConvergenceDetector(config DetectionConfig, logger *slog.Logger) *convergenceDetector {
	return &convergenceDetector{
		history: make([]responseSignature, 0, config.HistorySize),
		config:  config,
		logger:  logger,
	}
}

// recordResponse records an LLM response and checks for convergence.
// Returns true if convergence was detected (without tool calls).
func (cd *convergenceDetector) recordResponse(content string, hasTools bool) bool {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	// Normalize and hash content
	normalized := normalizeContent(content)
	contentHash := hashString(normalized)

	sig := responseSignature{
		contentHash: contentHash,
		hasTools:    hasTools,
		timestamp:   time.Now(),
	}

	// Add to history
	cd.history = append(cd.history, sig)
	if len(cd.history) > cd.config.HistorySize {
		cd.history = cd.history[1:]
	}

	// Only check convergence if no tools are being used
	// (responses with tools are expected to vary)
	if hasTools {
		return false
	}

	return cd.detectConvergence()
}

// detectConvergence checks if responses are converging without progress.
func (cd *convergenceDetector) detectConvergence() bool {
	if len(cd.history) < cd.config.ConvergenceThreshold {
		return false
	}

	// Check last N responses
	recent := cd.history[len(cd.history)-cd.config.ConvergenceThreshold:]

	// All must have no tools and similar content
	firstHash := recent[0].contentHash

	for i := 1; i < len(recent); i++ {
		if recent[i].hasTools || recent[i].contentHash != firstHash {
			return false
		}
	}

	// Rate limit warnings
	if time.Since(cd.lastWarn) > 30*time.Second {
		cd.logger.Warn("Convergence detected in responses",
			"content_hash", firstHash[:8],
			"count", len(recent),
		)
		cd.lastWarn = time.Now()
	}

	return true
}

// hashArgs creates a hash of tool arguments for comparison.
// Accepts JSON string arguments directly.
func hashArgs(argsJSON string) string {
	if argsJSON == "" || argsJSON == "{}" {
		return "empty"
	}

	// Normalize JSON: remove extra whitespace
	normalized := strings.TrimSpace(argsJSON)

	// For simple comparison, we can hash the normalized JSON directly
	// Most LLMs produce deterministic JSON for the same arguments
	return hashString(normalized)
}

// normalizeContent normalizes response content for comparison.
func normalizeContent(content string) string {
	// Trim, lowercase, remove extra whitespace
	content = strings.TrimSpace(content)
	content = strings.ToLower(content)

	// Collapse multiple spaces
	words := strings.Fields(content)
	return strings.Join(words, " ")
}

// hashString creates a SHA256 hash of a string.
func hashString(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))[:16] // First 16 chars is enough
}


// AgentConfig holds configuration for the agent loop.
type AgentConfig struct {
	MaxIterations            int
	Timeout                  time.Duration
	Constitution             string
	Restrictions             string
	Purpose                  string
	Personality              string
	SystemPromptOveride      string
	MaxConversationTokens    int // 0 means use DefaultConversationTokenBudget
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
	resolver     *llm.Resolver // Model resolver for alias resolution
	modelRef     string        // Model reference from agent spec (can be alias or direct ref)
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

	// Result cache for tool outputs
	cache *ResultCache

	// Progress tracking
	progressEnabled  bool          // Enable/disable progress events
	progressInterval time.Duration // Minimum interval between progress events (reserved for future use)

	// Configuration
	config          AgentConfig
	detectionConfig DetectionConfig

	// Cycle and convergence detection
	cycleDetector       *cycleDetector
	convergenceDetector *convergenceDetector

	// Conversation management
	conversations *ConversationStore

	// Prompt building
	promptBuilder *PromptBuilder

	// Claude artifacts integration
	artifactManager *ArtifactManager

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

// WithResolver sets the model resolver for alias resolution.
func WithResolver(resolver *llm.Resolver) LoopOption {
	return func(l *AgentLoop) {
		l.resolver = resolver
	}
}

// WithModelRef sets the model reference (alias name or direct model ref) from the agent spec.
func WithModelRef(modelRef string) LoopOption {
	return func(l *AgentLoop) {
		l.modelRef = modelRef
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

// WithResultCache sets the result cache for the agent loop.
func WithResultCache(cache *ResultCache) LoopOption {
	return func(l *AgentLoop) {
		l.cache = cache
	}
}

// WithProgressEnabled enables or disables progress event publishing.
func WithProgressEnabled(enabled bool) LoopOption {
	return func(l *AgentLoop) {
		l.progressEnabled = enabled
	}
}

// WithProgressInterval sets the minimum interval between progress events.
// Reserved for future use to throttle high-frequency progress updates.
func WithProgressInterval(interval time.Duration) LoopOption {
	return func(l *AgentLoop) {
		l.progressInterval = interval
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
		config:           DefaultAgentConfig(),
		detectionConfig:  DefaultDetectionConfig(),
		conversations:    NewConversationStore(100),
		logger:           slog.Default(),
	}

	for _, opt := range opts {
		opt(loop)
	}

	// Initialize detectors
	loop.cycleDetector = newCycleDetector(loop.detectionConfig, loop.logger)
	loop.convergenceDetector = newConvergenceDetector(loop.detectionConfig, loop.logger)

	// Create executor if we have a registry
	if loop.registry != nil {
		executorOpts := []ExecutorOption{
			WithExecutorLogger(loop.logger),
		}
		if loop.agentID != "" {
			executorOpts = append(executorOpts, WithExecutorAgentID(loop.agentID))
		}
		if loop.cache != nil {
			executorOpts = append(executorOpts, WithExecutorCache(loop.cache))
			loop.logger.Debug("Wired result cache to executor")
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

// Token budget constants for context management
const (
	// IterationTokenBudget is the maximum tokens to send per LLM iteration
	// This prevents context explosion across multiple iterations
	IterationTokenBudget = 30000

	// ToolResultMaxTokens is the maximum tokens per tool result
	// Large tool outputs are compressed to fit this limit
	ToolResultMaxTokens = 3000

	// DefaultConversationTokenBudget is the total token budget for a single
	// conversation turn across all iterations. When exceeded, the agent
	// stops gracefully and returns what it has so far.
	DefaultConversationTokenBudget = 50000

	// ConversationBudgetWarningRatio is the fraction of the conversation
	// budget at which the agent starts wrapping up (skips new tool calls).
	ConversationBudgetWarningRatio = 0.80
)

// conversationTokenBudget returns the effective conversation token budget.
func (l *AgentLoop) conversationTokenBudget() int {
	if l.config.MaxConversationTokens > 0 {
		return l.config.MaxConversationTokens
	}
	return DefaultConversationTokenBudget
}

// reasoningCycle runs the main reasoning loop with tool execution.
func (l *AgentLoop) reasoningCycle(ctx context.Context, conv *Conversation, conversationID string) (string, error) {
	var totalTokens int
	convBudget := l.conversationTokenBudget()
	inWarningZone := false

	for iteration := 1; iteration <= l.config.MaxIterations; iteration++ {
		select {
		case <-ctx.Done():
			return "", ErrContextCancelled
		default:
		}

		// Check conversation token budget
		if totalTokens >= convBudget {
			l.logger.Warn("Conversation token budget exhausted",
				"total_tokens", totalTokens,
				"budget", convBudget,
				"conversation", conversationID,
			)
			return "I've used my full token budget for this request. Here is what I accomplished so far -- " +
				"please let me know if you'd like me to continue in a follow-up.", ErrConversationBudgetExhausted
		}

		// Warning zone: at 80% of budget, prepare to wrap up
		if !inWarningZone && float64(totalTokens) >= float64(convBudget)*ConversationBudgetWarningRatio {
			inWarningZone = true
			l.logger.Info("Approaching conversation token budget",
				"total_tokens", totalTokens,
				"budget", convBudget,
				"conversation", conversationID,
			)
		}

		l.logger.Debug("Agent loop iteration",
			"iteration", iteration,
			"max", l.config.MaxIterations,
			"conversation", conversationID,
		)

		// Publish progress: thinking
		l.publishProgress(conversationID, iteration, "thinking", "", totalTokens)

		// Get tool definitions
		var tools []llm.ToolDefinition
		if l.registry != nil {
			tools = l.registry.GetDefinitions()
		}

		// Enforce token budget before LLM call to prevent context explosion.
		// Reserve space for tool definitions (~175 tokens per tool) which are
		// sent alongside messages but not counted by TruncateByTokens.
		toolOverhead := len(tools) * 175
		effectiveBudget := IterationTokenBudget - toolOverhead
		if effectiveBudget < 2000 {
			effectiveBudget = 2000 // minimum budget for messages
		}
		removed := conv.TruncateByTokens(effectiveBudget)
		if removed > 0 {
			l.logger.Debug("Truncated conversation for token budget",
				"removed", removed,
				"budget", effectiveBudget,
				"tool_overhead", toolOverhead,
				"conversation", conversationID,
			)
		}

		// Get messages for LLM with windowed context to prevent token explosion
		// This preserves system prompt, original user message, and recent context
		// Uses the same effective budget that accounts for tool definition overhead
		messages := conv.GetWindowedMessages(effectiveBudget)

		// Inject few-shot examples from shadow training (only on first iteration)
		if iteration == 1 && l.shadowMgr != nil && l.shadowMgr.IsEnabled() {
			messages = l.injectFewShotExamples(ctx, messages, conversationID)
		}

		var chatOpts []llm.ChatOption
		// In warning zone, don't send tools so the LLM produces a final text response
		if len(tools) > 0 && !inWarningZone {
			chatOpts = append(chatOpts, llm.WithTools(tools))
		}
		if inWarningZone {
			// Inject wrap-up instruction so the LLM summarizes without further tool use
			messages = append(messages, llm.ChatMessage{
				Role:    llm.RoleUser,
				Content: "[system: you are approaching your token budget. provide a final summary of what you've accomplished and any remaining work, without making additional tool calls.]",
			})
		}

		// Resolve alias to get the current model and switch the LLM client
		if l.modelRef != "" && l.resolver != nil && l.resolver.HasAlias(l.modelRef) {
			modelConfig, err := l.resolver.ResolveForAlias(l.modelRef)
			if err != nil {
				l.logger.Warn("Alias resolution failed, using default",
					"alias", l.modelRef,
					"error", err,
				)
			} else if l.llmClient != nil {
				// Switch the LLM client to the resolved model
				l.llmClient.SwitchModel(modelConfig)
				l.logger.Debug("Switched to alias model",
					"alias", l.modelRef,
					"model", modelConfig.ModelID,
				)
			}
		}

		response, err := l.chatWithFailover(ctx, messages, chatOpts...)
		if err != nil {
			l.logger.Error("LLM call failed",
				"iteration", iteration,
				"error", err,
			)
			return "", fmt.Errorf("LLM call failed: %w", err)
		}
		// Track token usage
		totalTokens += response.Usage.TotalTokens

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

			// Build tool names for progress
			var toolNames string
			for i, tc := range response.ToolCalls {
				if i > 0 {
					toolNames += ", "
				}
				toolNames += tc.Function.Name
			}

			// Publish progress: executing tools
			l.publishProgress(conversationID, iteration, "executing", toolNames, totalTokens)

			// Execute tools
			results := l.executeToolCalls(ctx, response.ToolCalls)

			// Record tool calls for cycle detection
			for _, tc := range response.ToolCalls {
				if l.cycleDetector.recordCall(tc.Function.Name, tc.Function.Arguments) {
					// Cycle detected - abort with helpful message
					l.logger.Warn("Cycle detected, aborting loop",
						"iteration", iteration,
						"tool", tc.Function.Name,
					)
					exhaustMsg := fmt.Sprintf("I detected I was repeating the same action (%s) and stopped to avoid getting stuck. "+
						"Please provide more specific guidance or clarify what you'd like me to do.", tc.Function.Name)
					return exhaustMsg, ErrCycleDetected
				}
			}

			// Add tool results to conversation with adaptive compression.
			// As we consume more of the conversation budget, compress tool results more aggressively.
			dynamicToolBudget := ToolResultMaxTokens
			if convBudget > 0 && totalTokens > 0 {
				ratio := 1.0 - float64(totalTokens)/float64(convBudget)
				if ratio < 0 {
					ratio = 0
				}
				dynamicToolBudget = int(float64(ToolResultMaxTokens) * ratio)
				if dynamicToolBudget < 600 {
					dynamicToolBudget = 600 // minimum readable result size
				}
			}
			for _, result := range results {
				conv.AddToolResult(result.ToolCallID, result.ToCompressedJSON(dynamicToolBudget))
			}

			// Publish agent result event
			l.publishResult(conversationID, iteration, results)

			// Continue loop for LLM to process tool results
			continue
		}

		// Record response for convergence detection
		if l.convergenceDetector.recordResponse(response.Content, false) {
			// Convergence detected - abort with helpful message
			l.logger.Warn("Convergence detected, aborting loop",
				"iteration", iteration,
			)
			exhaustMsg := "I noticed my responses were converging without making new progress. " +
				"Please provide more specific guidance or clarify what you'd like me to do."
			return exhaustMsg, ErrConvergenceDetected
		}

		// Case 2: LLM returned text response (no tool calls) - done
		l.logger.Info("Agent loop complete",
			"iterations", iteration,
			"conversation", conversationID,
		)

		// Publish progress: complete
		l.publishProgress(conversationID, iteration, "complete", "", totalTokens)

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

// chatWithFailover wraps LLM Chat calls with model rotation and backoff for rate limit handling.
// When a rate limit error occurs:
// 1. If there are more models in the alias, rotate to the next model and retry immediately.
// 2. If all models exhausted or only one model, apply exponential backoff and retry same model.
// 3. After max attempts, return the error.
func (l *AgentLoop) chatWithFailover(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	const maxAttempts = 5
	const maxBackoff = 30 * time.Second
	baseBackoff := 2 * time.Second

	attempt := 0
	currentBackoff := baseBackoff

	for {
		attempt++

		// Resolve model for this attempt
		if l.modelRef != "" && l.resolver != nil && l.resolver.HasAlias(l.modelRef) {
			modelConfig, err := l.resolver.ResolveForAlias(l.modelRef)
			if err != nil {
				l.logger.Warn("Alias resolution failed",
					"alias", l.modelRef,
					"attempt", attempt,
					"error", err,
				)
				// If all models in alias exhausted, apply backoff
				if attempt < maxAttempts {
					l.logger.Info("Waiting before retry due to exhausted alias",
						"backoff", currentBackoff,
						"attempt", attempt,
					)
					select {
					case <-time.After(currentBackoff):
						currentBackoff = time.Duration(float64(currentBackoff) * 2)
						if currentBackoff > maxBackoff {
							currentBackoff = maxBackoff
						}
						continue
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				return nil, err
			}
			if l.llmClient != nil {
				l.llmClient.SwitchModel(modelConfig)
			}
		}

		// Make the LLM call
		response, err := l.llm.Chat(ctx, messages, opts...)
		if err == nil {
			// Success - record it and return
			if l.modelRef != "" && l.resolver != nil && l.resolver.HasAlias(l.modelRef) {
				l.resolver.RecordAliasSuccess(l.modelRef)
			}
			return response, nil
		}

		// Check if it's a rate limit error
		var rateLimitErr *llm.RateLimitError
		if errors.As(err, &rateLimitErr) {
			l.logger.Warn("Rate limit hit, handling with backoff",
				"provider", rateLimitErr.ProviderID,
				"model", rateLimitErr.ModelID,
				"retry_after", rateLimitErr.RetryAfter,
				"attempt", attempt,
			)

			// Record failure for this alias
			if l.modelRef != "" && l.resolver != nil {
				l.resolver.RecordAliasFailure(l.modelRef, err)
			}

			// Check if we can rotate to another model
			if l.modelRef != "" && l.resolver != nil && l.resolver.HasAlias(l.modelRef) {
				// Try to rotate to next model
				_, rotateErr := l.resolver.RotateToNextModel(l.modelRef)
				if rotateErr == nil {
					l.logger.Info("Rotated to next model after rate limit",
						"alias", l.modelRef,
						"attempt", attempt,
					)
					// Retry immediately with the new model
					continue
				}
				l.logger.Warn("Failed to rotate model, applying backoff",
					"error", rotateErr,
				)
			}

			// No more models to rotate to, apply backoff
			if attempt >= maxAttempts {
				return nil, fmt.Errorf("max retry attempts (%d) reached for rate limit: %w", maxAttempts, err)
			}

			// Use Retry-After header if available, otherwise use computed backoff
			waitTime := currentBackoff
			if rateLimitErr.RetryAfter > 0 && rateLimitErr.RetryAfter < maxBackoff {
				waitTime = rateLimitErr.RetryAfter
			}

			l.logger.Info("Waiting before retry due to rate limit",
				"backoff", waitTime,
				"attempt", attempt,
			)

			select {
			case <-time.After(waitTime):
				// Increase backoff for next attempt
				currentBackoff = time.Duration(float64(currentBackoff) * 2)
				if currentBackoff > maxBackoff {
					currentBackoff = maxBackoff
				}
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		// Non-rate-limit error - return immediately
		if l.modelRef != "" && l.resolver != nil && l.resolver.HasAlias(l.modelRef) {
			l.resolver.RecordAliasFailure(l.modelRef, err)
		}
		return nil, err
	}
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

	// Add baseline capabilities and platform introspection guidelines
	builder.AddSection("Platform Capabilities", prompts.BaselineCapabilities)
	builder.AddSection("Platform Guidelines", prompts.BaselineGuidelines)

	// Add memory context section if present
	if len(contextParts) > 0 {
		contextSection := "## Relevant Context\n\n"
		for _, part := range contextParts {
			contextSection += "- " + part + "\n"
		}
		contextSection += "\n---\n"
		builder.AddSection("context", contextSection)
	}

	// Tool descriptions are omitted from the system prompt because they are
	// already sent via the API's tools parameter, avoiding duplication.

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

	// Add baseline capabilities and platform introspection guidelines
	builder.AddSection("Platform Capabilities", prompts.BaselineCapabilities)
	builder.AddSection("Platform Guidelines", prompts.BaselineGuidelines)

	// Tool descriptions are omitted from the system prompt because they are
	// already sent via the API's tools parameter, avoiding duplication.

	return builder.Build()
}

// buildSystemPromptWithOverride builds system prompt with an override.
// Tool descriptions are omitted because they are sent via the API's tools parameter.
func (l *AgentLoop) buildSystemPromptWithOverride() string {
	return l.config.SystemPromptOveride
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

// publishProgress publishes a progress event to the message bus.
func (l *AgentLoop) publishProgress(conversationID string, iteration int, stage string, detail string, tokenCount int) {
	// Skip if progress disabled or no bus
	if !l.progressEnabled || l.bus == nil {
		l.logger.Debug("Progress skipped", "enabled", l.progressEnabled, "bus_nil", l.bus == nil)
		return
	}

	l.logger.Info("Publishing progress event",
		"conversation", conversationID,
		"iteration", iteration,
		"stage", stage,
		"detail", detail,
		"tokens", tokenCount,
	)

	payload := map[string]any{
		"conversation_id": conversationID,
		"iteration":       iteration,
		"stage":           stage,
		"detail":          detail,
		"token_count":     tokenCount,
		"timestamp":       time.Now().UTC().Format(time.RFC3339),
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "agent", payload)
	if err != nil {
		l.logger.Warn("Failed to create progress bus message", "error", err)
		return
	}

	// Publish - don't care if nobody is listening
	delivered := l.bus.Publish("agent.progress", msg)
	if delivered == 0 {
		l.logger.Debug("Progress event published (no subscribers)", "stage", stage)
	}
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

// getAliasName extracts the alias name from a model reference.
// Returns empty string if not an alias.
func (l *AgentLoop) getAliasName(modelRef string) string {
	if modelRef == "" {
		return ""
	}
	if l.resolver == nil {
		return ""
	}
	// Check if it's a known alias
	if l.resolver.HasAlias(modelRef) {
		return modelRef
	}
	return ""
}

// resolveAliasModel resolves an alias to a specific model config.
// Returns nil if no alias or resolution fails.
func (l *AgentLoop) resolveAliasModel(aliasName string) *llm.ModelConfig {
	if aliasName == "" || l.resolver == nil {
		return nil
	}
	modelConfig, err := l.resolver.ResolveForAlias(aliasName)
	if err != nil {
		l.logger.Warn("Alias resolution failed", "alias", aliasName, "error", err)
		return nil
	}
	return modelConfig
}

// recordAliasFailure records a failure for the current model alias.
func (l *AgentLoop) recordAliasFailure(modelRef string, err error) {
	aliasName := l.getAliasName(modelRef)
	if aliasName != "" && l.resolver != nil {
		l.resolver.RecordAliasFailure(aliasName, err)
	}
}

// recordAliasSuccess records a success for the current model alias.
func (l *AgentLoop) recordAliasSuccess(modelRef string) {
	aliasName := l.getAliasName(modelRef)
	if aliasName != "" && l.resolver != nil {
		l.resolver.RecordAliasSuccess(aliasName)
	}
}
