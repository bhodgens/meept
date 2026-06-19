package shadow

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"golang.org/x/time/rate"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // Normal operation
	CircuitOpen                         // Failing, reject requests
	CircuitHalfOpen                     // Testing if service recovered
)

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	mu              sync.RWMutex
	state           CircuitState
	failures        int
	successes       int
	lastFailureTime time.Time
	lastStateChange time.Time

	// Configuration
	failureThreshold int           // Number of failures to open circuit
	successThreshold int           // Number of successes in half-open to close circuit
	resetTimeout     time.Duration // Time to wait before half-open
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(failureThreshold, successThreshold int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:            CircuitClosed,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		resetTimeout:     resetTimeout,
		lastStateChange:  time.Now(),
	}
}

// Allow checks if a request should be allowed through.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	switch cb.state {
	case CircuitClosed:
		return true

	case CircuitOpen:
		// Check if we should transition to half-open
		if now.Sub(cb.lastStateChange) > cb.resetTimeout {
			cb.state = CircuitHalfOpen
			cb.lastStateChange = now
			cb.successes = 0
			return true
		}
		return false

	case CircuitHalfOpen:
		return true
	}

	return true
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitHalfOpen:
		cb.successes++
		if cb.successes >= cb.successThreshold {
			cb.state = CircuitClosed
			cb.lastStateChange = time.Now()
			cb.failures = 0
		}

	case CircuitClosed:
		// Reset failure count on success
		cb.failures = 0
	}
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case CircuitClosed:
		if cb.failures >= cb.failureThreshold {
			cb.state = CircuitOpen
			cb.lastStateChange = time.Now()
		}

	case CircuitHalfOpen:
		// Any failure in half-open goes back to open
		cb.state = CircuitOpen
		cb.lastStateChange = time.Now()
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// TeacherClient orchestrates teacher model responses.
type TeacherClient struct {
	primary       *llm.Client
	fallback      *llm.Client
	config        *TeacherConfig
	trainingStore *SQLiteTrainingStore
	logger        *slog.Logger

	// Rate limiting
	limiter *rate.Limiter

	// Circuit breaker
	circuitBreaker *CircuitBreaker

	// Retry configuration
	maxRetries     int
	baseRetryDelay time.Duration

	// Daily usage tracking
	mu            sync.RWMutex
	dailyQueries  int
	dailyCost     float64
	lastResetDate string
}

// TeacherClientOption is a functional option for TeacherClient.
type TeacherClientOption func(*TeacherClient)

// WithTeacherLogger sets the logger.
func WithTeacherLogger(logger *slog.Logger) TeacherClientOption {
	return func(t *TeacherClient) {
		t.logger = logger
	}
}

// WithTrainingStore sets the training store for usage tracking.
func WithTrainingStore(store *SQLiteTrainingStore) TeacherClientOption {
	return func(t *TeacherClient) {
		t.trainingStore = store
	}
}

// NewTeacherClient creates a new teacher client.
func NewTeacherClient(primary, fallback *llm.Client, config *TeacherConfig, opts ...TeacherClientOption) *TeacherClient {
	t := &TeacherClient{
		primary:  primary,
		fallback: fallback,
		config:   config,
		logger:   slog.Default(),
		limiter:  rate.NewLimiter(rate.Limit(float64(config.RequestsPerMinute)/60.0), 1),
		// Circuit breaker: open after 5 failures, close after 2 successes, reset after 1 minute
		circuitBreaker: NewCircuitBreaker(5, 2, time.Minute),
		maxRetries:     3,
		baseRetryDelay: 500 * time.Millisecond,
	}

	for _, opt := range opts {
		opt(t)
	}

	return t
}

// GetResponse gets a teacher response for the given messages.
func (t *TeacherClient) GetResponse(ctx context.Context, messages []llm.ChatMessage) (response, reasoning string, err error) {
	// Check circuit breaker
	if !t.circuitBreaker.Allow() {
		return "", "", fmt.Errorf("circuit breaker open: teacher service unavailable")
	}

	// Check daily limits
	if err := t.checkLimits(ctx); err != nil {
		return "", "", err
	}

	// Wait for rate limit
	if err := t.limiter.Wait(ctx); err != nil {
		return "", "", fmt.Errorf("rate limit wait cancelled: %w", err)
	}

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, t.config.Timeout())
	defer cancel()

	// Try with retries and exponential backoff
	var lastErr error
	for attempt := 0; attempt <= t.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			delay := t.baseRetryDelay * time.Duration(1<<uint(attempt-1))
			t.logger.Debug("Retrying teacher request",
				"attempt", attempt,
				"delay", delay,
			)

			select {
			case <-timeoutCtx.Done():
				t.circuitBreaker.RecordFailure()
				return "", "", fmt.Errorf("timeout waiting for retry: %w", timeoutCtx.Err())
			case <-time.After(delay):
			}
		}

		// Try primary
		response, err := t.callTeacher(timeoutCtx, t.primary, messages)
		if err == nil {
			t.circuitBreaker.RecordSuccess()
			t.recordUsage(ctx, t.primary.Config(), response)
			return response.Content, t.primary.Config().ModelID, nil
		}
		lastErr = err

		// Check if error is retryable
		if !isRetryableError(err) {
			break
		}
	}

	t.logger.Warn("Primary teacher failed, trying fallback",
		"error", lastErr,
		"primary_model", t.primary.Config().ModelID,
	)

	// Try fallback if available
	if t.fallback != nil {
		response, err := t.callTeacher(timeoutCtx, t.fallback, messages)
		if err == nil {
			t.circuitBreaker.RecordSuccess()
			t.recordUsage(ctx, t.fallback.Config(), response)
			return response.Content, t.fallback.Config().ModelID, nil
		}

		t.logger.Error("Fallback teacher also failed",
			"error", err,
			"fallback_model", t.fallback.Config().ModelID,
		)
		lastErr = err
	}

	// Record failure for circuit breaker
	t.circuitBreaker.RecordFailure()

	return "", "", fmt.Errorf("teacher request failed: %w", lastErr)
}

