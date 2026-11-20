package bot

import (
	"context"
	"time"
)

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
	MaxRetries   int
	RetryBackoff time.Duration
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