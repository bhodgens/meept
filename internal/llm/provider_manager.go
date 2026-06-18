// Package llm provides LLM client functionality for OpenAI-compatible APIs.
package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// ProviderStatus represents the health status of a provider.
type ProviderStatus string

const (
	ProviderStatusHealthy   ProviderStatus = "healthy"
	ProviderStatusDegraded  ProviderStatus = "degraded"
	ProviderStatusUnhealthy ProviderStatus = "unhealthy"
	ProviderStatusDisabled  ProviderStatus = "disabled"
)

// ProviderHealth tracks health metrics for a provider.
type ProviderHealth struct {
	ProviderID       string         `json:"provider_id"`
	Status           ProviderStatus `json:"status"`
	LastSuccess      time.Time      `json:"last_success"`
	LastFailure      time.Time      `json:"last_failure"`
	SuccessCount     int64          `json:"success_count"`
	FailureCount     int64          `json:"failure_count"`
	ConsecutiveFails int            `json:"consecutive_fails"`
	AvgLatencyMs     float64        `json:"avg_latency_ms"`
	TotalCost        float64        `json:"total_cost"`
	TotalTokens      int64          `json:"total_tokens"`
	LastError        string         `json:"last_error,omitempty"`

	// successTimestamps and failureTimestamps track timestamps of successes/failures
	// within the sliding 5-minute window used for recovery decisions.
	successTimestamps []time.Time `json:"-"`
	failureTimestamps []time.Time `json:"-"`
}

// ProviderEntry represents a configured provider with its health state.
type ProviderEntry struct {
	Config   *ModelConfig
	Chatter  Chatter
	Health   *ProviderHealth
	Priority int // Lower = higher priority (0 = primary)
}

// ProviderManagerConfig holds configuration for the provider manager.
type ProviderManagerConfig struct {
	// Providers in priority order (first = primary)
	Providers []*ModelConfig

	// HealthCheckInterval is how often to check provider health (default: 5m)
	HealthCheckInterval time.Duration

	// FailureThreshold is how many consecutive failures before marking unhealthy (default: 3)
	FailureThreshold int

	// RecoveryThreshold is how many consecutive successes to recover from unhealthy (default: 2)
	RecoveryThreshold int

	// FailoverTimeout is the timeout for individual provider calls during failover (default: 30s)
	FailoverTimeout time.Duration

	// CostOptimized routes to cheapest available provider when true
	CostOptimized bool

	// Budget for tracking total usage
	Budget *Budget

	// Logger for operations
	Logger *slog.Logger

	// TokenResolver resolves OAuth access tokens for providers that use
	// device-code authentication. If nil, OAuth providers will fail at chat
	// time with a clear error.
	TokenResolver TokenResolver
}

// ProviderManager manages multiple LLM providers with failover and health tracking.
type ProviderManager struct {
	mu sync.RWMutex

	providers []*ProviderEntry
	config    ProviderManagerConfig
	logger    *slog.Logger

	// TokenResolver resolves OAuth access tokens for providers that use
	// device-code authentication.
	tokenResolver TokenResolver

	// Circuit breaker state
	lastHealthCheck time.Time
	stopChan        chan struct{}
	initialized     bool
}

// isAnthropic checks whether the given ModelConfig points to an Anthropic endpoint.
func isAnthropic(cfg *ModelConfig) bool {
	if cfg.ProviderID == ProviderIDAnthropic {
		return true
	}
	return strings.Contains(strings.ToLower(cfg.BaseURL), ProviderIDAnthropic)
}

