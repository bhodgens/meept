package shadow

import (
	"context"
	"log/slog"
	"math/rand"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/llm"
)

// ChatterWithConfig extends the basic chat interface with configuration access.
// This differs from llm.Chatter by requiring Config() instead of ChatWithProgress().
type ChatterWithConfig interface {
	Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error)
	Config() *llm.ModelConfig
}

// Middleware wraps an LLM client to intercept and shadow requests.
type Middleware struct {
	client  ChatterWithConfig
	manager *Manager
	config  *Config
	logger  *slog.Logger

	// For async shadowing
	shadowQueue chan *shadowRequest
	wg          sync.WaitGroup
}

type shadowRequest struct {
	ctx            context.Context
	conversationID string
	messages       []llm.ChatMessage
	response       *llm.Response
	domain         Domain
	taskType       TaskType
}

// MiddlewareOption is a functional option for configuring Middleware.
type MiddlewareOption func(*Middleware)

// WithMiddlewareLogger sets the logger.
func WithMiddlewareLogger(logger *slog.Logger) MiddlewareOption {
	return func(m *Middleware) {
		m.logger = logger
	}
}

// NewMiddleware creates a new shadow middleware.
func NewMiddleware(client ChatterWithConfig, manager *Manager, config *Config, opts ...MiddlewareOption) *Middleware {
	m := &Middleware{
		client:      client,
		manager:     manager,
		config:      config,
		logger:      slog.Default(),
		shadowQueue: make(chan *shadowRequest, config.Shadowing.QueueSize),
	}

	for _, opt := range opts {
		opt(m)
	}

	// Start background workers for async mode
	if config.Shadowing.Mode == ModeAsync {
		m.startWorkers(config.Shadowing.WorkerCount)
	}

	return m
}

// Chat forwards the chat request to the underlying client and optionally shadows it.
func (m *Middleware) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	// Forward to the actual client
	response, err := m.client.Chat(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}

	// Check if we should shadow this request
	if !m.shouldShadow(ctx, messages) {
		return response, nil
	}

	// Get context metadata
	domain := m.classifyDomain(messages)
	taskType := m.classifyTaskType(messages, response)
	convID := getConversationID(ctx)

	switch m.config.Shadowing.Mode {
	case ModeSync:
		// Synchronous shadowing - wait for teacher response
		m.shadowSync(ctx, convID, messages, response, domain, taskType)

	case ModeAsync, ModeSelective:
		// Asynchronous shadowing - queue for background processing
		m.queueShadow(convID, messages, response, domain, taskType)
	}

	return response, nil
}

// Config returns the underlying client's configuration.
func (m *Middleware) Config() *llm.ModelConfig {
	return m.client.Config()
}

// Close stops background workers and waits for them to finish.
func (m *Middleware) Close() {
	close(m.shadowQueue)
	m.wg.Wait()
}

func (m *Middleware) shouldShadow(_ context.Context, messages []llm.ChatMessage) bool {
	if !m.config.IsEnabled() {
		return false
	}

	cfg := m.config.Shadowing

	// Check mode-specific filters for selective mode
	if cfg.Mode == ModeSelective {
		// Sample rate check
		if cfg.SampleRate < 1.0 && rand.Float64() > cfg.SampleRate {
			return false
		}
	}

	// Check domain and task type filters
	domain := m.classifyDomain(messages)
	taskType := m.classifyTaskType(messages, nil)
	complexity := m.estimateComplexity(messages)

	return m.config.ShouldShadow(string(domain), string(taskType), complexity)
}

func (m *Middleware) shadowSync(ctx context.Context, convID string, messages []llm.ChatMessage, response *llm.Response, domain Domain, taskType TaskType) {
	// Convert llm.ChatMessage to shadow.Message
	shadowMessages := make([]Message, len(messages))
	for i, msg := range messages {
		shadowMessages[i] = Message{
			Role:    string(msg.Role),
			Content: msg.Content,
		}
	}

	// Create shadow record
	record := NewShadowRecord(convID, shadowMessages, m.client.Config().ModelID, response.Content)
	record.StudentTokensIn = response.Usage.PromptTokens
	record.StudentTokensOut = response.Usage.CompletionTokens
	record.Domain = domain
	record.TaskType = taskType

	// Get teacher response
	teacherContent, teacherModel, err := m.manager.GetTeacherResponse(ctx, messages)
	if err != nil {
		m.logger.Warn("Failed to get teacher response", "error", err)
	} else {
		record.TeacherModel = teacherModel
		record.TeacherContent = teacherContent
	}

	// Score and store
	if err := m.manager.ProcessRecord(ctx, record); err != nil {
		m.logger.Error("Failed to process shadow record", "error", err)
	}
}

