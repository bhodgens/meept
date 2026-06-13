package skills

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// isAnthropic checks whether the given ModelConfig points to an Anthropic endpoint.
func isAnthropic(cfg *llm.ModelConfig) bool {
	if cfg.ProviderID == "anthropic" {
		return true
	}
	return strings.Contains(strings.ToLower(cfg.BaseURL), "anthropic")
}

// createChatter creates a Chatter for a ModelConfig, selecting the right
// client implementation (Anthropic vs OpenAI-compatible) based on provider ID
// and base URL. It injects TokenResolver and ExtraHeaders when present so that
// skills that use OAuth-protected providers or providers requiring custom headers
// work correctly.
func createChatter(cfg *llm.ModelConfig, logger *slog.Logger, tokenResolver llm.TokenResolver, extraHeaders map[string]string) llm.Chatter {
	if isAnthropic(cfg) {
		return llm.NewAnthropicClient(cfg, llm.WithAnthropicLogger(logger))
	}
	opts := []llm.ClientOption{llm.WithLogger(logger)}
	if tokenResolver != nil {
		opts = append(opts, llm.WithTokenResolver(tokenResolver, cfg.OAuthProvider))
	}
	if len(extraHeaders) > 0 {
		opts = append(opts, llm.WithExtraHeaders(extraHeaders))
	}
	return llm.NewClient(cfg, opts...)
}

// Executor errors.
var (
	ErrNoSkill                = errors.New("skill is nil")
	ErrNoLLMClient            = errors.New("LLM client is nil")
	ErrNoResolver             = errors.New("model resolver is nil")
	ErrModelNotFound          = errors.New("no suitable model found for skill requirements")
	ErrPrerequisitesNotMet    = errors.New("skill prerequisites not met")
)

// ExecutorError wraps an execution error with context.
type ExecutorError struct {
	SkillName string
	Message   string
	Cause     error
}

func (e *ExecutorError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("skill %q: %s: %v", e.SkillName, e.Message, e.Cause)
	}
	return fmt.Sprintf("skill %q: %s", e.SkillName, e.Message)
}

func (e *ExecutorError) Unwrap() error {
	return e.Cause
}

// Executor executes skills using the LLM client.
type Executor struct {
	resolver             *llm.Resolver
	client               llm.Chatter
	logger               *slog.Logger
	lazyLoader           *LazySkillLoader
	prerequisiteChecker  PrerequisiteChecker
	validatePrerequisites bool
	toolMapper           *HermesToolMapper
	tokenResolver        llm.TokenResolver
	extraHeaders         map[string]string
}

// ExecutorOption is a functional option for configuring Executor.
type ExecutorOption func(*Executor)

// WithExecutorLogger sets the logger for the executor.
func WithExecutorLogger(logger *slog.Logger) ExecutorOption {
	return func(e *Executor) {
		e.logger = logger
	}
}

// WithClient sets a specific LLM client for the executor.
// If not set, the executor will create a client based on resolved model.
// Accepts any Chatter (e.g., *llm.Client or *llm.AnthropicClient).
func WithClient(client llm.Chatter) ExecutorOption {
	return func(e *Executor) {
		if client != nil {
			e.client = client
		}
	}
}

// WithLazyLoader sets a lazy loader for on-demand skill body loading.
func WithLazyLoader(loader *LazySkillLoader) ExecutorOption {
	return func(e *Executor) {
		e.lazyLoader = loader
	}
}

// WithPrerequisiteChecker sets a prerequisite checker for Hermes skill validation.
// Nil checker is ignored (no prerequisite validation).
func WithPrerequisiteChecker(checker PrerequisiteChecker) ExecutorOption {
	return func(e *Executor) {
		if checker != nil {
			e.prerequisiteChecker = checker
		}
	}
}

// WithValidatePrerequisites enables or disables prerequisite validation.
// Default is false (no validation).
func WithValidatePrerequisites(enabled bool) ExecutorOption {
	return func(e *Executor) {
		e.validatePrerequisites = enabled
	}
}

