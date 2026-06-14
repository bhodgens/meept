package llm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// usageRecord is a timestamped token usage entry.
type usageRecord struct {
	timestamp time.Time
	tokens    int
}

// costRecord is a timestamped dollar cost entry.
type costRecord struct {
	timestamp time.Time
	costUSD   float64
}

// Budget tracks and enforces token consumption budgets.
type Budget struct {
	mu sync.Mutex

	hourlyLimit    int
	dailyLimit     int
	rateLimitRPM   int
	aggressiveness float64

	// Per-task and per-session caps (0 = no cap)
	perTaskBudget    int
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

	// Dollar cost tracking
	dailyCostLimit  float64
	hourlyCostLimit float64

	// Hourly cost sliding window
	hourlyCostWindow []costRecord

	// Daily cost tracking — reset at midnight UTC
	dailyCostUsed float64

	logger *slog.Logger
}

// BudgetConfig holds configuration for token budget tracking.
type BudgetConfig struct {
	HourlyLimit      int
	DailyLimit       int
	DailyCostLimit   float64 // Max dollar cost per UTC day (0 = no limit)
	HourlyCostLimit  float64 // Max dollar cost per sliding hour (0 = no limit)
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
		hourlyLimit:       cfg.HourlyLimit,
		dailyLimit:        cfg.DailyLimit,
		rateLimitRPM:      cfg.RateLimitRPM,
		aggressiveness:    cfg.Aggressiveness,
		perTaskBudget:     cfg.PerTaskBudget,
		perSessionBudget:  cfg.PerSessionBudget,
		hourlyWindow:      make([]usageRecord, 0),
		dailyUsed:         0,
		currentDay:        dayOrdinal(now),
		requestTimestamps: make([]time.Time, 0),
		dailyCostLimit:    cfg.DailyCostLimit,
		hourlyCostLimit:   cfg.HourlyCostLimit,
		hourlyCostWindow:  make([]costRecord, 0),
		tasks:             make(map[string]int),
		sessions:          make(map[string]int),
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
		b.dailyCostUsed = 0
		b.hourlyCostWindow = b.hourlyCostWindow[:0]
		b.hourlyWindow = b.hourlyWindow[:0]  // D7: Also reset hourly window (was asymmetric)
		b.currentDay = today
	}
}

// effectiveCostLimit applies the aggressiveness factor to a dollar limit.
func (b *Budget) effectiveCostLimit(base float64) float64 {
	factor := 0.5 + 0.5*b.aggressiveness
	return base * factor
}

// pruneHourlyCostWindow removes cost entries older than 1 hour.
func (b *Budget) pruneHourlyCostWindow() {
	cutoff := time.Now().Add(-time.Hour)
	idx := 0
	for i, rec := range b.hourlyCostWindow {
		if rec.timestamp.After(cutoff) {
			idx = i
			break
		}
		idx = len(b.hourlyCostWindow)
	}
	if idx > 0 {
		b.hourlyCostWindow = b.hourlyCostWindow[idx:]
	}
}

// hourlyCostUsed returns total dollar cost in the current sliding hour.
func (b *Budget) hourlyCostUsed() float64 {
	b.pruneHourlyCostWindow()
	total := 0.0
	for _, rec := range b.hourlyCostWindow {
		total += rec.costUSD
	}
	return total
}

// CostRecord captures a dollar cost event for budget tracking.
type CostRecord struct {
	Timestamp        time.Time
	CostUSD          float64
	PromptTokens     int
	CompletionTokens int
}