// createChatterFor creates a Chatter for a ModelConfig, selecting the right
// client implementation (Anthropic vs OpenAI-compatible) based on provider ID
// and base URL.
func createChatterFor(cfg *ModelConfig, budget *Budget, logger *slog.Logger, tr TokenResolver) Chatter {
	if isAnthropic(cfg) {
		opts := []AnthropicClientOption{
			WithAnthropicLogger(logger),
		}
		if budget != nil {
			opts = append(opts, WithAnthropicBudget(budget))
		}
		if cfg.Timeout > 0 {
			opts = append(opts, WithAnthropicTimeout(cfg.Timeout))
		}
		return NewAnthropicClient(cfg, opts...)
	}

	opts := []ClientOption{
		WithLogger(logger),
	}
	if budget != nil {
		opts = append(opts, WithBudget(budget))
	}
	if tr != nil && cfg.OAuthProvider != "" {
		opts = append(opts, WithTokenResolver(tr, cfg.OAuthProvider))
	}
	if len(cfg.ExtraHeaders) > 0 {
		opts = append(opts, WithExtraHeaders(cfg.ExtraHeaders))
	}
	if cfg.Timeout > 0 {
		opts = append(opts, WithTimeout(cfg.Timeout))
	}
	return NewClient(cfg, opts...)
}

// NewProviderManager creates a new provider manager.
func NewProviderManager(cfg ProviderManagerConfig) *ProviderManager {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.HealthCheckInterval == 0 {
		cfg.HealthCheckInterval = 5 * time.Minute
	}
	if cfg.FailureThreshold == 0 {
		cfg.FailureThreshold = 3
	}
	if cfg.RecoveryThreshold == 0 {
		cfg.RecoveryThreshold = 2
	}
	// FailoverTimeout of 0 means no timeout (use provider's HTTP client timeout only)

	pm := &ProviderManager{
		config:        cfg,
		logger:        cfg.Logger,
		stopChan:      make(chan struct{}),
		tokenResolver: cfg.TokenResolver,
	}

	// Initialize providers
	for i, providerCfg := range cfg.Providers {
		entry := &ProviderEntry{
			Config:   providerCfg,
			Chatter:  createChatterFor(providerCfg, cfg.Budget, cfg.Logger, cfg.TokenResolver),
			Priority: i,
			Health: &ProviderHealth{
				ProviderID: providerCfg.ProviderID,
				Status:     ProviderStatusHealthy,
			},
		}
		pm.providers = append(pm.providers, entry)
	}

	pm.initialized = true
	return pm
}

// Chat sends a chat completion request with automatic failover.
func (pm *ProviderManager) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	pm.mu.RLock()
	if !pm.initialized || len(pm.providers) == 0 {
		pm.mu.RUnlock()
		return nil, errors.New("provider manager not initialized or no providers configured")
	}

	// Get providers in order of preference and snapshot health status while locked
	orderedProviders := pm.getOrderedProviders()
	healthSnapshot := make([]ProviderStatus, len(orderedProviders))
	for i, e := range orderedProviders {
		healthSnapshot[i] = e.Health.Status
	}
	pm.mu.RUnlock()

	var lastErr error
	var attempts int

	for i, entry := range orderedProviders {
		if healthSnapshot[i] == ProviderStatusDisabled {
			continue
		}

		// Skip unhealthy providers unless it's the only option
		if healthSnapshot[i] == ProviderStatusUnhealthy && len(orderedProviders) > 1 {
			if attempts == 0 {
				// Only skip if we haven't tried anything yet
				pm.logger.Debug("Skipping unhealthy provider",
					"provider", entry.Config.ProviderID,
				)
				continue
			}
		}

		attempts++
		start := time.Now()

		// Create timeout context for this attempt (if FailoverTimeout is set)
		var attemptCtx context.Context
		var cancel context.CancelFunc
		if pm.config.FailoverTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, pm.config.FailoverTimeout)
		} else {
			attemptCtx, cancel = context.WithCancel(ctx)
		}
		resp, err := entry.Chatter.Chat(attemptCtx, messages, opts...)
		cancel()

		latency := time.Since(start)

		if err != nil {
			lastErr = err

			// Check if context was cancelled (user cancelled, not timeout)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}

			// Error-type-aware failover: differentiate error types for
			// smarter routing decisions.
			switch {
			case IsRateLimitError(err):
				// Rate limit is provider-specific — rotate immediately.
				// Don't record as a health failure since rate limits are transient.
				pm.logger.Warn("Rate limit hit, rotating to next provider",
					"provider", entry.Config.ProviderID,
					"error", err,
					"latency", latency,
				)
				continue

			case isAuthError(err):
				// Auth errors mean the provider is misconfigured or the key is
				// bad — mark unhealthy so we skip it on future attempts.
				pm.recordFailure(entry, err, latency)
				pm.logger.Warn("Auth error, marking provider unhealthy and rotating",
					"provider", entry.Config.ProviderID,
					"error", err,
					"latency", latency,
				)
				pm.mu.Lock()
				entry.Health.Status = ProviderStatusUnhealthy
				entry.Health.LastError = err.Error()
				pm.mu.Unlock()
				continue

			case isClientError(err):
				// Client errors (400) are request-level, not provider-specific.
				// Don't rotate — the same error will happen on any provider.
				pm.logger.Warn("Client error, not rotating (request-level issue)",
					"provider", entry.Config.ProviderID,
					"error", err,
					"latency", latency,
				)
				return nil, err

			default:
				// Server errors (5xx), network errors, etc. — rotate normally.
				pm.recordFailure(entry, err, latency)
				pm.logger.Warn("Provider call failed, trying next",
					"provider", entry.Config.ProviderID,
					"error", err,
					"latency", latency,
				)
				continue
			}
		}

		// Success
		pm.recordSuccess(entry, resp, latency)

		// S3-8 FIX: guard resp dereference in debug log.
		if resp != nil {
			pm.logger.Debug("Provider call succeeded",
				"provider", entry.Config.ProviderID,
				"latency", latency,
				"tokens", resp.Usage.TotalTokens,
			)
		} else {
			pm.logger.Debug("Provider call succeeded",
				"provider", entry.Config.ProviderID,
				"latency", latency,
			)
		}

		return resp, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all providers failed: %w", lastErr)
	}

	return nil, errors.New("no available providers")
}

