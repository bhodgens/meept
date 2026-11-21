// Package employee — goal_loop_retry.go: bounded per-round retry for the
// GoalLoop EXECUTE step (usability remediation C).
//
// The retired BotRunner lane (internal/bot/runner.go, deleted with the dead
// lane) wrapped executor.ExecuteBot in a bounded retry loop with exponential
// backoff (DefaultRetryPolicy: 2 retries, 1s initial backoff doubling to 2s)
// so transient failures — network blips, brief rate-limit windows — never
// reached the failure counter. The live GoalLoop lane called ExecuteBot
// exactly once, so every transient executor error fed ConsecutiveFailures
// and could break a healthy goal. This file restores the retry semantics on
// the GoalLoop side, self-contained: nothing is imported from the bot
// package's retired runner types.
//
// Policy gate (mirrors the old runner):
//   - context cancellation (including deadline) is NON-retriable — the
//     executor error is returned immediately once ctx.Err() is set.
//   - a zero-value policy (MaxRetries=0) disables retry entirely, which is
//     the test/opt-out lever (WithRetryPolicy(RetryPolicy{})).
//   - failure after the FINAL attempt still produces the same failed
//     BotExecutionResult, so Reflect feeds ConsecutiveFailures exactly once
//     per round regardless of how many attempts burned.
package employee

import (
	"context"
	"time"

	"github.com/caimlas/meept/internal/bot"
)

// RetryPolicy configures the GoalLoop EXECUTE retry behaviour for
// executor.ExecuteBot calls. When MaxRetries is 0, no retries are attempted
// (the executor's single call is the only attempt). RetryBackoff is the
// initial backoff duration; each retry doubles the backoff (exponential).
// A positive MaxRetries with a non-positive RetryBackoff falls back to 1s,
// matching the retired runner's guard.
type RetryPolicy struct {
	MaxRetries   int
	RetryBackoff time.Duration
}

// DefaultRetryPolicy is the GoalLoop default, matching the retired bot
// runner's semantics: 2 retries with 1s initial backoff (doubling to 2s on
// the second retry). This covers transient network errors and brief
// rate-limit windows without adding excessive latency.
var DefaultRetryPolicy = RetryPolicy{
	MaxRetries:   2,
	RetryBackoff: 1 * time.Second,
}

// executeBotWithRetry calls runner.ExecuteBot under the loop's RetryPolicy.
// It returns the final attempt's (output, tokens, err) — exactly the shape
// the single ExecuteBot call returned before, so the surrounding success/
// failure bookkeeping (result construction, bus emissions, Reflect's
// consecutive-failure feeding) is unchanged: one EXECUTE round still makes
// exactly one result event and at most one failure contribution.
//
// All I/O happens outside the loop mutex (mutex-scope rule); the policy is
// read once under the lock.
func (l *GoalLoop) executeBotWithRetry(ctx context.Context, runner bot.BotExecutor, systemPrompt, userMessage string) (string, int, error) {
	l.mu.Lock()
	policy := l.retryPolicy
	l.mu.Unlock()

	maxRetries := policy.MaxRetries
	backoff := policy.RetryBackoff
	if backoff <= 0 && maxRetries > 0 {
		backoff = 1 * time.Second
	}

	var (
		output  string
		tokens  int
		lastErr error
	)
retryLoop:
	for attempt := 0; attempt <= maxRetries; attempt++ {
		output, tokens, lastErr = runner.ExecuteBot(ctx, systemPrompt, userMessage)
		if lastErr == nil {
			break
		}
		// Context cancellation (including timeout) is not retriable.
		if ctx.Err() != nil {
			break
		}
		if attempt < maxRetries {
			if l.logger != nil {
				l.logger.Warn("execute retrying",
					"employee_id", l.employeeID,
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
	return output, tokens, lastErr
}
