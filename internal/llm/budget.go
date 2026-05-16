package llm

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// usageRecord is a timestamped token usage entry.
type usageRecord struct {
	timestamp time.Time
	tokens    int
}

// Budget tracks and enforces token consumption budgets.
type Budget struct {
	mu sync.Mutex

	hourlyLimit    int
	dailyLimit     int
	rateLimitRPM   int
	aggressiveness float64

	// Per-task and per-session caps (0 = no cap)
	perTaskBudget  int
	perSessionBudget int

	// Sliding window for the last hour
	hourlyWindow []usageRecord

	// Daily tracking - reset at midnight UTC
	dailyUsed  int
	currentDay int // ordinal day number

	// Per-task tracking
	tasks map[string]int // taskID -> tokens used in this task

	// Per-session tracking
	sessions map[string]int // sessionID -> tokens used in this session

	// RPM tracking (sliding window of request timestamps)
	requestTimestamps []time.Time

	logger *slog.Logger
}

// BudgetConfig holds configuration for token budget tracking.
type BudgetConfig struct {
	HourlyLimit      int
	DailyLimit       int
	RateLimitRPM     int
	Aggressiveness   float64
	PerTaskBudget    int // max tokens per single task (0 = no cap)
	PerSessionBudget int // max tokens per single session (0 = no cap)
}

// NewBudget creates a new token budget tracker.
func NewBudget(cfg BudgetConfig, logger *slog.Logger) *Budget {
	if logger == nil {
		logger = slog.Default()
	}

	if cfg.Aggressiveness < 0 {
		cfg.Aggressiveness = 0
	}
	if cfg.Aggressiveness > 1 {
		cfg.Aggressiveness = 1
	}

	now := time.Now().UTC()

	return &Budget{
		hourlyLimit:        cfg.HourlyLimit,
		dailyLimit:         cfg.DailyLimit,
		rateLimitRPM:       cfg.RateLimitRPM,
		aggressiveness:     cfg.Aggressiveness,
		perTaskBudget:      cfg.PerTaskBudget,
		perSessionBudget:   cfg.PerSessionBudget,
		hourlyWindow:       make([]usageRecord, 0),
		dailyUsed:          0,
		currentDay:         dayOrdinal(now),
		requestTimestamps:  make([]time.Time, 0),
		tasks:              make(map[string]int),
		sessions:           make(map[string]int),
		logger:             logger,
	}
}

// NewBudgetFromDefaults creates a budget with default settings.
func NewBudgetFromDefaults(logger *slog.Logger) *Budget {
	return NewBudget(BudgetConfig{
		HourlyLimit:    500000,
		DailyLimit:     5000000,
		RateLimitRPM:   0, // unlimited
		Aggressiveness: 0.5,
	}, logger)
}

// dayOrdinal returns an ordinal day number for a given time.
func dayOrdinal(t time.Time) int {
	return int(t.Unix() / 86400)
}

// effectiveLimit applies the aggressiveness factor to a base limit.
func (b *Budget) effectiveLimit(base int) int {
	// factor = 0.5 + 0.5 * aggressiveness
	// At aggressiveness=0: factor=0.5 (conservative)
	// At aggressiveness=1: factor=1.0 (full budget)
	factor := 0.5 + 0.5*b.aggressiveness
	return int(float64(base) * factor)
}

// pruneHourlyWindow removes entries older than 1 hour.
func (b *Budget) pruneHourlyWindow() {
	cutoff := time.Now().Add(-time.Hour)
	idx := 0
	for i, rec := range b.hourlyWindow {
		if rec.timestamp.After(cutoff) {
			idx = i
			break
		}
		idx = len(b.hourlyWindow) // All expired
	}
	if idx > 0 {
		b.hourlyWindow = b.hourlyWindow[idx:]
	}
}

// pruneRPMWindow removes timestamps older than 60 seconds.
func (b *Budget) pruneRPMWindow() {
	cutoff := time.Now().Add(-time.Minute)
	idx := 0
	for i, ts := range b.requestTimestamps {
		if ts.After(cutoff) {
			idx = i
			break
		}
		idx = len(b.requestTimestamps) // All expired
	}
	if idx > 0 {
		b.requestTimestamps = b.requestTimestamps[idx:]
	}
}

// maybeResetDaily resets the daily counter if we've crossed into a new UTC day.
func (b *Budget) maybeResetDaily() {
	today := dayOrdinal(time.Now().UTC())
	if today != b.currentDay {
		b.logger.Info("Daily token budget reset (new UTC day)")
		b.dailyUsed = 0
		b.currentDay = today
	}
}