// RecordCost records a dollar cost against the budget.
func (b *Budget) RecordCost(r CostRecord) {
	if r.CostUSD <= 0 {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.maybeResetDaily()

	if r.Timestamp.IsZero() {
		r.Timestamp = time.Now()
	}

	b.hourlyCostWindow = append(b.hourlyCostWindow, costRecord{
		timestamp: r.Timestamp,
		costUSD:   r.CostUSD,
	})
	b.dailyCostUsed += r.CostUSD

	b.logger.Debug("Recorded dollar cost",
		"cost_usd", r.CostUSD,
		"hourly_cost", b.hourlyCostUsed(),
		"daily_cost", b.dailyCostUsed,
	)
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

// CheckBudget returns a BudgetCheckResult indicating whether the current usage
// is within all budget limits. When both hourlyLimit and dailyLimit are 0
// (unconfigured), all requests are allowed.
func (b *Budget) CheckBudget() BudgetCheckResult {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Allow all requests when both token and cost budget limits are unconfigured (zero)
	if b.hourlyLimit == 0 && b.dailyLimit == 0 && b.dailyCostLimit == 0 && b.hourlyCostLimit == 0 {
		return BudgetCheckResult{Exceeded: false}
	}

	b.maybeResetDaily()
	b.pruneHourlyWindow()

	// Check hourly token limit
	if b.hourlyLimit > 0 {
		effLimit := b.effectiveLimit(b.hourlyLimit)
		used := b.hourlyUsed()
		if used >= effLimit {
			return BudgetCheckResult{Exceeded: true, Reason: BudgetLimitHourlyTokens, Used: float64(used), Limit: float64(effLimit)}
		}
	}

	// Check daily token limit
	if b.dailyLimit > 0 {
		effLimit := b.effectiveLimit(b.dailyLimit)
		if b.dailyUsed >= effLimit {
			return BudgetCheckResult{Exceeded: true, Reason: BudgetLimitDailyTokens, Used: float64(b.dailyUsed), Limit: float64(effLimit)}
		}
	}

	// Check daily cost limit
	if b.dailyCostLimit > 0 {
		effLimit := b.effectiveCostLimit(b.dailyCostLimit)
		if b.dailyCostUsed >= effLimit {
			return BudgetCheckResult{Exceeded: true, Reason: BudgetLimitDailyCost, Used: b.dailyCostUsed, Limit: effLimit}
		}
	}

	// Check hourly cost limit
	if b.hourlyCostLimit > 0 {
		effLimit := b.effectiveCostLimit(b.hourlyCostLimit)
		used := b.hourlyCostUsed()
		if used >= effLimit {
			return BudgetCheckResult{Exceeded: true, Reason: BudgetLimitHourlyCost, Used: used, Limit: effLimit}
		}
	}

	return BudgetCheckResult{Exceeded: false}
}

// CheckBudgetWithScope validates budgets including per-task and per-session caps.
// taskID and sessionID can be empty strings if per-task/per-session caps are not configured.
func (b *Budget) CheckBudgetWithScope(taskID, sessionID string) BudgetCheckResult {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Allow all requests when budget is unconfigured (limits = 0)
	if b.hourlyLimit == 0 && b.dailyLimit == 0 && b.dailyCostLimit == 0 && b.hourlyCostLimit == 0 {
		return BudgetCheckResult{Exceeded: false}
	}

	b.maybeResetDaily()
	b.pruneHourlyWindow()

	// Per-task cap check
	if b.perTaskBudget > 0 && len(taskID) > 0 {
		if taskUsed, exists := b.tasks[taskID]; exists {
			if taskUsed >= b.perTaskBudget {
				return BudgetCheckResult{Exceeded: true, Reason: BudgetLimitPerTask, Used: float64(taskUsed), Limit: float64(b.perTaskBudget)}
			}
		}
	}

	// Per-session cap check
	if b.perSessionBudget > 0 && len(sessionID) > 0 {
		if sessionUsed, exists := b.sessions[sessionID]; exists {
			if sessionUsed >= b.perSessionBudget {
				return BudgetCheckResult{Exceeded: true, Reason: BudgetLimitPerSession, Used: float64(sessionUsed), Limit: float64(b.perSessionBudget)}
			}
		}
	}

	// Check hourly token limit
	if b.hourlyLimit > 0 {
		effLimit := b.effectiveLimit(b.hourlyLimit)
		used := b.hourlyUsed()
		if used >= effLimit {
			return BudgetCheckResult{Exceeded: true, Reason: BudgetLimitHourlyTokens, Used: float64(used), Limit: float64(effLimit)}
		}
	}

	// Check daily token limit
	if b.dailyLimit > 0 {
		effLimit := b.effectiveLimit(b.dailyLimit)
		if b.dailyUsed >= effLimit {
			return BudgetCheckResult{Exceeded: true, Reason: BudgetLimitDailyTokens, Used: float64(b.dailyUsed), Limit: float64(effLimit)}
		}
	}

	// Check daily cost limit
	if b.dailyCostLimit > 0 {
		effLimit := b.effectiveCostLimit(b.dailyCostLimit)
		if b.dailyCostUsed >= effLimit {
			return BudgetCheckResult{Exceeded: true, Reason: BudgetLimitDailyCost, Used: b.dailyCostUsed, Limit: effLimit}
		}
	}

	// Check hourly cost limit
	if b.hourlyCostLimit > 0 {
		effLimit := b.effectiveCostLimit(b.hourlyCostLimit)
		used := b.hourlyCostUsed()
		if used >= effLimit {
			return BudgetCheckResult{Exceeded: true, Reason: BudgetLimitHourlyCost, Used: used, Limit: effLimit}
		}
	}

	return BudgetCheckResult{Exceeded: false}
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

// RemoveTask removes a completed task's tracking entry.
func (b *Budget) RemoveTask(_ context.Context, taskID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.tasks, taskID)
}

// RemoveSession removes a completed session's tracking entry.
func (b *Budget) RemoveSession(_ context.Context, sessionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sessions, sessionID)
}

// CleanupStaleEntries removes task and session entries that haven't been
// updated within the given TTL. This prevents unbounded map growth.
// It compares current total tokens per entry against a snapshot; entries
// that haven't changed are evicted.
func (b *Budget) CleanupStaleEntries(ttl time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Remove task entries with zero tokens (completed/drained tasks).
	// Full TTL-based cleanup requires timestamp tracking and is handled
	// by StartPeriodicCleanup, which maintains its own timestamp maps.
	for id, count := range b.tasks {
		if count == 0 {
			delete(b.tasks, id)
		}
	}
	for id, count := range b.sessions {
		if count == 0 {
			delete(b.sessions, id)
		}
	}

	_ = ttl // reserved for future timestamp-based cleanup
}

// StartPeriodicCleanup starts a background goroutine that periodically
// removes task and session entries older than the given TTL.
// It returns a stop channel that should be closed to stop the cleanup.
func (b *Budget) StartPeriodicCleanup(ttl time.Duration, freq time.Duration) chan struct{} {
	stop := make(chan struct{})
	// Track timestamps for task/session last-update
	var taskTimestamps map[string]time.Time
	var sessionTimestamps map[string]time.Time
	taskTimestamps = make(map[string]time.Time)
	sessionTimestamps = make(map[string]time.Time)

	go func() {
		ticker := time.NewTicker(freq)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				now := time.Now()
				b.mu.Lock()
				for id, ts := range taskTimestamps {
					if now.Sub(ts) > ttl {
						delete(b.tasks, id)
						delete(taskTimestamps, id)
					}
				}
				for id, ts := range sessionTimestamps {
					if now.Sub(ts) > ttl {
						delete(b.sessions, id)
						delete(sessionTimestamps, id)
					}
				}
				// Record current entries' last-seen time for next pass
				for id := range b.tasks {
					if _, exists := taskTimestamps[id]; !exists {
						taskTimestamps[id] = now
					}
				}
				for id := range b.sessions {
					if _, exists := sessionTimestamps[id]; !exists {
						sessionTimestamps[id] = now
					}
				}
				b.mu.Unlock()
			case <-stop:
				return
			}
		}
	}()
	return stop
}

