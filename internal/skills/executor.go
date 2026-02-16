package skills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// Executor errors.
var (
	ErrNoSkill       = errors.New("skill is nil")
	ErrNoLLMClient   = errors.New("LLM client is nil")
	ErrNoResolver    = errors.New("model resolver is nil")
	ErrModelNotFound = errors.New("no suitable model found for skill requirements")
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
	resolver *llm.Resolver
	client   *llm.Client
	logger   *slog.Logger
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
func WithClient(client *llm.Client) ExecutorOption {
	return func(e *Executor) {
		e.client = client
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
	client := e.client
	if client == nil {
		client = llm.NewClient(modelConfig, llm.WithLogger(e.logger))
		defer client.Close()
	} else {
		// Switch to resolved model if different
		if client.Config().ModelID != modelConfig.ModelID {
			client.SwitchModel(modelConfig)
		}
	}

	// Build messages
	messages := []llm.ChatMessage{
		{
			Role:    llm.RoleSystem,
			Content: skill.Body,
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
	resp, err := client.Chat(ctx, messages, chatOpts...)
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

	return &SkillExecutionResult{
		Content:          resp.Content,
		Model:            resp.Model,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
	}, nil
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
func (e *Executor) buildPrompt(skill *Skill, input string) string {
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
	client := e.client
	if client == nil {
		client = llm.NewClient(modelConfig, llm.WithLogger(e.logger))
		defer client.Close()
	} else {
		if client.Config().ModelID != modelConfig.ModelID {
			client.SwitchModel(modelConfig)
		}
	}

	// Prepend system message with skill body if not already present
	if len(messages) == 0 || messages[0].Role != llm.RoleSystem {
		systemMsg := llm.ChatMessage{
			Role:    llm.RoleSystem,
			Content: skill.Body,
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
	resp, err := client.Chat(ctx, messages, chatOpts...)
	if err != nil {
		return nil, &ExecutorError{
			SkillName: skill.Name,
			Message:   "LLM request failed",
			Cause:     err,
		}
	}

	return &SkillExecutionResult{
		Content:          resp.Content,
		Model:            resp.Model,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
	}, nil
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