// ChatWithProgress sends a chat completion request with progress reporting.
// Attempts each provider in order, calling progress callback on attempt starts and failures.
func (pm *ProviderManager) ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	pm.mu.RLock()
	if !pm.initialized || len(pm.providers) == 0 {
		pm.mu.RUnlock()
		return nil, errors.New("provider manager not initialized or no providers configured")
	}

	orderedProviders := pm.getOrderedProviders()
	healthSnapshot := make([]ProviderStatus, len(orderedProviders))
	for i, e := range orderedProviders {
		healthSnapshot[i] = e.Health.Status
	}
	pm.mu.RUnlock()

	var lastErr error
	var attempts int

	for i, entry := range orderedProviders {
		if healthSnapshot[i] == ProviderStatusDisabled {
			continue
		}
		if healthSnapshot[i] == ProviderStatusUnhealthy && len(orderedProviders) > 1 {
			if attempts == 0 {
				if progress != nil {
					progress(ProgressStageThinking, fmt.Sprintf("Skipping unhealthy provider %s", entry.Config.ProviderID))
				}
				continue
			}
		}

		attempts++
		if progress != nil {
			progress(ProgressStageStarting, fmt.Sprintf("Attempting provider %s (attempt %d)", entry.Config.ProviderID, attempts))
		}

		start := time.Now()

		// Create timeout context for this attempt, mirroring Chat() so a
		// stalled primary yields to failover under progress-based calls too.
		// Without this, a hung provider blocks all progress-based callers
		// indefinitely.
		var attemptCtx context.Context
		var cancel context.CancelFunc
		if pm.config.FailoverTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, pm.config.FailoverTimeout)
		} else {
			attemptCtx, cancel = context.WithCancel(ctx)
		}
		resp, err := entry.Chatter.Chat(attemptCtx, messages, opts...)
		cancel()

		latency := time.Since(start)

		if err != nil {
			lastErr = err

			if ctx.Err() != nil {
				return nil, ctx.Err()
			}

			switch {
			case IsRateLimitError(err):
				if progress != nil {
					progress(ProgressStageDone, fmt.Sprintf("Provider %s rate limited, rotating (%s)", entry.Config.ProviderID, latency.Round(time.Millisecond)))
				}
				continue

			case isAuthError(err):
				pm.recordFailure(entry, err, latency)
				pm.mu.Lock()
				entry.Health.Status = ProviderStatusUnhealthy
				entry.Health.LastError = err.Error()
				pm.mu.Unlock()
				if progress != nil {
					progress(ProgressStageDone, fmt.Sprintf("Provider %s auth error, marked unhealthy (%s)", entry.Config.ProviderID, latency.Round(time.Millisecond)))
				}
				continue

			case isClientError(err):
				if progress != nil {
					progress(ProgressStageDone, fmt.Sprintf("Provider %s client error, not rotating (%s)", entry.Config.ProviderID, latency.Round(time.Millisecond)))
				}
				return nil, err

			default:
				pm.recordFailure(entry, err, latency)
				if progress != nil {
					progress(ProgressStageDone, fmt.Sprintf("Provider %s failed: %v (%s)", entry.Config.ProviderID, err, latency.Round(time.Millisecond)))
				}
				continue
			}
		}

		if progress != nil {
			progress(ProgressStageStreaming, fmt.Sprintf("Provider %s responded (%s)", entry.Config.ProviderID, latency.Round(time.Millisecond)))
		}
		pm.recordSuccess(entry, resp, latency)
		return resp, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all %d provider(s) failed (last: %w)", len(orderedProviders), lastErr)
	}
	return nil, fmt.Errorf("no providers available")
}

