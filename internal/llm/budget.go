package llm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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
	perTaskBudget       int     // max tokens per single task
	perSessionBudget    int     // max tokens per single session
	perTaskCostLimit    float64 // max USD per single task (0 = no cap)
	perSessionCostLimit float64 // max USD per single session (0 = no cap)

	// Sliding window for the last hour
	hourlyWindow []usageRecord

	// Daily tracking - reset at midnight UTC
	dailyUsed  int
	currentDay int // ordinal day number

	// Per-task tracking
	tasks     map[string]int     // taskID -> tokens used
	taskCosts map[string]float64 // taskID -> USD cost

	// Per-session tracking
	sessions     map[string]int     // sessionID -> tokens used
	sessionCosts map[string]float64 // sessionID -> USD cost

	// RPM tracking (sliding window of request timestamps)
	requestTimestamps []time.Time

	// Dollar cost tracking
	dailyCostLimit  float64
	hourlyCostLimit float64

	// Hourly cost sliding window
	hourlyCostWindow []costRecord

	// Daily cost tracking — reset at midnight UTC
	dailyCostUsed float64

	// Advisory-mode state (Contract C7): mode gates enforcement; the
	// advisory dedup set tracks which (limit|task|session) breaches have
	// already emitted a signal so one breach episode yields exactly one
	// BudgetAdvisorySignal, not one per budget check. advisorySeen is
	// cleared when a check observes no breach, re-arming the edge.
	mode              BudgetMode
	advisorySeen      map[string]struct{}
	advisoryPublisher AdvisoryPublisher

	logger *slog.Logger
}

// BudgetMode selects how a breached budget is enforced (Contract C7).
type BudgetMode string

const (
	// BudgetModeEnforce is the DEFAULT: a breached budget returns
	// *BudgetExceededError exactly as before this leaf.
	BudgetModeEnforce BudgetMode = "enforce"
	// BudgetModeSteer never refuses a turn: the budget check reports
	// "allowed", Advisory() reports the breach, and one
	// BudgetAdvisorySignal rides the existing agent.quota_wait topic with
	// class "budget_advisory".
	BudgetModeSteer BudgetMode = "steer"
)

// ParseBudgetMode maps a config string to a BudgetMode. "" and any
// unrecognized value map to BudgetModeEnforce (backward compatible; the
// default posture is byte-identical to today).
func ParseBudgetMode(s string) BudgetMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(BudgetModeSteer):
		return BudgetModeSteer
	default:
		return BudgetModeEnforce
	}
}

// AdvisoryVerdict reports the breach state WITHOUT blocking (Contract C7).
// It reads the same counters CheckBudgetWithScope maintains; it never
// mutates them.
type AdvisoryVerdict struct {
	OverTarget bool
	Limit      BudgetLimit // the existing BudgetLimit enum
	Used       float64     // tokens or USD, matching Limit's unit
	Cap        float64     // the configured limit after aggressiveness
}

// BudgetAdvisorySignal is the advisory payload (Contract C7). It rides the
// EXISTING agent.quota_wait topic with Class "budget_advisory" — never a
// new topic, never a new WS type.
type BudgetAdvisorySignal struct {
	Class string // "budget_advisory"
	Limit string // the BudgetLimit reason string
	Used  float64
	Cap   float64
	Scope string // "hourly" | "daily" | "per_task" | "per_session"
}

// BudgetAdvisoryClass is the wire class carried by the advisory signal on
// the agent.quota_wait topic.
const BudgetAdvisoryClass = "budget_advisory"

// AdvisoryPublisher is the emission seam for BudgetAdvisorySignal. The
// daemon wiring installs a publisher that marshals the signal onto the
// EXISTING agent.quota_wait topic (class "budget_advisory", same shape as
// the quota park events). A nil publisher drops the signal silently —
// Advisory() remains the in-process source of truth. Never called under
// b.mu: publishers do I/O.
type AdvisoryPublisher func(BudgetAdvisorySignal)