// WithToolMapper sets a Hermes tool mapper for translating tool references.
// Nil mapper is ignored.
func WithToolMapper(mapper *HermesToolMapper) ExecutorOption {
	return func(e *Executor) {
		if mapper != nil {
			e.toolMapper = mapper
		}
	}
}

// WithExecutorTokenResolver sets the OAuth token resolver for the executor.
// When set, locally created clients will use it to obtain fresh access tokens.
// Nil resolver is ignored.
func WithExecutorTokenResolver(tr llm.TokenResolver) ExecutorOption {
	return func(e *Executor) {
		if tr != nil {
			e.tokenResolver = tr
		}
	}
}

// WithExecutorExtraHeaders sets additional HTTP headers for locally created
// LLM clients (e.g. for providers that require custom headers like
// X-GitHub-Api-Version). Nil map is ignored.
func WithExecutorExtraHeaders(headers map[string]string) ExecutorOption {
	return func(e *Executor) {
		if headers != nil {
			e.extraHeaders = headers
		}
	}
}

// NewExecutor creates a new skill executor.
func NewExecutor(resolver *llm.Resolver, opts ...ExecutorOption) *Executor {
	e := &Executor{
		resolver: resolver,
		logger:   slog.Default(),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Execute runs a skill with the given input and returns the result.
func (e *Executor) Execute(ctx context.Context, skill *Skill, input string) (*SkillExecutionResult, error) {
	if skill == nil {
		return nil, ErrNoSkill
	}
	if e.resolver == nil {
		return nil, ErrNoResolver
	}

	e.logger.Info("Executing skill",
		"name", skill.Name,
		"requires", skill.Requires,
	)

	// Validate Hermes prerequisites if configured and present.
	if e.validatePrerequisites && skill.Prerequisites != nil && e.prerequisiteChecker != nil {
		if err := CheckPrerequisites(e.prerequisiteChecker, skill.Prerequisites); err != nil {
			return nil, &ExecutorError{
				SkillName: skill.Name,
				Message:   "prerequisites not met",
				Cause:     err,
			}
		}
	}

	// Translate Hermes tool references in skill body if mapper is set and source is hermes.
	execBody := skill.Body
	if e.toolMapper != nil && skill.SourceOrigin == "hermes" {
		execBody = e.toolMapper.TranslateToolReferences(skill.Body)
	}

	// Start MCP runtime if skill declares MCP servers.
	var mcpRuntime *MCPRuntime
	if len(skill.MCPServers) > 0 {
		mcpRuntime = NewMCPRuntime(skill.MCPServers, e.logger)
		if err := mcpRuntime.Start(ctx); err != nil {
			e.logger.Warn("MCP runtime start had errors, continuing with available servers",
				"skill", skill.Name,
				"error", err,
			)
		}
		defer func() {
			if err := mcpRuntime.Shutdown(); err != nil {
				e.logger.Warn("MCP runtime shutdown error",
					"skill", skill.Name,
					"error", err,
				)
			}
		}()
	}

	// Resolve model based on skill requirements
	modelConfig, err := e.resolveModel(skill)
	if err != nil {
		return nil, &ExecutorError{
			SkillName: skill.Name,
			Message:   "failed to resolve model",
			Cause:     err,
		}
	}

	e.logger.Debug("Resolved model for skill",
		"skill", skill.Name,
		"model", modelConfig.ModelID,
		"provider", modelConfig.ProviderID,
	)

	// Build prompt
	prompt := e.buildPrompt(skill, input)

	// Create or use existing client
	var chatter llm.Chatter
	createdLocally := false
	switch {
	case e.client == nil:
		chatter = createChatter(modelConfig, e.logger, e.tokenResolver, e.extraHeaders)
		createdLocally = true
	case e.client.Config().ModelID != modelConfig.ModelID:
		// AnthropicClient doesn't support SwitchModel, so create a new one
		chatter = createChatter(modelConfig, e.logger, e.tokenResolver, e.extraHeaders)
		createdLocally = true
	default:
		chatter = e.client
	}
	defer func() {
		if createdLocally {
			if closer, ok := chatter.(io.Closer); ok {
				closer.Close()
			}
		}
	}()

	// Build messages
	messages := []llm.ChatMessage{
		{
			Role:    llm.RoleSystem,
			Content: execBody,
		},
		{
			Role:    llm.RoleUser,
			Content: prompt,
		},
	}

	// Build chat options
	var chatOpts []llm.ChatOption
	if skill.Temperature != nil {
		chatOpts = append(chatOpts, llm.WithTemperature(*skill.Temperature))
	}
	if skill.MaxTokens != nil {
		chatOpts = append(chatOpts, llm.WithMaxTokens(*skill.MaxTokens))
	}

	// Execute
	resp, err := chatter.Chat(ctx, messages, chatOpts...)
	if err != nil {
		return nil, &ExecutorError{
			SkillName: skill.Name,
			Message:   "LLM request failed",
			Cause:     err,
		}
	}

	e.logger.Info("Skill execution complete",
		"name", skill.Name,
		"model", resp.Model,
		"tokens", resp.Usage.TotalTokens,
	)

	result := &SkillExecutionResult{
		Content:          resp.Content,
		Model:            resp.Model,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
	}

	if mcpRuntime != nil && mcpRuntime.Started() {
		result.MCPTools = mcpRuntime.Tools()
		result.MCPServersStarted = true
	}

	return result, nil
}

// resolveModel finds an appropriate model for the skill's requirements.
func (e *Executor) resolveModel(skill *Skill) (*llm.ModelConfig, error) {
	// Build SkillRequirements for the resolver
	skillReq := &llm.SkillRequirements{
		Name:     skill.Name,
		Requires: skill.Requires,
	}

	// Try to resolve using current client's model if available
	var currentModel *llm.ModelConfig
	if e.client != nil {
		currentModel = e.client.Config()
	}

	model, err := e.resolver.ResolveForSkill(skillReq, currentModel)
	if err != nil {
		return nil, err
	}

	if model == nil {
		return nil, ErrModelNotFound
	}

	return model, nil
}

// buildPrompt constructs the user prompt for the skill.
func (e *Executor) buildPrompt(_ *Skill, input string) string {
	// If the skill body already contains specific instructions,
	// we just pass the user input directly
	return strings.TrimSpace(input)
}

// ExecuteWithMessages executes a skill with custom message history.
// This allows for multi-turn conversations within a skill.
func (e *Executor) ExecuteWithMessages(
	ctx context.Context,
	skill *Skill,
	messages []llm.ChatMessage,
) (*SkillExecutionResult, error) {
	if skill == nil {
		return nil, ErrNoSkill
	}
	if e.resolver == nil {
		return nil, ErrNoResolver
	}

	// Validate Hermes prerequisites if configured and present.
	if e.validatePrerequisites && skill.Prerequisites != nil && e.prerequisiteChecker != nil {
		if err := CheckPrerequisites(e.prerequisiteChecker, skill.Prerequisites); err != nil {
			return nil, &ExecutorError{
				SkillName: skill.Name,
				Message:   "prerequisites not met",
				Cause:     err,
			}
		}
	}

	// Translate Hermes tool references in skill body.
	execBody := skill.Body
	if e.toolMapper != nil && skill.SourceOrigin == "hermes" {
		execBody = e.toolMapper.TranslateToolReferences(skill.Body)
	}

	// Start MCP runtime if skill declares MCP servers.
	var mcpRuntime *MCPRuntime
	if len(skill.MCPServers) > 0 {
		mcpRuntime = NewMCPRuntime(skill.MCPServers, e.logger)
		if err := mcpRuntime.Start(ctx); err != nil {
			e.logger.Warn("MCP runtime start had errors, continuing with available servers",
				"skill", skill.Name,
				"error", err,
			)
		}
		defer func() {
			if err := mcpRuntime.Shutdown(); err != nil {
				e.logger.Warn("MCP runtime shutdown error",
					"skill", skill.Name,
					"error", err,
				)
			}
		}()
	}

	// Resolve model
	modelConfig, err := e.resolveModel(skill)
	if err != nil {
		return nil, &ExecutorError{
			SkillName: skill.Name,
			Message:   "failed to resolve model",
			Cause:     err,
		}
	}

	// Create or use existing client
	var chatter llm.Chatter
	createdLocally := false
	switch {
	case e.client == nil:
		chatter = createChatter(modelConfig, e.logger, e.tokenResolver, e.extraHeaders)
		createdLocally = true
	case e.client.Config().ModelID != modelConfig.ModelID:
		// AnthropicClient doesn't support SwitchModel, so create a new one
		chatter = createChatter(modelConfig, e.logger, e.tokenResolver, e.extraHeaders)
		createdLocally = true
	default:
		chatter = e.client
	}
	defer func() {
		if createdLocally {
			if closer, ok := chatter.(io.Closer); ok {
				closer.Close()
			}
		}
	}()

	// Prepend system message with skill body if not already present
	if len(messages) == 0 || messages[0].Role != llm.RoleSystem {
		systemMsg := llm.ChatMessage{
			Role:    llm.RoleSystem,
			Content: execBody,
		}
		messages = append([]llm.ChatMessage{systemMsg}, messages...)
	}

	// Build chat options
	var chatOpts []llm.ChatOption
	if skill.Temperature != nil {
		chatOpts = append(chatOpts, llm.WithTemperature(*skill.Temperature))
	}
	if skill.MaxTokens != nil {
		chatOpts = append(chatOpts, llm.WithMaxTokens(*skill.MaxTokens))
	}

	// Execute
	resp, err := chatter.Chat(ctx, messages, chatOpts...)
	if err != nil {
		return nil, &ExecutorError{
			SkillName: skill.Name,
			Message:   "LLM request failed",
			Cause:     err,
		}
	}

	result := &SkillExecutionResult{
		Content:          resp.Content,
		Model:            resp.Model,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
	}

	if mcpRuntime != nil && mcpRuntime.Started() {
		result.MCPTools = mcpRuntime.Tools()
		result.MCPServersStarted = true
	}

	return result, nil
}

// CanExecute checks if the executor can execute a skill.
// Returns true if a suitable model can be found for the skill's requirements.
func (e *Executor) CanExecute(skill *Skill) bool {
	if skill == nil || e.resolver == nil {
		return false
	}

	model, err := e.resolveModel(skill)
	return err == nil && model != nil
}

// GetModelForSkill returns the model that would be used for a skill.
func (e *Executor) GetModelForSkill(skill *Skill) (*llm.ModelConfig, error) {
	if skill == nil {
		return nil, ErrNoSkill
	}
	if e.resolver == nil {
		return nil, ErrNoResolver
	}

	return e.resolveModel(skill)
}

// ExecuteByName executes a skill by name using the lazy loader.
// This is useful when you only have the skill name and want on-demand loading.
func (e *Executor) ExecuteByName(ctx context.Context, name, input string) (*SkillExecutionResult, error) {
	if e.lazyLoader == nil {
		return nil, fmt.Errorf("lazy loader not configured")
	}

	skill, err := e.lazyLoader.Load(ctx, name)
	if err != nil {
		return nil, &ExecutorError{
			SkillName: name,
			Message:   "failed to load skill",
			Cause:     err,
		}
	}

	return e.Execute(ctx, skill, input)
}

// LazyLoader returns the configured lazy loader (may be nil).
func (e *Executor) LazyLoader() *LazySkillLoader {
	return e.lazyLoader
}

// SetLazyLoader sets the lazy loader for on-demand skill loading.
func (e *Executor) SetLazyLoader(loader *LazySkillLoader) {
	e.lazyLoader = loader
}
