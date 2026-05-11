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

	// Sliding window for the last hour
	hourlyWindow []usageRecord

	// Daily tracking - reset at midnight UTC
	dailyUsed  int
	currentDay int // ordinal day number

	// RPM tracking (sliding window of request timestamps)
	requestTimestamps []time.Time

	logger *slog.Logger
}

// BudgetConfig holds configuration for token budget tracking.
type BudgetConfig struct {
	HourlyLimit    int
	DailyLimit     int
	RateLimitRPM   int
	Aggressiveness float64
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
		hourlyLimit:       cfg.HourlyLimit,
		dailyLimit:        cfg.DailyLimit,
		rateLimitRPM:      cfg.RateLimitRPM,
		aggressiveness:    cfg.Aggressiveness,
		hourlyWindow:      make([]usageRecord, 0),
		dailyUsed:         0,
		currentDay:        dayOrdinal(now),
		requestTimestamps: make([]time.Time, 0),
		logger:            logger,
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

// CheckBudget returns true if the current usage is within all budget limits.
func (b *Budget) CheckBudget() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.maybeResetDaily()
	b.pruneHourlyWindow()

	hourlyOK := b.hourlyUsed() < b.effectiveLimit(b.hourlyLimit)
	dailyOK := b.dailyUsed < b.effectiveLimit(b.dailyLimit)
	return hourlyOK && dailyOK
}

// Status represents a snapshot of current budget status.
type Status struct {
	HourlyUsed      int     `json:"hourly_used"`
	HourlyLimit     int     `json:"hourly_limit"`
	HourlyRemaining int     `json:"hourly_remaining"`
	DailyUsed       int     `json:"daily_used"`
	DailyLimit      int     `json:"daily_limit"`
	DailyRemaining  int     `json:"daily_remaining"`
	RPMCurrent      int     `json:"rpm_current"`
	RPMLimit        int     `json:"rpm_limit"`
	Aggressiveness  float64 `json:"aggressiveness"`
	WithinBudget    bool    `json:"within_budget"`
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

	return Status{
		HourlyUsed:      hourlyUsed,
		HourlyLimit:     effHourly,
		HourlyRemaining: hourlyRemaining,
		DailyUsed:       b.dailyUsed,
		DailyLimit:      effDaily,
		DailyRemaining:  dailyRemaining,
		RPMCurrent:      len(b.requestTimestamps),
		RPMLimit:        b.rateLimitRPM,
		Aggressiveness:  b.aggressiveness,
		WithinBudget:    hourlyUsed < effHourly && b.dailyUsed < effDaily,
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
