package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"log/slog"
)

const maxConsecutiveFailures = 10

// BotExecutor abstracts the agent loop execution for bots.
type BotExecutor interface {
	ExecuteBot(ctx context.Context, systemPrompt, userMessage string) (output string, tokensUsed int, err error)
}

// BotExecutionResult holds the outcome of a single bot execution.
type BotExecutionResult struct {
	BotID      string
	Output     string
	TokensUsed int
	Success    bool
	Error      string
	Duration   time.Duration
}

type BotRunner struct {
	definition BotDefinition
	namespace  *MemoryNamespace
	executor   BotExecutor
	logger     *slog.Logger
}

func NewBotRunner(def BotDefinition) *BotRunner {
	return &BotRunner{
		definition: def,
		namespace:  NewMemoryNamespace(def.ID),
	}
}

func (r *BotRunner) Definition() BotDefinition {
	return r.definition
}

// ShouldRun checks whether the bot should execute given its current state.
func (r *BotRunner) ShouldRun(state *BotState) bool {
	if state == nil {
		return true
	}

	if state.ConsecutiveFailures >= maxConsecutiveFailures {
		return false
	}

	if r.definition.Constraints.DailyBudgetCents > 0 {
		today := time.Now().Format("2006-01-02")
		if state.TodayDate == today && state.TodayCostCents >= r.definition.Constraints.DailyBudgetCents {
			return false
		}
	}

	if r.definition.Constraints.MaxInvocationsPerDay > 0 {
		today := time.Now().Format("2006-01-02")
		if state.TodayDate == today && state.TodayRuns >= r.definition.Constraints.MaxInvocationsPerDay {
			return false
		}
	}

	return true
}

// BuildSystemPrompt constructs the system prompt for a bot invocation.
func (r *BotRunner) BuildSystemPrompt(triggerContext string) string {
	var b strings.Builder

	b.WriteString(r.definition.Prompt)

	b.WriteString("\n\n## Bot Identity\n")
	b.WriteString(fmt.Sprintf("You are bot %q (%s).\n", r.definition.ID, r.definition.Name))
	b.WriteString(fmt.Sprintf("Description: %s\n", r.definition.Description))

	b.WriteString("\n## Current Invocation\n")
	b.WriteString(fmt.Sprintf("Trigger context: %s\n", triggerContext))
	b.WriteString(fmt.Sprintf("Timestamp: %s\n", time.Now().UTC().Format(time.RFC3339)))

	b.WriteString("\n## Instructions\n")
	b.WriteString("Perform your task and store any important observations in memory for future invocations.\n")
	b.WriteString("Be concise. You are running autonomously - there is no user to interact with.\n")

	return b.String()
}

// BuildUserMessage constructs the user message for a bot invocation.
func (r *BotRunner) BuildUserMessage(triggerContext string) string {
	return fmt.Sprintf("[Bot %s triggered] %s", r.definition.ID, triggerContext)
}

// WithExecutor returns a copy of the runner with the given executor.
func (r *BotRunner) WithExecutor(executor BotExecutor) *BotRunner {
	cp := *r
	cp.executor = executor
	return &cp
}

// WithLogger returns a copy of the runner with the given logger.
func (r *BotRunner) WithLogger(logger *slog.Logger) *BotRunner {
	cp := *r
	cp.logger = logger
	return &cp
}

// Execute runs the bot through the executor, checking ShouldRun first.
func (r *BotRunner) Execute(ctx context.Context, state *BotState, triggerCtx string) (*BotExecutionResult, error) {
	if r.executor == nil {
		return nil, fmt.Errorf("bot %q: no executor configured", r.definition.ID)
	}

	if !r.ShouldRun(state) {
		return &BotExecutionResult{
			BotID:   r.definition.ID,
			Success: false,
			Error:   "skipped: budget exhausted or consecutive failure limit reached",
		}, nil
	}

	systemPrompt := r.BuildSystemPrompt(triggerCtx)
	userMessage := r.BuildUserMessage(triggerCtx)

	// Apply per-bot timeout if configured.
	if r.definition.Constraints.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.definition.Constraints.Timeout)
		defer cancel()
	}

	start := time.Now()
	output, tokensUsed, err := r.executor.ExecuteBot(ctx, systemPrompt, userMessage)
	duration := time.Since(start)

	result := &BotExecutionResult{
		BotID:      r.definition.ID,
		Output:     output,
		TokensUsed: tokensUsed,
		Duration:   duration,
	}

	if err != nil {
		result.Success = false
		result.Error = err.Error()
		if r.logger != nil {
			r.logger.Error("bot execution failed", "bot_id", r.definition.ID, "error", err, "duration", duration)
		}
	} else {
		result.Success = true
		if r.logger != nil {
			r.logger.Info("bot execution succeeded", "bot_id", r.definition.ID, "tokens", tokensUsed, "duration", duration)
		}
	}

	return result, nil
}
