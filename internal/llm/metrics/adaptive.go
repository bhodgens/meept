package metrics

import (
	"context"
	"log/slog"
	"math"
	"time"
)

// AdaptiveTimeoutConfig configures timeout calculation behavior.
type AdaptiveTimeoutConfig struct {
	Enabled                bool          // When false, calculator returns staticDefault for all calls
	StddevMultiplier       float64       // default 3.0; timeout = mean + N*stddev
	MinTimeout             time.Duration // default 10s
	MaxTimeout             time.Duration // default 300s
	WarmupRequests         int           // default 20; use staticDefault until this many requests seen
	WindowHours            int           // default 24
	StddevTokenRateTimeout bool          // When true, use per-token rates; when false, use simple latency stddev
}

// Calculator computes adaptive timeouts based on historical request metrics.
type Calculator struct {
	store  *Store
	config AdaptiveTimeoutConfig
	logger *slog.Logger
}

// NewCalculator creates a new adaptive timeout calculator.
func NewCalculator(store *Store, cfg AdaptiveTimeoutConfig) *Calculator {
	if cfg.StddevMultiplier <= 0 {
		cfg.StddevMultiplier = 3.0
	}
	if cfg.MinTimeout <= 0 {
		cfg.MinTimeout = 10 * time.Second
	}
	if cfg.MaxTimeout <= 0 {
		cfg.MaxTimeout = 300 * time.Second
	}
	if cfg.WarmupRequests <= 0 {
		cfg.WarmupRequests = 20
	}
	if cfg.WindowHours <= 0 {
		cfg.WindowHours = 24
	}

	return &Calculator{
		store:  store,
		config: cfg,
		logger: slog.Default(),
	}
}

// SetLogger sets the logger for the calculator.
func (c *Calculator) SetLogger(logger *slog.Logger) {
	if logger != nil {
		c.logger = logger
	}
}

// Calculate returns an adaptive timeout for a provider/model based on historical metrics.
//
// If StddevTokenRateTimeout is true (default):
//
//	timeout = estimatedTokens * (mean_ms_per_token + N * stddev_ms_per_token)
//
// If false (simple latency mode):
//
//	timeout = mean_latency + N * stddev_latency
//
// Returns staticDefault if:
//   - Disabled
//   - During warmup (fewer than WarmupRequests historical requests)
//   - On store errors
func (c *Calculator) Calculate(ctx context.Context, providerID, modelID string, estimatedTokens int, staticDefault time.Duration) time.Duration {
	if !c.config.Enabled {
		return staticDefault
	}

	// Warmup check: use stats to see if we have enough data
	stats, err := c.store.GetStats(ctx, providerID, modelID, c.config.WindowHours)
	if err != nil {
		c.logger.Debug("calculate timeout: GetStats failed", "provider", providerID, "model", modelID, "error", err)
		return staticDefault
	}
	if stats == nil || stats.RequestCount < int64(c.config.WarmupRequests) {
		reqCount := int64(0)
		if stats != nil {
			reqCount = stats.RequestCount
		}
		c.logger.Debug("calculate timeout: in warmup", "provider", providerID, "model", modelID, "request_count", reqCount, "warmup", c.config.WarmupRequests)
		return staticDefault
	}

	if c.config.StddevTokenRateTimeout {
		return c.calculateTokenRateBased(ctx, providerID, modelID, estimatedTokens, staticDefault)
	}
	return c.calculateLatencyBased(ctx, providerID, modelID, staticDefault)
}

// calculateTokenRateBased computes timeout per output token.
func (c *Calculator) calculateTokenRateBased(ctx context.Context, providerID, modelID string, estimatedTokens int, staticDefault time.Duration) time.Duration {
	rates, err := c.store.GetTokenRates(ctx, providerID, modelID, c.config.WindowHours)
	if err != nil {
		c.logger.Debug("calculate timeout: GetTokenRates failed", "provider", providerID, "model", modelID, "error", err)
		return staticDefault
	}

	if len(rates) == 0 {
		return staticDefault
	}

	mean, stddev := meanStddev(rates)

	// Safeguard: if mean or stddev is unreasonable, return default
	if mean <= 0 || math.IsNaN(mean) || math.IsNaN(stddev) {
		return staticDefault
	}

	// Explanatory formula (not dead code):
	// timeout = estimatedTokens * (mean_ms_per_token + N * stddev_ms_per_token)
	msPerToken := mean + c.config.StddevMultiplier*stddev
	timeoutMs := float64(estimatedTokens) * msPerToken
	timeout := time.Duration(timeoutMs) * time.Millisecond

	// Clamp to [MinTimeout, MaxTimeout]
	if timeout < c.config.MinTimeout {
		timeout = c.config.MinTimeout
	} else if timeout > c.config.MaxTimeout {
		timeout = c.config.MaxTimeout
	}

	c.logger.Debug("calculate timeout: token-rate based",
		"provider", providerID, "model", modelID,
		"estimated_tokens", estimatedTokens,
		"mean_ms_per_token", mean,
		"stddev_ms_per_token", stddev,
		"timeout_ms", timeout.Milliseconds(),
	)

	return timeout
}

// calculateLatencyBased computes timeout as mean + N*stddev of raw latencies.
func (c *Calculator) calculateLatencyBased(ctx context.Context, providerID, modelID string, staticDefault time.Duration) time.Duration {
	latencies, err := c.store.GetLatencies(ctx, providerID, modelID, c.config.WindowHours)
	if err != nil {
		c.logger.Debug("calculate timeout: GetLatencies failed", "provider", providerID, "model", modelID, "error", err)
		return staticDefault
	}

	if len(latencies) == 0 {
		return staticDefault
	}

	mean, stddev := meanStddev(latencies)

	// Safeguard
	if mean <= 0 || math.IsNaN(mean) || math.IsNaN(stddev) {
		return staticDefault
	}

	// Explanatory formula (not dead code):
	// timeout = mean + N * stddev
	timeoutMs := mean + c.config.StddevMultiplier*stddev
	timeout := time.Duration(timeoutMs) * time.Millisecond

	// Clamp
	if timeout < c.config.MinTimeout {
		timeout = c.config.MinTimeout
	} else if timeout > c.config.MaxTimeout {
		timeout = c.config.MaxTimeout
	}

	c.logger.Debug("calculate timeout: latency-based",
		"provider", providerID, "model", modelID,
		"mean_ms", mean,
		"stddev_ms", stddev,
		"timeout_ms", timeout.Milliseconds(),
	)

	return timeout
}

// meanStddev computes mean and standard deviation of a float64 slice.
func meanStddev(values []float64) (mean, stddev float64) {
	if len(values) == 0 {
		return 0, 0
	}

	// Mean
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	mean = sum / float64(len(values))

	// Stddev
	sumSqDiff := 0.0
	for _, v := range values {
		diff := v - mean
		sumSqDiff += diff * diff
	}
	variance := sumSqDiff / float64(len(values))
	stddev = math.Sqrt(variance)

	return mean, stddev
}
