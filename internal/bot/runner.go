package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"log/slog"
)

const maxConsecutiveFailures = 10

// RetryPolicy configures the BotRunner's retry behaviour for LLM calls.
// When MaxRetries is 0, no retries are attempted (the executor's single
// call is the only attempt). RetryBackoff is the initial backoff duration;
// each retry doubles the backoff (exponential). The total elapsed time
// across all retries is capped by the context deadline (if set) or the
// bot's Constraints.Timeout.
//
// H2: The spec says "LLM call fails → BotRunner's existing retry path"
// (line 588). The LLM client itself has retry logic, but the runner
// orchestrates higher-level retry with backoff so transient failures
// (network blips, rate-limit 429s) are retried without propagating
// errors to the GoalLoop's failure counter.
type RetryPolicy struct {
	MaxRetries    int
	RetryBackoff  time.Duration
}

// DefaultRetryPolicy is a conservative retry policy used when no explicit
// policy is set. 2 retries with 1s initial backoff (doubling to 2s on the
// second retry). This covers transient network errors and brief rate-limit
// windows without adding excessive latency.
var DefaultRetryPolicy = RetryPolicy{
	MaxRetries:   2,
	RetryBackoff: 1 * time.Second,
}

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
	definition   BotDefinition
	namespace    *MemoryNamespace
	executor     BotExecutor
	logger       *slog.Logger
	retryPolicy  RetryPolicy
}

func NewBotRunner(def BotDefinition) *BotRunner {
	return &BotRunner{
		definition:  def,
		namespace:   NewMemoryNamespace(def.ID),
		retryPolicy: DEFAULT_RETRY_POLICY_FOR_NEW_RUNNERS,
	}
}

// DEFAULT_RETRY_POLICY_FOR_NEW_RUNNERS is the retry policy used for
// newly-constructed runners. It is set to a zero-value RetryPolicy
// (MaxRetries=0) by default so existing behavior is unchanged; callers
// who want retries should call WithRetryPolicy.
var DEFAULT_RETRY_POLICY_FOR_NEW_RUNNERS = RetryPolicy{}

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

// WithRetryPolicy returns a copy of the runner with the given retry policy.
// The policy controls how many times the LLM call is retried on failure and
// the initial backoff duration (which doubles on each retry). A zero-value
// RetryPolicy (MaxRetries=0) disables retry entirely.
func (r *BotRunner) WithRetryPolicy(policy RetryPolicy) *BotRunner {
	cp := *r
	cp.retryPolicy = policy
	return &cp
}

// Execute runs the bot through the executor, checking ShouldRun first.
// H2: LLM calls are wrapped in a retry loop with exponential backoff
// when a RetryPolicy with MaxRetries > 0 is configured. Context
// cancellation (including timeout) propagates immediately without
// further retries.
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
	var output string
	var tokensUsed int
	var lastErr error

	maxRetries := r.retryPolicy.MaxRetries
	backoff := r.retryPolicy.RetryBackoff
	if backoff <= 0 && maxRetries > 0 {
		backoff = 1 * time.Second
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		output, tokensUsed, lastErr = r.executor.ExecuteBot(ctx, systemPrompt, userMessage)
		if lastErr == nil {
			break
		}
		// Context cancellation is not retriable.
		if ctx.Err() != nil {
			break
		}
		if attempt < maxRetries {
			if r.logger != nil {
				r.logger.Warn("bot execution retrying",
					"bot_id", r.definition.ID,
					"attempt", attempt+1,
					"max_retries", maxRetries,
					"error", lastErr,
					"backoff", backoff)
			}
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				lastErr = ctx.Err()
				break
			}
			backoff *= 2
		}
	}

	duration := time.Since(start)

	result := &BotExecutionResult{
		BotID:      r.definition.ID,
		Output:     output,
		TokensUsed: tokensUsed,
		Duration:   duration,
	}

	if lastErr != nil {
		result.Success = false
		result.Error = lastErr.Error()
		if r.logger != nil {
			r.logger.Error("bot execution failed",
				"bot_id", r.definition.ID,
				"error", lastErr,
				"duration", duration,
				"retries", maxRetries)
		}
	} else {
		result.Success = true
		if r.logger != nil {
			r.logger.Info("bot execution succeeded",
				"bot_id", r.definition.ID,
				"tokens", tokensUsed,
				"duration", duration)
		}
	}

	return result, nil
}