func (m *Middleware) queueShadow(convID string, messages []llm.ChatMessage, response *llm.Response, domain Domain, taskType TaskType) {
	req := &shadowRequest{
		ctx:            context.Background(), // Use background context for async processing
		conversationID: convID,
		messages:       messages,
		response:       response,
		domain:         domain,
		taskType:       taskType,
	}

	select {
	case m.shadowQueue <- req:
		// Queued successfully
	default:
		m.logger.Warn("Shadow queue full, dropping request")
	}
}

func (m *Middleware) startWorkers(count int) {
	for range count {
		m.wg.Add(1)
		go m.worker()
	}
}

func (m *Middleware) worker() {
	defer m.wg.Done()

	for req := range m.shadowQueue {
		m.processShadowRequest(req)
	}
}

func (m *Middleware) processShadowRequest(req *shadowRequest) {
	// Convert llm.ChatMessage to shadow.Message
	shadowMessages := make([]Message, len(req.messages))
	for i, msg := range req.messages {
		shadowMessages[i] = Message{
			Role:    string(msg.Role),
			Content: msg.Content,
		}
	}

	// Create shadow record
	record := NewShadowRecord(req.conversationID, shadowMessages, m.client.Config().ModelID, req.response.Content)
	record.StudentTokensIn = req.response.Usage.PromptTokens
	record.StudentTokensOut = req.response.Usage.CompletionTokens
	record.Domain = req.domain
	record.TaskType = req.taskType

	// Get teacher response
	teacherContent, teacherModel, err := m.manager.GetTeacherResponse(req.ctx, req.messages)
	if err != nil {
		m.logger.Warn("Failed to get teacher response", "error", err)
	} else {
		record.TeacherModel = teacherModel
		record.TeacherContent = teacherContent
	}

	// Score and store
	if err := m.manager.ProcessRecord(req.ctx, record); err != nil {
		m.logger.Error("Failed to process shadow record", "error", err)
	}
}

func (m *Middleware) classifyDomain(messages []llm.ChatMessage) Domain {
	// Simple keyword-based classification
	// In production, could use a classifier model

	var text strings.Builder
	for _, msg := range messages {
		text.WriteString(" " + msg.Content)
	}

	codeKeywords := []string{"code", "function", "class", "variable", "bug", "error", "compile", "syntax", "import", "package"}
	planningKeywords := []string{"plan", "step", "first", "then", "next", "strategy", "approach", "design", "architecture"}
	debuggingKeywords := []string{"debug", "fix", "issue", "problem", "crash", "stack trace", "exception", "traceback"}
	analysisKeywords := []string{"analyze", "explain", "why", "how does", "what is", "understand", "review"}

	if containsAny(text.String(), codeKeywords) {
		return DomainCode
	}
	if containsAny(text.String(), debuggingKeywords) {
		return DomainDebugging
	}
	if containsAny(text.String(), planningKeywords) {
		return DomainPlanning
	}
	if containsAny(text.String(), analysisKeywords) {
		return DomainAnalysis
	}

	return DomainGeneral
}

func (m *Middleware) classifyTaskType(messages []llm.ChatMessage, response *llm.Response) TaskType {
	// Check if response has tool calls
	if response != nil && response.HasToolCalls() {
		return TaskTypeToolUse
	}

	// Check for multi-step patterns
	var text strings.Builder
	for _, msg := range messages {
		text.WriteString(" " + msg.Content)
	}

	multiStepKeywords := []string{"step by step", "first", "second", "then", "finally", "multiple steps"}
	reasoningKeywords := []string{"think", "reason", "consider", "analyze", "evaluate", "compare"}

	if containsAny(text.String(), multiStepKeywords) {
		return TaskTypeMultiStep
	}
	if containsAny(text.String(), reasoningKeywords) {
		return TaskTypeReasoning
	}

	return TaskTypeChat
}

func (m *Middleware) estimateComplexity(messages []llm.ChatMessage) Complexity {
	// Simple heuristic based on message length and structure

	var totalLength int
	var hasCode bool
	var hasMultipleMessages bool

	for _, msg := range messages {
		totalLength += len(msg.Content)
		if containsAny(msg.Content, []string{"```", "func ", "def ", "class ", "import "}) {
			hasCode = true
		}
	}

	hasMultipleMessages = len(messages) > 2

	if totalLength > 2000 || (hasCode && hasMultipleMessages) {
		return ComplexityComplex
	}
	if totalLength > 500 || hasCode || hasMultipleMessages {
		return ComplexityModerate
	}

	return ComplexitySimple
}

// Context key for conversation ID
type contextKey string

const conversationIDKey contextKey = "conversation_id"

// WithConversationID adds a conversation ID to the context.
func WithConversationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, conversationIDKey, id)
}

func getConversationID(ctx context.Context) string {
	if id, ok := ctx.Value(conversationIDKey).(string); ok {
		return id
	}
	return "unknown"
}

// containsAny checks if text contains any of the keywords (case-insensitive).
func containsAny(text string, keywords []string) bool {
	lower := toLower(text)
	for _, kw := range keywords {
		if containsSubstr(lower, toLower(kw)) {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
