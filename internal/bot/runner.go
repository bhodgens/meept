package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
)

const maxConsecutiveFailures = 10

type BotRunner struct {
	definition  BotDefinition
	namespace   *MemoryNamespace
	executor    BotExecutor
	logger      *slog.Logger
	retryPolicy RetryPolicy

	// bus is the optional message bus used to publish execution-result and
	// turn-state events. It is set before execution begins (nil-safe).
	bus *bus.MessageBus
}

// SetBus sets the message bus used for execution-result and turn-state
// emission. A nil bus is a no-op: execution proceeds without publishing.
func (r *BotRunner) SetBus(b *bus.MessageBus) {
	r.bus = b
}

func NewBotRunner(def BotDefinition) *BotRunner {
	return &BotRunner{
		definition:  def,
		namespace:   NewMemoryNamespace(def.ID),
		retryPolicy: defaultRetryPolicyForNewRunners,
	}
}

// defaultRetryPolicyForNewRunners is the retry policy used for
// newly-constructed runners. It is set to a zero-value RetryPolicy
// (MaxRetries=0) by default so existing behavior is unchanged; callers
// who want retries should call WithRetryPolicy.
var defaultRetryPolicyForNewRunners = RetryPolicy{}

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
	fmt.Fprintf(&b, "You are bot %q (%s).\n", r.definition.ID, r.definition.Name)
	fmt.Fprintf(&b, "Description: %s\n", r.definition.Description)

	b.WriteString("\n## Current Invocation\n")
	fmt.Fprintf(&b, "Trigger context: %s\n", triggerContext)
	fmt.Fprintf(&b, "Timestamp: %s\n", time.Now().UTC().Format(time.RFC3339))

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

// emitTurnState publishes one TurnStateEvent edge on agent.TopicTurnState.
// A nil bus is a no-op; phase is deliberately left empty (bots have no
// pair-phase). Publish happens outside any lock.
func (r *BotRunner) emitTurnState(state agent.AgentTurnState, stop agent.StopReason) {
	if r.bus == nil {
		return
	}
	bus.PublishT(r.bus, agent.TopicTurnState, "bot", agent.TurnStateEvent{
		AgentID:    r.definition.ID,
		State:      state,
		StopReason: stop,
		Timestamp:  time.Now().UTC(),
	})
}

// emitResult publishes the shared ExecutionResultEvent on the bot result
// topic. The topic name is the sole tier discriminator — there is no kind
// field. A nil bus is a no-op; publish happens outside any lock.
func (r *BotRunner) emitResult(res *BotExecutionResult, status string, stop agent.StopReason) {
	if r.bus == nil {
		return
	}
	bus.PublishT(r.bus, agent.TopicBotResult, "bot", agent.ExecutionResultEvent{
		AgentID:    res.BotID,
		Status:     status,
		Output:     res.Output,
		TokensUsed: res.TokensUsed,
		Error:      res.Error,
		DurationMS: int64(res.Duration / time.Millisecond),
		StopReason: stop,
		Timestamp:  time.Now().UTC(),
	})
}

// Execute runs the bot through the executor, checking ShouldRun first.
// H2: LLM calls are wrapped in a retry loop with exponential backoff
// when a RetryPolicy with MaxRetries > 0 is configured. Context
// cancellation (including timeout) propagates immediately without
// further retries.
//
// Every execution return path emits a bot.result event plus a running -> idle
// turn-state pair (agent.TurnStateRunning at entry, agent.TurnStateIdle at
// exit). Skipped runs carry status=skipped / stop_reason=cancelled, successes
// completed / end_turn, failures failed / error. All emission is nil-bus-safe.
func (r *BotRunner) Execute(ctx context.Context, state *BotState, triggerCtx string) (*BotExecutionResult, error) {
	if r.executor == nil {
		return nil, fmt.Errorf("bot %q: no executor configured", r.definition.ID)
	}

	r.emitTurnState(agent.TurnStateRunning, "")

	if !r.ShouldRun(state) {
		result := &BotExecutionResult{
			BotID:   r.definition.ID,
			Success: false,
			Error:   "skipped: budget exhausted or consecutive failure limit reached",
		}
		r.emitResult(result, "skipped", agent.StopReasonCancelled)
		r.emitTurnState(agent.TurnStateIdle, agent.StopReasonCancelled)
		return result, nil
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

retryLoop:
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
				break retryLoop
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
		r.emitResult(result, "failed", agent.StopReasonError)
		r.emitTurnState(agent.TurnStateIdle, agent.StopReasonError)
	} else {
		result.Success = true
		if r.logger != nil {
			r.logger.Info("bot execution succeeded",
				"bot_id", r.definition.ID,
				"tokens", tokensUsed,
				"duration", duration)
		}
		r.emitResult(result, "completed", agent.StopReasonEndTurn)
		r.emitTurnState(agent.TurnStateIdle, agent.StopReasonEndTurn)
	}

	return result, nil
}