// hourlyUsed returns total tokens used in the current sliding hour.
func (b *Budget) hourlyUsed() int {
	b.pruneHourlyWindow()
	total := 0
	for _, rec := range b.hourlyWindow {
		total += rec.tokens
	}
	return total
}

// RecordUsage records a completed API call's token usage.
func (b *Budget) RecordUsage(usage TokenUsage) {
	if usage.TotalTokens < 0 {
		b.logger.Warn("Negative token count - ignoring", "tokens", usage.TotalTokens)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	b.maybeResetDaily()

	b.hourlyWindow = append(b.hourlyWindow, usageRecord{
		timestamp: now,
		tokens:    usage.TotalTokens,
	})
	b.dailyUsed += usage.TotalTokens
	b.requestTimestamps = append(b.requestTimestamps, now)

	b.logger.Debug("Recorded token usage",
		"tokens", usage.TotalTokens,
		"hourly", b.hourlyUsed(),
		"daily", b.dailyUsed,
	)
}

// RecordUsageWithScope records token usage and tracks it per-task and per-session.
func (b *Budget) RecordUsageWithScope(usage TokenUsage, taskID, sessionID string) {
	if usage.TotalTokens < 0 {
		b.logger.Warn("Negative token count - ignoring", "tokens", usage.TotalTokens)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	b.maybeResetDaily()

	b.hourlyWindow = append(b.hourlyWindow, usageRecord{
		timestamp: now,
		tokens:    usage.TotalTokens,
	})
	b.dailyUsed += usage.TotalTokens
	b.requestTimestamps = append(b.requestTimestamps, now)

	// Track per-task
	if len(taskID) > 0 {
		b.tasks[taskID] += usage.TotalTokens
	}
	// Track per-session
	if len(sessionID) > 0 {
		b.sessions[sessionID] += usage.TotalTokens
	}

	b.logger.Debug("Recorded token usage with scope",
		"tokens", usage.TotalTokens,
		"hourly", b.hourlyUsed(),
		"daily", b.dailyUsed,
		"task", taskID,
		"session", sessionID,
	)
}

// CheckBudget returns true if the current usage is within all budget limits.
// When both hourlyLimit and dailyLimit are 0 (unconfigured), all requests are allowed.
func (b *Budget) CheckBudget() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Allow all requests when both budget limits are unconfigured (zero)
	if b.hourlyLimit == 0 && b.dailyLimit == 0 {
		return true
	}

	b.maybeResetDaily()
	b.pruneHourlyWindow()

	// Only enforce limits that are configured (non-zero)
	var hourlyOK, dailyOK bool
	if b.hourlyLimit > 0 {
		hourlyOK = b.hourlyUsed() < b.effectiveLimit(b.hourlyLimit)
	} else {
		hourlyOK = true
	}
	if b.dailyLimit > 0 {
		dailyOK = b.dailyUsed < b.effectiveLimit(b.dailyLimit)
	} else {
		dailyOK = true
	}
	return hourlyOK && dailyOK
}

// CheckBudgetWithScope validates budgets including per-task and per-session caps.
// taskID and sessionID can be empty strings if per-task/per-session caps are not configured.
func (b *Budget) CheckBudgetWithScope(taskID, sessionID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Allow all requests when budget is unconfigured (limits = 0)
	if b.hourlyLimit == 0 && b.dailyLimit == 0 {
		return true
	}

	b.maybeResetDaily()
	b.pruneHourlyWindow()

	// Per-task cap check
	if b.perTaskBudget > 0 && len(taskID) > 0 {
		if taskUsed, exists := b.tasks[taskID]; exists {
			if taskUsed >= b.perTaskBudget {
				return false
			}
		}
	}

	// Per-session cap check
	if b.perSessionBudget > 0 && len(sessionID) > 0 {
		if sessionUsed, exists := b.sessions[sessionID]; exists {
			if sessionUsed >= b.perSessionBudget {
				return false
			}
		}
	}

	// Only enforce limits that are configured (non-zero)
	var hourlyOK, dailyOK bool
	if b.hourlyLimit > 0 {
		hourlyOK = b.hourlyUsed() < b.effectiveLimit(b.hourlyLimit)
	} else {
		hourlyOK = true
	}
	if b.dailyLimit > 0 {
		dailyOK = b.dailyUsed < b.effectiveLimit(b.dailyLimit)
	} else {
		dailyOK = true
	}

	return hourlyOK && dailyOK
}

// RecordTaskUsage tracks tokens consumed by a specific task.
func (b *Budget) RecordTaskUsage(taskID string, tokens int) {
	if tokens <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tasks[taskID] += tokens
}

// RecordSessionUsage tracks tokens consumed by a specific session.
func (b *Budget) RecordSessionUsage(sessionID string, tokens int) {
	if tokens <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sessions[sessionID] += tokens
}

// Status represents a snapshot of current budget status.
type Status struct {
	HourlyUsed         int     `json:"hourly_used"`
	HourlyLimit        int     `json:"hourly_limit"`
	HourlyRemaining    int     `json:"hourly_remaining"`
	DailyUsed          int     `json:"daily_used"`
	DailyLimit         int     `json:"daily_limit"`
	DailyRemaining     int     `json:"daily_remaining"`
	PerTaskBudget      int     `json:"per_task_budget"`
	PerTaskUsed        int     `json:"per_task_used"`
	PerSessionBudget   int     `json:"per_session_budget"`
	PerSessionUsed     int     `json:"per_session_used"`
	RPMCurrent         int     `json:"rpm_current"`
	RPMLimit           int     `json:"rpm_limit"`
	Aggressiveness     float64 `json:"aggressiveness"`
	WithinBudget       bool    `json:"within_budget"`
	TaskBudgetExhausted bool    `json:"task_budget_exhausted"`
	SessionBudgetExhausted bool `json:"session_budget_exhausted"`
}

// GetStatus returns a snapshot of current budget status.
func (b *Budget) GetStatus() Status {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.maybeResetDaily()
	b.pruneHourlyWindow()
	b.pruneRPMWindow()

	effHourly := b.effectiveLimit(b.hourlyLimit)
	effDaily := b.effectiveLimit(b.dailyLimit)
	hourlyUsed := b.hourlyUsed()

	hourlyRemaining := max(effHourly-hourlyUsed, 0)
	dailyRemaining := max(effDaily-b.dailyUsed, 0)

	// Aggregate task/session usage
	totalTaskUsed := 0
	for _, v := range b.tasks {
		totalTaskUsed += v
	}
	totalSessionUsed := 0
	for _, v := range b.sessions {
		totalSessionUsed += v
	}

	// Check if any specific task or session has exhausted its cap
	taskBudgetExhausted := false
	sessionBudgetExhausted := false
	if b.perTaskBudget > 0 {
		for _, used := range b.tasks {
			if used >= b.perTaskBudget {
				taskBudgetExhausted = true
				break
			}
		}
	}
	if b.perSessionBudget > 0 {
		for _, used := range b.sessions {
			if used >= b.perSessionBudget {
				sessionBudgetExhausted = true
				break
			}
		}
	}

	return Status{
		HourlyUsed:             hourlyUsed,
		HourlyLimit:            effHourly,
		HourlyRemaining:        hourlyRemaining,
		DailyUsed:              b.dailyUsed,
		DailyLimit:             effDaily,
		DailyRemaining:         dailyRemaining,
		PerTaskBudget:          b.perTaskBudget,
		PerTaskUsed:            totalTaskUsed,
		PerSessionBudget:       b.perSessionBudget,
		PerSessionUsed:         totalSessionUsed,
		RPMCurrent:             len(b.requestTimestamps),
		RPMLimit:               b.rateLimitRPM,
		Aggressiveness:         b.aggressiveness,
		WithinBudget:           hourlyUsed < effHourly && b.dailyUsed < effDaily,
		TaskBudgetExhausted:    taskBudgetExhausted,
		SessionBudgetExhausted: sessionBudgetExhausted,
	}
}

// WaitForRateLimit blocks until the RPM rate limit window allows another request.
// If rateLimitRPM is 0 (unlimited), this returns immediately.
func (b *Budget) WaitForRateLimit(ctx context.Context) error {
	if b.rateLimitRPM <= 0 {
		return nil
	}

	b.mu.Lock()
	b.pruneRPMWindow()

	if len(b.requestTimestamps) < b.rateLimitRPM {
		b.mu.Unlock()
		return nil
	}

	// Calculate how long until the oldest request falls out of the window
	oldest := b.requestTimestamps[0]
	waitDuration := time.Minute - time.Since(oldest)
	b.mu.Unlock()

	if waitDuration <= 0 {
		return nil
	}

	b.logger.Info("Rate limited - waiting", "duration", waitDuration)

	select {
	case <-time.After(waitDuration):
		b.mu.Lock()
		b.pruneRPMWindow()
		b.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// BudgetExceededError is returned when a request would exceed the token budget.
// It implements NonRetryableError because budget exhaustion cannot be resolved
// by retrying the same request.
type BudgetExceededError struct {
	Message string
}

func (e *BudgetExceededError) Error() string {
	return e.Message
}

// NonRetryable returns true because budget exhaustion cannot be resolved by retry.
func (e *BudgetExceededError) NonRetryable() bool {
	return true
}

// Ensure BudgetExceededError implements NonRetryableError
var _ NonRetryableError = (*BudgetExceededError)(nil)