// SetAdvisoryPublisher installs the advisory emission seam. Nil-guarded at
// the call site per repo setter convention: a nil publisher is refused.
func (b *Budget) SetAdvisoryPublisher(p AdvisoryPublisher) {
	if p == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.advisoryPublisher = p
}

// BudgetConfig holds configuration for token budget tracking.
type BudgetConfig struct {
	HourlyLimit         int
	DailyLimit          int
	DailyCostLimit      float64 // Max dollar cost per UTC day (0 = no limit)
	HourlyCostLimit     float64 // Max dollar cost per sliding hour (0 = no limit)
	RateLimitRPM        int
	Aggressiveness      float64
	PerTaskBudget       int     // max tokens per single task (0 = no cap)
	PerSessionBudget    int     // max tokens per single session (0 = no cap)
	PerTaskCostLimit    float64 // max USD per single task (0 = no cap)
	PerSessionCostLimit float64 // max USD per single session (0 = no cap)
	// Mode selects enforce (default; "" or unknown -> enforce) or steer.
	Mode BudgetMode
}

// NewBudget creates a new token budget tracker.
//
// This is the enforcement side of the budget contract: budget VALUES come from
// meept.json5 and nowhere else. A zero field means "unlimited" for that limit,
// so an all-zero config (the disabled posture) enforces nothing.
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
		mode:                ParseBudgetMode(string(cfg.Mode)),
		advisorySeen:        make(map[string]struct{}),
		hourlyLimit:         cfg.HourlyLimit,
		dailyLimit:          cfg.DailyLimit,
		rateLimitRPM:        cfg.RateLimitRPM,
		aggressiveness:      cfg.Aggressiveness,
		perTaskBudget:       cfg.PerTaskBudget,
		perSessionBudget:    cfg.PerSessionBudget,
		perTaskCostLimit:    cfg.PerTaskCostLimit,
		perSessionCostLimit: cfg.PerSessionCostLimit,
		hourlyWindow:        make([]usageRecord, 0),
		dailyUsed:           0,
		currentDay:          dayOrdinal(now),
		requestTimestamps:   make([]time.Time, 0),
		dailyCostLimit:      cfg.DailyCostLimit,
		hourlyCostLimit:     cfg.HourlyCostLimit,
		hourlyCostWindow:    make([]costRecord, 0),
		tasks:               make(map[string]int),
		taskCosts:           make(map[string]float64),
		sessions:            make(map[string]int),
		sessionCosts:        make(map[string]float64),
		logger:              logger,
	}
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
		b.hourlyWindow = b.hourlyWindow[:0] // D7: Also reset hourly window (was asymmetric)
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
	b.RecordCostWithScope(r, "", "")
}