// getOrderedProviders returns providers sorted by preference.
func (pm *ProviderManager) getOrderedProviders() []*ProviderEntry {
	// Copy the slice for sorting
	ordered := make([]*ProviderEntry, len(pm.providers))
	copy(ordered, pm.providers)

	if pm.config.CostOptimized {
		// Sort by cost (cheapest first), then by priority
		sort.Slice(ordered, func(i, j int) bool {
			costI := ordered[i].Config.TotalCost()
			costJ := ordered[j].Config.TotalCost()

			// If costs are close (within 10%), prefer healthier provider
			if costI > 0 && costJ > 0 {
				ratio := costI / costJ
				if ratio > 0.9 && ratio < 1.1 {
					// Similar cost, prefer healthier
					if ordered[i].Health.Status != ordered[j].Health.Status {
						return ordered[i].Health.Status == ProviderStatusHealthy
					}
					return ordered[i].Priority < ordered[j].Priority
				}
			}

			return costI < costJ
		})
	} else {
		// Sort by health status, then by priority
		sort.Slice(ordered, func(i, j int) bool {
			// Healthy before degraded before unhealthy
			statusOrder := map[ProviderStatus]int{
				ProviderStatusHealthy:   0,
				ProviderStatusDegraded:  1,
				ProviderStatusUnhealthy: 2,
				ProviderStatusDisabled:  3,
			}

			si := statusOrder[ordered[i].Health.Status]
			sj := statusOrder[ordered[j].Health.Status]

			if si != sj {
				return si < sj
			}
			return ordered[i].Priority < ordered[j].Priority
		})
	}

	return ordered
}