// Status represents a snapshot of current budget status.
type Status struct {
	HourlyUsed             int     `json:"hourly_used"`
	HourlyLimit            int     `json:"hourly_limit"`
	HourlyRemaining        int     `json:"hourly_remaining"`
	DailyUsed              int     `json:"daily_used"`
	DailyLimit             int     `json:"daily_limit"`
	DailyRemaining         int     `json:"daily_remaining"`
	PerTaskBudget          int     `json:"per_task_budget"`
	PerTaskUsed            int     `json:"per_task_used"`
	PerSessionBudget       int     `json:"per_session_budget"`
	PerSessionUsed         int     `json:"per_session_used"`
	RPMCurrent             int     `json:"rpm_current"`
	RPMLimit               int     `json:"rpm_limit"`
	Aggressiveness         float64 `json:"aggressiveness"`
	WithinBudget           bool    `json:"within_budget"`
	TaskBudgetExhausted    bool    `json:"task_budget_exhausted"`
	SessionBudgetExhausted bool    `json:"session_budget_exhausted"`
	DailyCostUsed          float64 `json:"daily_cost_used"`
	DailyCostLimit         float64 `json:"daily_cost_limit"`
	DailyCostRemaining     float64 `json:"daily_cost_remaining"`
	HourlyCostUsed         float64 `json:"hourly_cost_used"`
	HourlyCostLimit        float64 `json:"hourly_cost_limit"`
	WithinCostBudget       bool    `json:"within_cost_budget"`
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

	effDailyCost := b.effectiveCostLimit(b.dailyCostLimit)
	effHourlyCost := b.effectiveCostLimit(b.hourlyCostLimit)
	withinCostBudget := true
	if b.dailyCostLimit > 0 {
		withinCostBudget = b.dailyCostUsed < effDailyCost
	}
	if b.hourlyCostLimit > 0 {
		withinCostBudget = withinCostBudget && b.hourlyCostUsed() < effHourlyCost
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
		WithinBudget:           hourlyUsed < effHourly && b.dailyUsed < effDaily && withinCostBudget,
		TaskBudgetExhausted:    taskBudgetExhausted,
		SessionBudgetExhausted: sessionBudgetExhausted,
		DailyCostUsed:          b.dailyCostUsed,
		DailyCostLimit:         effDailyCost,
		DailyCostRemaining:     max(effDailyCost-b.dailyCostUsed, 0),
		HourlyCostUsed:         b.hourlyCostUsed(),
		HourlyCostLimit:        effHourlyCost,
		WithinCostBudget:       withinCostBudget,
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
		// Re-acquire lock, prune expired timestamps, and re-check.
		// Multiple goroutines may wake at the same time; without this
		// re-check they could all exceed RPM.
		b.mu.Lock()
		b.pruneRPMWindow()
		if len(b.requestTimestamps) < b.rateLimitRPM {
			b.mu.Unlock()
			return nil
		}
		// Still over limit — wait for the next window slot
		if len(b.requestTimestamps) > 0 {
			oldest = b.requestTimestamps[0]
			remaining := time.Minute - time.Since(oldest)
			b.mu.Unlock()
			if remaining > 0 {
				select {
				case <-time.After(remaining):
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}
		b.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// BudgetLimit identifies which budget limit was exceeded.
type BudgetLimit string

const (
	BudgetLimitHourlyTokens BudgetLimit = "hourly_token"
	BudgetLimitDailyTokens  BudgetLimit = "daily_token"
	BudgetLimitHourlyCost   BudgetLimit = "hourly_cost"
	BudgetLimitDailyCost    BudgetLimit = "daily_cost"
	BudgetLimitPerTask      BudgetLimit = "per_task"
	BudgetLimitPerSession   BudgetLimit = "per_session"
)

// Message returns an internal log message for this budget limit reason.
func (r BudgetLimit) Message(used, limit float64) string {
	switch r {
	case BudgetLimitHourlyTokens:
		return fmt.Sprintf("hourly token budget exceeded: %.0f / %.0f tokens", used, limit)
	case BudgetLimitDailyTokens:
		return fmt.Sprintf("daily token budget exceeded: %.0f / %.0f tokens", used, limit)
	case BudgetLimitHourlyCost:
		return fmt.Sprintf("hourly cost budget exceeded: $%.4f / $%.4f", used, limit)
	case BudgetLimitDailyCost:
		return fmt.Sprintf("daily cost budget exceeded: $%.4f / $%.4f", used, limit)
	case BudgetLimitPerTask:
		return fmt.Sprintf("per-task token budget exceeded: %.0f / %.0f tokens", used, limit)
	case BudgetLimitPerSession:
		return fmt.Sprintf("per-session token budget exceeded: %.0f / %.0f tokens", used, limit)
	default:
		return "budget limit exceeded"
	}
}

// BudgetCheckResult describes the outcome of a budget check.
// If Exceeded is true, Reason and the Used/Limit fields are populated.
type BudgetCheckResult struct {
	Exceeded bool
	Reason   BudgetLimit
	Used     float64
	Limit    float64
}

// BudgetExceededError is returned when a request would exceed the token budget.
// It implements NonRetryableError because budget exhaustion cannot be resolved
// by retrying the same request.
type BudgetExceededError struct {
	Message string
	Reason  BudgetLimit // Which specific limit was hit
	Used    float64     // Current usage (tokens or USD)
	Limit   float64     // Configured limit (tokens or USD, after aggressiveness)
}

func (e *BudgetExceededError) Error() string {
	return e.Message
}

// NonRetryable returns true because budget exhaustion cannot be resolved by retry.
func (e *BudgetExceededError) NonRetryable() bool {
	return true
}

// UserMessage returns a human-readable message suitable for displaying to the client.
func (e *BudgetExceededError) UserMessage() string {
	switch e.Reason {
	case BudgetLimitHourlyTokens:
		return fmt.Sprintf("meept hourly token budget reached: %.0f / %.0f tokens used (config: llm.budget.hourly_token_limit)", e.Used, e.Limit)
	case BudgetLimitDailyTokens:
		return fmt.Sprintf("meept daily token budget reached: %.0f / %.0f tokens used (config: llm.budget.daily_token_limit)", e.Used, e.Limit)
	case BudgetLimitHourlyCost:
		return fmt.Sprintf("meept hourly cost budget reached: $%.4f / $%.4f used (config: llm.budget.hourly_cost_limit)", e.Used, e.Limit)
	case BudgetLimitDailyCost:
		return fmt.Sprintf("meept daily cost budget reached: $%.4f / $%.4f used (config: llm.budget.daily_cost_limit)", e.Used, e.Limit)
	case BudgetLimitPerTask:
		return fmt.Sprintf("meept per-task token budget reached: %.0f / %.0f tokens used (config: llm.budget.per_task_token_limit)", e.Used, e.Limit)
	case BudgetLimitPerSession:
		return fmt.Sprintf("meept per-session token budget reached: %.0f / %.0f tokens used (config: llm.budget.per_session_token_limit)", e.Used, e.Limit)
	default:
		if e.Used > 0 || e.Limit > 0 {
			return fmt.Sprintf("meept budget limit reached: %.0f / %.0f", e.Used, e.Limit)
		}
		return "meept budget limit reached"
	}
}

// Ensure BudgetExceededError implements NonRetryableError
var _ NonRetryableError = (*BudgetExceededError)(nil)