// RecordCostWithScope records a dollar cost and tracks it per-task and per-session.
func (b *Budget) RecordCostWithScope(r CostRecord, taskID, sessionID string) {
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

	// Track per-task cost
	if len(taskID) > 0 {
		b.taskCosts[taskID] += r.CostUSD
	}
	// Track per-session cost
	if len(sessionID) > 0 {
		b.sessionCosts[sessionID] += r.CostUSD
	}

	b.logger.Debug("Recorded dollar cost with scope",
		"cost_usd", r.CostUSD,
		"hourly_cost", b.hourlyCostUsed(),
		"daily_cost", b.dailyCostUsed,
		"task", taskID,
		"session", sessionID,
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

// effectiveTokenCount returns the best available token count from a usage
// record. Some providers set TotalTokens while others leave it 0 and only
// populate PromptTokens + CompletionTokens. Fall back to the sum so budgets
// are enforced correctly regardless of provider behavior.
func effectiveTokenCount(usage TokenUsage) int {
	if usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	return usage.PromptTokens + usage.CompletionTokens
}

// RecordUsage records a completed API call's token usage.
func (b *Budget) RecordUsage(usage TokenUsage) {
	tokens := effectiveTokenCount(usage)
	if tokens < 0 {
		b.logger.Warn("Negative token count - ignoring", "tokens", tokens)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	b.maybeResetDaily()

	b.hourlyWindow = append(b.hourlyWindow, usageRecord{
		timestamp: now,
		tokens:    tokens,
	})
	b.dailyUsed += tokens
	// RPM timestamp is reserved by WaitForRateLimit (sole appender for
	// requestTimestamps) to avoid double-counting: exactly one timestamp per request.

	b.logger.Debug("Recorded token usage",
		"tokens", usage.TotalTokens,
		"hourly", b.hourlyUsed(),
		"daily", b.dailyUsed,
	)
}

// RecordUsageWithScope records token usage and tracks it per-task and per-session.
func (b *Budget) RecordUsageWithScope(usage TokenUsage, taskID, sessionID string) {
	tokens := effectiveTokenCount(usage)
	if tokens < 0 {
		b.logger.Warn("Negative token count - ignoring", "tokens", tokens)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	b.maybeResetDaily()

	b.hourlyWindow = append(b.hourlyWindow, usageRecord{
		timestamp: now,
		tokens:    tokens,
	})
	b.dailyUsed += tokens
	// RPM timestamp is reserved by WaitForRateLimit (sole appender for
	// requestTimestamps) to avoid double-counting: exactly one timestamp per request.

	// Track per-task
	if len(taskID) > 0 {
		b.tasks[taskID] += tokens
	}
	// Track per-session
	if len(sessionID) > 0 {
		b.sessions[sessionID] += tokens
	}

	b.logger.Debug("Recorded token usage with scope",
		"tokens", tokens,
		"hourly", b.hourlyUsed(),
		"daily", b.dailyUsed,
		"task", taskID,
		"session", sessionID,
	)
}

// CheckBudget returns a BudgetCheckResult indicating whether the current usage
// is within all budget limits. When both hourlyLimit and dailyLimit are 0
// (unconfigured), all requests are allowed.
//
// Contract C7: in steer mode a breach NEVER returns Exceeded=true; the turn
// is allowed and exactly one BudgetAdvisorySignal per breach episode is
// emitted on the wired publisher (agent.quota_wait, class budget_advisory).
func (b *Budget) CheckBudget() BudgetCheckResult {
	b.mu.Lock()
	res, sigs := b.applyModeGateLocked(b.checkBudgetLocked("", ""), "", "")
	b.mu.Unlock()
	b.emitPending(sigs)
	return res
}

// CheckBudgetWithScope validates budgets including per-task and per-session caps.
// taskID and sessionID can be empty strings if per-task/per-session caps are not configured.
func (b *Budget) CheckBudgetWithScope(taskID, sessionID string) BudgetCheckResult {
	b.mu.Lock()
	res, sigs := b.applyModeGateLocked(b.checkBudgetLocked(taskID, sessionID), taskID, sessionID)
	b.mu.Unlock()
	b.emitPending(sigs)
	return res
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

// RemoveTaskCost removes a completed task's cost tracking entry.
func (b *Budget) RemoveTaskCost(_ context.Context, taskID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.taskCosts, taskID)
}

// RemoveSessionCost removes a completed session's cost tracking entry.
func (b *Budget) RemoveSessionCost(_ context.Context, sessionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sessionCosts, sessionID)
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
	PerTaskCost            float64 `json:"per_task_cost"`
	PerSessionBudget       int     `json:"per_session_budget"`
	PerSessionUsed         int     `json:"per_session_used"`
	PerSessionCost         float64 `json:"per_session_cost"`
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
	totalTaskCost := 0.0
	for _, v := range b.tasks {
		totalTaskUsed += v
	}
	for _, cost := range b.taskCosts {
		totalTaskCost += cost
	}
	totalSessionUsed := 0
	totalSessionCost := 0.0
	for _, v := range b.sessions {
		totalSessionUsed += v
	}
	for _, cost := range b.sessionCosts {
		totalSessionCost += cost
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

	// A token limit only constrains when > 0 (0 = disabled/unlimited) —
	// the same guard shape the cost half above uses. Without it, a
	// disabled limit (effHourly == 0) made `hourlyUsed < 0` false and an
	// unlimited config reported WithinBudget == false.
	withinTokenBudget := true
	if b.hourlyLimit > 0 {
		withinTokenBudget = hourlyUsed < effHourly
	}
	if b.dailyLimit > 0 {
		withinTokenBudget = withinTokenBudget && b.dailyUsed < effDaily
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
		PerTaskCost:            totalTaskCost,
		PerSessionBudget:       b.perSessionBudget,
		PerSessionUsed:         totalSessionUsed,
		PerSessionCost:         totalSessionCost,
		RPMCurrent:             len(b.requestTimestamps),
		RPMLimit:               b.rateLimitRPM,
		Aggressiveness:         b.aggressiveness,
		WithinBudget:           withinTokenBudget && withinCostBudget,
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
//
// This method is the sole appender to requestTimestamps. It reserves a
// rate-limit slot by appending time.Now() when capacity is available.
// Without the reservation, N concurrent callers would all observe the same
// spare capacity and all proceed, exceeding RPM. RecordUsage does NOT append
// to requestTimestamps — the reservation here is the single source of truth
// for RPM accounting, giving exactly one timestamp per request.
func (b *Budget) WaitForRateLimit(ctx context.Context) error {
	if b.rateLimitRPM <= 0 {
		return nil
	}

	for {
		b.mu.Lock()
		b.pruneRPMWindow()

		if len(b.requestTimestamps) < b.rateLimitRPM {
			// Reserve a slot atomically with the capacity check so that
			// concurrent callers see one fewer free slot.
			b.requestTimestamps = append(b.requestTimestamps, time.Now())
			b.mu.Unlock()
			return nil
		}

		// Over limit: calculate how long until the oldest request expires.
		oldest := b.requestTimestamps[0]
		waitDuration := time.Minute - time.Since(oldest)
		b.mu.Unlock()

		if waitDuration <= 0 {
			// Oldest timestamp should have been pruned; loop to re-check.
			continue
		}

		b.logger.Info("Rate limited - waiting", "duration", waitDuration)

		select {
		case <-time.After(waitDuration):
			// Loop back and re-check under the lock.
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// ReleaseRateLimitSlot releases the most recently reserved RPM slot.
// Call this when a request that called WaitForRateLimit fails before
// reaching the API (e.g., concurrency limit acquisition fails, payload
// marshalling error), so the reserved timestamp doesn't artificially
// consume rate-limit capacity.
func (b *Budget) ReleaseRateLimitSlot() {
	if b.rateLimitRPM <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.requestTimestamps) == 0 {
		return
	}
	// Remove the most recent entry (the reservation).
	b.requestTimestamps = b.requestTimestamps[:len(b.requestTimestamps)-1]
}

// BudgetLimit identifies which budget limit was exceeded.
type BudgetLimit string

const (
	BudgetLimitHourlyTokens   BudgetLimit = "hourly_token"
	BudgetLimitDailyTokens    BudgetLimit = "daily_token"
	BudgetLimitHourlyCost     BudgetLimit = "hourly_cost"
	BudgetLimitDailyCost      BudgetLimit = "daily_cost"
	BudgetLimitPerTask        BudgetLimit = "per_task"
	BudgetLimitPerSession     BudgetLimit = "per_session"
	BudgetLimitPerTaskCost    BudgetLimit = "per_task_cost"
	BudgetLimitPerSessionCost BudgetLimit = "per_session_cost"
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
	case BudgetLimitPerTaskCost:
		return fmt.Sprintf("per-task cost budget exceeded: $%.4f / $%.4f", used, limit)
	case BudgetLimitPerSessionCost:
		return fmt.Sprintf("per-session cost budget exceeded: $%.4f / $%.4f", used, limit)
	default:
		return "budget limit exceeded"
	}
}

// BudgetCheckResult describes the outcome of a budget check.
// Mode returns the budget's enforcement mode (Contract C7).
func (b *Budget) Mode() BudgetMode {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.mode == "" {
		return BudgetModeEnforce
	}
	return b.mode
}

// budgetLimitScope maps a BudgetLimit reason to its wire scope.
func budgetLimitScope(l BudgetLimit) string {
	switch l {
	case BudgetLimitHourlyTokens, BudgetLimitHourlyCost:
		return "hourly"
	case BudgetLimitDailyTokens, BudgetLimitDailyCost:
		return "daily"
	case BudgetLimitPerTask, BudgetLimitPerTaskCost:
		return "per_task"
	case BudgetLimitPerSession, BudgetLimitPerSessionCost:
		return "per_session"
	default:
		return ""
	}
}

// advisorySignalFor builds the wire signal for a breach result.
func advisorySignalFor(res BudgetCheckResult) BudgetAdvisorySignal {
	return BudgetAdvisorySignal{
		Class: BudgetAdvisoryClass,
		Limit: string(res.Reason),
		Used:  res.Used,
		Cap:   res.Limit,
		Scope: budgetLimitScope(res.Reason),
	}
}

// Advisory reports the breach state WITHOUT blocking (Contract C7). It
// reads the same counters RecordCost maintains; it never mutates them.
// The first global (hourly/daily) breach is reported, in the same
// precedence CheckBudget uses.
func (b *Budget) Advisory() AdvisoryVerdict {
	b.mu.Lock()
	defer b.mu.Unlock()
	res := b.checkBudgetLocked("", "")
	return AdvisoryVerdict{
		OverTarget: res.Exceeded,
		Limit:      res.Reason,
		Used:       res.Used,
		Cap:        res.Limit,
	}
}

// noteAdvisoryLocked records a breach for dedup and returns the signal to
// publish, or false when this exact breach episode already signalled.
func (b *Budget) noteAdvisoryLocked(res BudgetCheckResult, taskID, sessionID string) (BudgetAdvisorySignal, bool) {
	// Global (hourly/daily) breaches are the SAME episode regardless of
	// which caller checks them: the async handler pre-check uses an empty
	// scope while the client budget gate uses the real task/session scope.
	// Keying those on scope emitted two budget_advisory signals for one
	// breach. Per-task/per-session breaches stay scope-specific.
	scope := budgetLimitScope(res.Reason)
	if scope != "per_task" && scope != "per_session" {
		taskID, sessionID = "", ""
	}
	key := string(res.Reason) + "|" + taskID + "|" + sessionID
	if _, seen := b.advisorySeen[key]; seen {
		return BudgetAdvisorySignal{}, false
	}
	b.advisorySeen[key] = struct{}{}
	return advisorySignalFor(res), true
}

// clearAdvisoryLocked re-arms the edge when a check observes no breach.
func (b *Budget) clearAdvisoryLocked() {
	if len(b.advisorySeen) > 0 {
		b.advisorySeen = make(map[string]struct{})
	}
}

// emitPending publishes collected signals OUTSIDE b.mu (publishers do I/O).
func (b *Budget) emitPending(sigs []BudgetAdvisorySignal) {
	b.mu.Lock()
	p := b.advisoryPublisher
	b.mu.Unlock()
	for _, sig := range sigs {
		if p == nil {
			b.logger.Debug("budget advisory signal (no publisher wired; dropping)",
				"limit", sig.Limit, "used", sig.Used, "cap", sig.Cap, "scope", sig.Scope)
			continue
		}
		p(sig)
	}
}

// checkBudgetLocked is the shared decision table for CheckBudget /
// CheckBudgetWithScope / Advisory. Callers hold b.mu. taskID/sessionID may
// be empty (CheckBudget / Advisory semantics: global limits only).
func (b *Budget) checkBudgetLocked(taskID, sessionID string) BudgetCheckResult {
	// Allow all requests when budget is unconfigured (limits = 0)
	if b.hourlyLimit == 0 && b.dailyLimit == 0 && b.dailyCostLimit == 0 && b.hourlyCostLimit == 0 &&
		b.perTaskBudget == 0 && b.perSessionBudget == 0 && b.perTaskCostLimit == 0 && b.perSessionCostLimit == 0 {
		return BudgetCheckResult{Exceeded: false}
	}

	b.maybeResetDaily()
	b.pruneHourlyWindow()

	// Per-task token cap check
	if b.perTaskBudget > 0 && len(taskID) > 0 {
		if taskUsed, exists := b.tasks[taskID]; exists {
			if taskUsed >= b.perTaskBudget {
				return BudgetCheckResult{Exceeded: true, Reason: BudgetLimitPerTask, Used: float64(taskUsed), Limit: float64(b.perTaskBudget)}
			}
		}
	}

	// Per-session token cap check
	if b.perSessionBudget > 0 && len(sessionID) > 0 {
		if sessionUsed, exists := b.sessions[sessionID]; exists {
			if sessionUsed >= b.perSessionBudget {
				return BudgetCheckResult{Exceeded: true, Reason: BudgetLimitPerSession, Used: float64(sessionUsed), Limit: float64(b.perSessionBudget)}
			}
		}
	}

	// Per-task cost cap check
	if b.perTaskCostLimit > 0 && len(taskID) > 0 {
		if taskCost, exists := b.taskCosts[taskID]; exists {
			if taskCost >= b.perTaskCostLimit {
				return BudgetCheckResult{Exceeded: true, Reason: BudgetLimitPerTaskCost, Used: taskCost, Limit: b.perTaskCostLimit}
			}
		}
	}

	// Per-session cost cap check
	if b.perSessionCostLimit > 0 && len(sessionID) > 0 {
		if sessionCost, exists := b.sessionCosts[sessionID]; exists {
			if sessionCost >= b.perSessionCostLimit {
				return BudgetCheckResult{Exceeded: true, Reason: BudgetLimitPerSessionCost, Used: sessionCost, Limit: b.perSessionCostLimit}
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

// applyModeGate applies Contract C7 to a locked breach result: in enforce
// mode the result is returned unchanged (byte-identical to today); in steer
// mode the breach becomes "allowed" and at most one advisory signal per
// breach episode is collected for the caller to publish after unlock.
func (b *Budget) applyModeGateLocked(res BudgetCheckResult, taskID, sessionID string) (BudgetCheckResult, []BudgetAdvisorySignal) {
	if b.mode != BudgetModeSteer {
		if !res.Exceeded {
			b.clearAdvisoryLocked()
		}
		return res, nil
	}
	if !res.Exceeded {
		b.clearAdvisoryLocked()
		return res, nil
	}
	sig, ok := b.noteAdvisoryLocked(res, taskID, sessionID)
	if !ok {
		return BudgetCheckResult{Exceeded: false}, nil
	}
	return BudgetCheckResult{Exceeded: false}, []BudgetAdvisorySignal{sig}
}

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
	case BudgetLimitPerTaskCost:
		return fmt.Sprintf("meept per-task cost budget reached: $%.4f / $%.4f used (config: llm.budget.per_task_cost_limit)", e.Used, e.Limit)
	case BudgetLimitPerSessionCost:
		return fmt.Sprintf("meept per-session cost budget reached: $%.4f / $%.4f used (config: llm.budget.per_session_cost_limit)", e.Used, e.Limit)
	default:
		if e.Used > 0 || e.Limit > 0 {
			return fmt.Sprintf("meept budget limit reached: %.0f / %.0f", e.Used, e.Limit)
		}
		return "meept budget limit reached"
	}
}

// Ensure BudgetExceededError implements NonRetryableError
var _ NonRetryableError = (*BudgetExceededError)(nil)