// recordSuccess updates health metrics after a successful call.
func (pm *ProviderManager) recordSuccess(entry *ProviderEntry, resp *Response, latency time.Duration) {
	// S3-8 FIX: guard against nil resp (callers may pass nil on edge cases).
	if resp == nil {
		return
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	h := entry.Health
	h.SuccessCount++
	h.LastSuccess = time.Now()
	h.ConsecutiveFails = 0

	// Update average latency (exponential moving average)
	latencyMs := float64(latency.Milliseconds())
	if h.AvgLatencyMs == 0 {
		h.AvgLatencyMs = latencyMs
	} else {
		h.AvgLatencyMs = h.AvgLatencyMs*0.9 + latencyMs*0.1
	}

	// Track usage
	h.TotalTokens += int64(resp.Usage.TotalTokens)
	cost := float64(resp.Usage.PromptTokens) * entry.Config.CostPerMillionInput / 1_000_000
	cost += float64(resp.Usage.CompletionTokens) * entry.Config.CostPerMillionOutput / 1_000_000
	h.TotalCost += cost

	// Track dollar cost in budget for enforcement
	if pm.config.Budget != nil {
		pm.config.Budget.RecordCost(CostRecord{
			Timestamp:        time.Now(),
			CostUSD:          cost,
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
		})
	}

	// Track recent success timestamp (for sliding window recovery)
	h.appendSuccessTimestamp()
	h.pruneOldEntries()

	// Update status
	if h.Status == ProviderStatusUnhealthy || h.Status == ProviderStatusDegraded {
		// Check if we should recover using sliding window rates
		rSuc := h.recentSuccessCount()
		rFail := h.recentFailureCount()
		totalRecent := rSuc + rFail
		if totalRecent > 0 {
			recentRate := float64(rSuc) / float64(totalRecent)
			if recentRate > 0.8 {
				h.Status = ProviderStatusHealthy
				h.LastError = ""
				pm.logger.Info("Provider recovered",
					"provider", entry.Config.ProviderID,
					"recent_successes", rSuc,
					"recent_failures", rFail,
					"success_rate", recentRate,
				)
			} else if h.Status == ProviderStatusUnhealthy && h.ConsecutiveFails == 0 {
				h.Status = ProviderStatusDegraded
			}
		}
	}
}

// recordFailure updates health metrics after a failed call.
func (pm *ProviderManager) recordFailure(entry *ProviderEntry, err error, latency time.Duration) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	h := entry.Health
	h.FailureCount++
	h.LastFailure = time.Now()
	h.ConsecutiveFails++
	h.LastError = err.Error()

	// Track recent failure timestamp (for sliding window recovery)
	h.appendFailureTimestamp()
	h.pruneOldEntries()

	// Update average latency
	latencyMs := float64(latency.Milliseconds())
	if h.AvgLatencyMs == 0 {
		h.AvgLatencyMs = latencyMs
	} else {
		h.AvgLatencyMs = h.AvgLatencyMs*0.9 + latencyMs*0.1
	}

	// Update status based on consecutive failures
	if h.ConsecutiveFails >= pm.config.FailureThreshold {
		if h.Status != ProviderStatusUnhealthy {
			h.Status = ProviderStatusUnhealthy
			pm.logger.Warn("Provider marked unhealthy",
				"provider", entry.Config.ProviderID,
				"consecutive_fails", h.ConsecutiveFails,
				"last_error", h.LastError,
			)
		}
	} else if h.ConsecutiveFails > 0 && h.Status == ProviderStatusHealthy {
		h.Status = ProviderStatusDegraded
	}
}

// recentSuccessCount returns the count of successes within the sliding 5-minute window.
func (h *ProviderHealth) recentSuccessCount() int {
	now := time.Now()
	count := 0
	for _, t := range h.successTimestamps {
		if now.Sub(t) <= 5*time.Minute {
			count++
		}
	}
	return count
}

// recentFailureCount returns the count of failures within the sliding 5-minute window.
func (h *ProviderHealth) recentFailureCount() int {
	now := time.Now()
	count := 0
	for _, t := range h.failureTimestamps {
		if now.Sub(t) <= 5*time.Minute {
			count++
		}
	}
	return count
}

// pruneOldEntries removes entries older than 5 minutes from the recent tracking slices.
// Must be called while holding the ProviderManager's lock.
func (h *ProviderHealth) pruneOldEntries() {
	now := time.Now()
	cutoff := now.Add(-5 * time.Minute)

	suc := h.successTimestamps[:0]
	for _, t := range h.successTimestamps {
		if t.After(cutoff) {
			suc = append(suc, t)
		}
	}
	h.successTimestamps = suc

	fail := h.failureTimestamps[:0]
	for _, t := range h.failureTimestamps {
		if t.After(cutoff) {
			fail = append(fail, t)
		}
	}
	h.failureTimestamps = fail
}

func (h *ProviderHealth) appendSuccessTimestamp() {
	h.successTimestamps = append(h.successTimestamps, time.Now())
}

func (h *ProviderHealth) appendFailureTimestamp() {
	h.failureTimestamps = append(h.failureTimestamps, time.Now())
}

// GetProviderHealth returns health information for all providers.
func (pm *ProviderManager) GetProviderHealth() []*ProviderHealth {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	health := make([]*ProviderHealth, len(pm.providers))
	for i, p := range pm.providers {
		// Return a copy
		h := *p.Health
		health[i] = &h
	}
	return health
}