// isRetryableError determines if an error is transient and worth retrying.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	retryablePatterns := []string{
		"timeout",
		"connection reset",
		"connection refused",
		"temporary failure",
		"503",
		"429", // Rate limited
		"502", // Bad gateway
		"504", // Gateway timeout
	}

	for _, pattern := range retryablePatterns {
		if containsIgnoreCase(errStr, pattern) {
			return true
		}
	}

	return false
}

// containsIgnoreCase checks if s contains substr (case-insensitive).
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func (t *TeacherClient) callTeacher(ctx context.Context, client *llm.Client, messages []llm.ChatMessage) (*llm.Response, error) {
	opts := []llm.ChatOption{
		llm.WithTemperature(t.config.Temperature),
		llm.WithMaxTokens(t.config.MaxTokens),
	}

	return client.Chat(ctx, messages, opts...)
}

func (t *TeacherClient) checkLimits(ctx context.Context) error {
	// Check if we need to reset daily counters, and if so, whether
	// we need to load from the database. Collect under lock, do I/O
	// outside lock (CLAUDE.md "Mutex scope" rule).
	t.mu.Lock()
	today := time.Now().UTC().Format("2006-01-02")
	needReset := t.lastResetDate != today
	if needReset {
		t.dailyQueries = 0
		t.dailyCost = 0
		t.lastResetDate = today
	}
	maxQueries := t.config.MaxDailyQueries
	maxCost := t.config.MaxDailyCost
	queries := t.dailyQueries
	cost := t.dailyCost
	store := t.trainingStore
	t.mu.Unlock()

	// Load from database outside the lock if we reset
	if needReset && store != nil {
		dbQueries, dbCost, err := store.GetTeacherUsageToday(ctx)
		if err == nil {
			t.mu.Lock()
			t.dailyQueries = dbQueries
			t.dailyCost = dbCost
			queries = dbQueries
			cost = dbCost
			t.mu.Unlock()
		}
	}

	// Check query limit
	if maxQueries > 0 && queries >= maxQueries {
		return fmt.Errorf("daily teacher query limit reached (%d)", maxQueries)
	}

	// Check cost limit
	if maxCost > 0 && cost >= maxCost {
		return fmt.Errorf("daily teacher cost limit reached ($%.2f)", maxCost)
	}

	return nil
}

func (t *TeacherClient) recordUsage(ctx context.Context, cfg *llm.ModelConfig, response *llm.Response) {
	// Calculate cost outside the lock (no shared state needed)
	inputCost := float64(response.Usage.PromptTokens) * cfg.CostPerMillionInput / 1_000_000.0
	outputCost := float64(response.Usage.CompletionTokens) * cfg.CostPerMillionOutput / 1_000_000.0
	actualCost := inputCost + outputCost

	// Update counters under lock, then do I/O outside lock
	t.mu.Lock()
	t.dailyQueries++
	t.dailyCost += actualCost
	store := t.trainingStore
	dailyQueries := t.dailyQueries
	dailyCost := t.dailyCost
	t.mu.Unlock()

	// Persist to database outside the lock
	if store != nil {
		if err := store.RecordTeacherUsage(ctx, 1, actualCost); err != nil {
			t.logger.Warn("Failed to record teacher usage", "error", err)
		}
	}

	t.logger.Debug("Teacher usage recorded",
		"daily_queries", dailyQueries,
		"daily_cost", dailyCost,
		"input_tokens", response.Usage.PromptTokens,
		"output_tokens", response.Usage.CompletionTokens,
	)
}

// GetUsageStats returns current daily usage statistics.
func (t *TeacherClient) GetUsageStats() (queries int, cost float64) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.dailyQueries, t.dailyCost
}

// IsAvailable returns true if the teacher is available (within limits and circuit closed).
func (t *TeacherClient) IsAvailable(ctx context.Context) bool {
	if t.circuitBreaker != nil && !t.circuitBreaker.Allow() {
		return false
	}
	return t.checkLimits(ctx) == nil
}

// CircuitState returns the current state of the circuit breaker.
func (t *TeacherClient) CircuitState() CircuitState {
	if t.circuitBreaker == nil {
		return CircuitClosed
	}
	return t.circuitBreaker.State()
}

// ResetCircuit resets the circuit breaker to closed state.
func (t *TeacherClient) ResetCircuit() {
	if t.circuitBreaker != nil {
		t.circuitBreaker.mu.Lock()
		t.circuitBreaker.state = CircuitClosed
		t.circuitBreaker.failures = 0
		t.circuitBreaker.successes = 0
		t.circuitBreaker.lastStateChange = time.Now()
		t.circuitBreaker.mu.Unlock()
	}
}