// GetProviderStatus returns summary status.
func (pm *ProviderManager) GetProviderStatus() map[string]any {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	healthyCount := 0
	degradedCount := 0
	unhealthyCount := 0
	totalCost := 0.0
	totalTokens := int64(0)

	for _, p := range pm.providers {
		switch p.Health.Status {
		case ProviderStatusHealthy:
			healthyCount++
		case ProviderStatusDegraded:
			degradedCount++
		case ProviderStatusUnhealthy, ProviderStatusDisabled:
			unhealthyCount++
		}
		totalCost += p.Health.TotalCost
		totalTokens += p.Health.TotalTokens
	}

	return map[string]any{
		"total_providers":   len(pm.providers),
		"healthy_count":     healthyCount,
		"degraded_count":    degradedCount,
		"unhealthy_count":   unhealthyCount,
		"total_cost":        totalCost,
		"total_tokens":      totalTokens,
		"cost_optimized":    pm.config.CostOptimized,
		"last_health_check": pm.lastHealthCheck,
	}
}

// EnableProvider enables a disabled provider.
func (pm *ProviderManager) EnableProvider(providerID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, p := range pm.providers {
		if p.Config.ProviderID == providerID {
			if p.Health.Status == ProviderStatusDisabled {
				p.Health.Status = ProviderStatusDegraded
				p.Health.ConsecutiveFails = 0
				pm.logger.Info("Provider enabled", "provider", providerID)
			}
			return nil
		}
	}

	return fmt.Errorf("provider not found: %s", providerID)
}

// DisableProvider temporarily disables a provider.
func (pm *ProviderManager) DisableProvider(providerID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, p := range pm.providers {
		if p.Config.ProviderID == providerID {
			p.Health.Status = ProviderStatusDisabled
			pm.logger.Info("Provider disabled", "provider", providerID)
			return nil
		}
	}

	return fmt.Errorf("provider not found: %s", providerID)
}

// SetCostOptimized enables or disables cost-optimized routing.
func (pm *ProviderManager) SetCostOptimized(enabled bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.config.CostOptimized = enabled
}

// AddProvider adds a new provider dynamically.
func (pm *ProviderManager) AddProvider(cfg *ModelConfig, priority int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	entry := &ProviderEntry{
		Config:   cfg,
		Chatter:  createChatterFor(cfg, pm.config.Budget, pm.logger, pm.tokenResolver),
		Priority: priority,
		Health: &ProviderHealth{
			ProviderID: cfg.ProviderID,
			Status:     ProviderStatusHealthy,
		},
	}

	pm.providers = append(pm.providers, entry)
	pm.logger.Info("Provider added", "provider", cfg.ProviderID, "priority", priority)
}

// RemoveProvider removes a provider.
func (pm *ProviderManager) RemoveProvider(providerID string) error {
	// Find and remove the provider under lock, then close the client
	// outside the lock to avoid blocking other callers during HTTP
	// connection teardown (CLAUDE.md mutex scope rule).
	pm.mu.Lock()

	var removed *ProviderEntry
	for i, p := range pm.providers {
		if p.Config.ProviderID == providerID {
			removed = p
			pm.providers = append(pm.providers[:i], pm.providers[i+1:]...)
			break
		}
	}
	pm.mu.Unlock()

	if removed == nil {
		return fmt.Errorf("provider not found: %s", providerID)
	}

	// Close the chatter if it implements io.Closer.
	// Concrete Chatter implementations (Client, AnthropicClient) have
	// signature Close() error, so the assertion must match that.
	if closer, ok := removed.Chatter.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			pm.logger.Warn("Error closing provider client", "provider", providerID, "error", err)
		}
	}

	pm.logger.Info("Provider removed", "provider", providerID)
	return nil
}

// GetPrimaryProvider returns the current primary provider.
func (pm *ProviderManager) GetPrimaryProvider() *ProviderEntry {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	providers := pm.getOrderedProviders()
	for _, p := range providers {
		if p.Health.Status != ProviderStatusDisabled && p.Health.Status != ProviderStatusUnhealthy {
			return p
		}
	}

	// Return first provider even if unhealthy
	if len(pm.providers) > 0 {
		return pm.providers[0]
	}

	return nil
}

// ResetProviderHealth resets health metrics for a provider.
func (pm *ProviderManager) ResetProviderHealth(providerID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, p := range pm.providers {
		if p.Config.ProviderID == providerID {
			p.Health = &ProviderHealth{
				ProviderID: providerID,
				Status:     ProviderStatusHealthy,
			}
			pm.logger.Info("Provider health reset", "provider", providerID)
			return nil
		}
	}

	return fmt.Errorf("provider not found: %s", providerID)
}

// StartHealthChecks starts periodic health checks (optional).
func (pm *ProviderManager) StartHealthChecks(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(pm.config.HealthCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-pm.stopChan:
				return
			case <-ticker.C:
				pm.runHealthCheck(ctx)
			}
		}
	}()
}

// runHealthCheck performs a health check on unhealthy providers by sending
// a minimal chat request ("test", 1 token) to elicit a response.
//
// LLM-13: Health checks make live API calls and consume real budget
// (each check counts against the provider's token quota and cost tracking).
// Operators should set HealthCheckInterval high enough (default 5m) to
// avoid significant cumulative cost, especially with providers that have
// per-request minimum charges. For zero-cost providers (local models) this
// is not a concern.
func (pm *ProviderManager) runHealthCheck(ctx context.Context) {
	pm.mu.Lock()
	pm.lastHealthCheck = time.Now()

	// Snapshot providers while holding the lock.
	snapshots := make([]*ProviderEntry, len(pm.providers))
	copy(snapshots, pm.providers)
	pm.mu.Unlock()

	// Health check: try a minimal request on unhealthy providers only
	for _, entry := range snapshots {
		pm.mu.RLock()
		status := entry.Health.Status
		pm.mu.RUnlock()
		if status != ProviderStatusUnhealthy {
			continue
		}
		// Try a minimal request
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)

		messages := []ChatMessage{
			{Role: RoleUser, Content: "test"},
		}

		start := time.Now()
		_, err := entry.Chatter.Chat(checkCtx, messages, WithMaxTokens(1))
		latency := time.Since(start)
		cancel()

		if err == nil {
			pm.logger.Info("Health check passed for unhealthy provider",
				"provider", entry.Config.ProviderID,
			)
			pm.mu.Lock()
			entry.Health.Status = ProviderStatusDegraded
			entry.Health.ConsecutiveFails = 0
			pm.mu.Unlock()
		}

		_ = latency // Could use for latency tracking
	}
}

// Stop stops the provider manager and closes all clients.
func (pm *ProviderManager) Stop() {
	// S3-5 FIX: guard against double-close of stopChan using select pattern.
	select {
	case <-pm.stopChan:
		return // already closed
	default:
		close(pm.stopChan)
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, p := range pm.providers {
		// Match Close() error signature (both Client and AnthropicClient).
		if closer, ok := p.Chatter.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil { //nolint:mutexio // one-time teardown guarded by stopChan select
				pm.logger.Warn("Error closing provider client", "error", err)
			}
		}
	}

	pm.initialized = false
}

// Config returns the model configuration of the primary provider.
func (pm *ProviderManager) Config() *ModelConfig {
	pp := pm.GetPrimaryProvider()
	if pp != nil {
		return pp.Config
	}
	return nil
}

// ProviderCount returns the number of configured providers.
func (pm *ProviderManager) ProviderCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.providers)
}

// HealthyProviderCount returns the number of healthy providers.
func (pm *ProviderManager) HealthyProviderCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	count := 0
	for _, p := range pm.providers {
		if p.Health.Status == ProviderStatusHealthy {
			count++
		}
	}
	return count
}

// isAuthError returns true if err is (or wraps) an APIError with a 401 or 403 status.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden
	}
	return false
}

// isClientError returns true if err is (or wraps) an APIError with a 400-level status
// that is not 401, 403, or 429 (those are handled separately).
func isClientError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		status := apiErr.StatusCode
		return status >= 400 && status < 500 &&
			status != http.StatusUnauthorized &&
			status != http.StatusForbidden &&
			status != http.StatusTooManyRequests
	}
	return false
}
