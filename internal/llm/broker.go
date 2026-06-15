package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm/metrics"
)

// ProviderStatus type is defined in provider_manager.go (reuse it)

// BrokerConfig configures a ModelBroker.
type BrokerConfig struct {
	ProvidersConfig *ProvidersConfig
	MaxErrorRate    float64 // default 0.10
	MaxP95LatencyMS float64 // default 30000
	FallbackEnabled bool    // default true
	MetricsStore    *metrics.Store
	TimeoutCalc     *metrics.Calculator
	Budget          *Budget
	TokenCache      ResponseCache
	TokenResolver   TokenResolver
	Logger          *slog.Logger
}

// brokerEntry holds a Chatter and its health state.
type brokerEntry struct {
	model               *ModelConfig
	chatter             Chatter
	status              ProviderStatus
	lastStatusCheckTime time.Time
}

// ModelBroker manages multiple LLM providers and routes requests with health awareness.
type ModelBroker struct {
	mu        sync.RWMutex
	entries   map[string]*brokerEntry // keyed by "provider/model-id"
	entryKeys []string                // maintains deterministic iteration order
	config    BrokerConfig
	logger    *slog.Logger
	fallback  Chatter // fallback model if primary fails
}

// BrokerStatus provides a snapshot of broker health.
type BrokerStatus struct {
	Providers []ProviderStatusEntry
}

// ProviderStatusEntry describes the status of a single provider/model.
type ProviderStatusEntry struct {
	ProviderID     string
	ModelID        string
	Status         ProviderStatus
	ErrorRate      float64
	P95LatencyMs   float64
	CurrentTimeout time.Duration
	TotalRequests  int64
}

// NewModelBroker creates a new model broker.
func NewModelBroker(cfg BrokerConfig) *ModelBroker {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	b := &ModelBroker{
		entries: make(map[string]*brokerEntry),
		config:  cfg,
		logger:  cfg.Logger,
	}

	// Initialize all models from config
	if cfg.ProvidersConfig != nil {
		allModels := GetAllModels(cfg.ProvidersConfig)
		for _, modelCfg := range allModels {
			chatter := b.newChatterFor(modelCfg)
			key := fmt.Sprintf("%s/%s", modelCfg.ProviderID, modelCfg.ModelID)
			b.entries[key] = &brokerEntry{
				model:   modelCfg,
				chatter: chatter,
				status:  ProviderStatusHealthy,
			}
			b.entryKeys = append(b.entryKeys, key) // maintain insertion order
		}

		// Set fallback to SmallModel if configured
		if cfg.ProvidersConfig.SmallModel != "" {
			fallbackModel := ResolveModelRef(cfg.ProvidersConfig.SmallModel, cfg.ProvidersConfig)
			if fallbackModel != nil {
				b.fallback = b.newChatterFor(fallbackModel)
			}
		}
	}

	b.logger.Debug("model broker initialized", "models", len(b.entries))
	return b
}

// newChatterFor creates a Chatter for a ModelConfig.
// Detects Anthropic vs OpenAI-compat and injects metrics/timeout options.
func (b *ModelBroker) newChatterFor(cfg *ModelConfig) Chatter {
	// Detect Anthropic
	if cfg.ProviderID == ProviderIDAnthropic || strings.Contains(strings.ToLower(cfg.BaseURL), ProviderIDAnthropic) {
		opts := []AnthropicClientOption{
			WithAnthropicLogger(b.logger),
		}
		if b.config.Budget != nil {
			opts = append(opts, WithAnthropicBudget(b.config.Budget))
		}
		if b.config.MetricsStore != nil {
			opts = append(opts, WithAnthropicMetricsStore(b.config.MetricsStore))
		}
		if b.config.TimeoutCalc != nil {
			opts = append(opts, WithAnthropicTimeoutCalculator(b.config.TimeoutCalc))
		}
		if b.config.TokenCache != nil {
			opts = append(opts, WithAnthropicTokenCache(b.config.TokenCache))
		}
		if cfg.Timeout > 0 {
			opts = append(opts, WithAnthropicTimeout(cfg.Timeout))
		}
		return NewAnthropicClient(cfg, opts...)
	}

	// OpenAI-compat
	opts := []ClientOption{
		WithLogger(b.logger),
	}
	if b.config.Budget != nil {
		opts = append(opts, WithBudget(b.config.Budget))
	}
	if b.config.MetricsStore != nil {
		opts = append(opts, WithMetricsStore(b.config.MetricsStore))
	}
	if b.config.TimeoutCalc != nil {
		opts = append(opts, WithTimeoutCalculator(b.config.TimeoutCalc))
	}
	if b.config.TokenCache != nil {
		opts = append(opts, WithTokenCache(b.config.TokenCache))
	}
	if b.config.TokenResolver != nil {
		opts = append(opts, WithTokenResolver(b.config.TokenResolver, cfg.OAuthProvider))
	}
	if len(cfg.ExtraHeaders) > 0 {
		opts = append(opts, WithExtraHeaders(cfg.ExtraHeaders))
	}
	if cfg.Timeout > 0 {
		opts = append(opts, WithTimeout(cfg.Timeout))
	}
	return NewClient(cfg, opts...)
}

// Chat sends a request to the broker, which routes to a healthy provider.
// D2 FIX: On runtime failure (5xx/rate-limit), iterates through remaining
// healthy providers before falling back or failing.
func (b *ModelBroker) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	b.mu.RLock()
	// Collect all healthy providers
	var healthyEntries []*brokerEntry
	for _, key := range b.entryKeys {
		entry := b.entries[key]
		if entry.status == ProviderStatusHealthy {
			healthyEntries = append(healthyEntries, entry)
		}
	}
	b.mu.RUnlock()

	// D2: Try each healthy provider in order until one succeeds
	var lastErr error
	for i, entry := range healthyEntries {
		resp, err := entry.chatter.Chat(ctx, messages, opts...)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		// D2: Only retry on transient errors (5xx, rate limits)
		if !isRetryableError(err) {
			b.logger.Warn("non-retryable error from provider",
				"provider", fmt.Sprintf("%s/%s", entry.model.ProviderID, entry.model.ModelID),
				"error", err)
			return nil, err
		}

		// Log retry and continue to next provider
		b.logger.Debug("retryable error from provider, trying next",
			"provider", fmt.Sprintf("%s/%s", entry.model.ProviderID, entry.model.ModelID),
			"attempt", i+1,
			"total_healthy", len(healthyEntries),
			"error", err)
	}

	// All healthy providers failed, try fallback if enabled
	if len(healthyEntries) > 0 && b.config.FallbackEnabled && b.fallback != nil {
		b.logger.Warn("all healthy providers failed, using fallback",
			"last_error", lastErr)
		return b.fallback.Chat(ctx, messages, opts...)
	}

	if len(healthyEntries) == 0 {
		return nil, errors.New("no healthy providers available")
	}

	return nil, fmt.Errorf("all %d healthy providers failed: %w", len(healthyEntries), lastErr)
}

// ChatWithProgress sends a request with progress reporting.
// D2 FIX: On runtime failure (5xx/rate-limit), iterates through remaining
// healthy providers before falling back or failing.
func (b *ModelBroker) ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	b.mu.RLock()
	// Collect all healthy providers
	var healthyEntries []*brokerEntry
	for _, key := range b.entryKeys {
		entry := b.entries[key]
		if entry.status == ProviderStatusHealthy {
			healthyEntries = append(healthyEntries, entry)
		}
	}
	b.mu.RUnlock()

	// D2: Try each healthy provider in order until one succeeds
	var lastErr error
	for i, entry := range healthyEntries {
		resp, err := entry.chatter.ChatWithProgress(ctx, messages, progress, opts...)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		// D2: Only retry on transient errors (5xx, rate limits)
		if !isRetryableError(err) {
			b.logger.Warn("non-retryable error from provider",
				"provider", fmt.Sprintf("%s/%s", entry.model.ProviderID, entry.model.ModelID),
				"error", err)
			return nil, err
		}

		// Log retry and continue to next provider
		b.logger.Debug("retryable error from provider, trying next",
			"provider", fmt.Sprintf("%s/%s", entry.model.ProviderID, entry.model.ModelID),
			"attempt", i+1,
			"total_healthy", len(healthyEntries),
			"error", err)
	}

	// All healthy providers failed, try fallback if enabled
	if len(healthyEntries) > 0 && b.config.FallbackEnabled && b.fallback != nil {
		b.logger.Warn("all healthy providers failed, using fallback",
			"last_error", lastErr)
		return b.fallback.ChatWithProgress(ctx, messages, progress, opts...)
	}

	if len(healthyEntries) == 0 {
		return nil, errors.New("no healthy providers available")
	}

	return nil, fmt.Errorf("all %d healthy providers failed: %w", len(healthyEntries), lastErr)
}

// ChatWithModel sends a request to a specific model, bypassing health checks.
// modelRef is in the format "provider/model-id".
func (b *ModelBroker) ChatWithModel(ctx context.Context, modelRef string, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	b.mu.RLock()
	entry, ok := b.entries[modelRef]
	b.mu.RUnlock()

	if !ok || entry == nil {
		return nil, fmt.Errorf("model not found: %s", modelRef)
	}

	return entry.chatter.Chat(ctx, messages, opts...)
}

// ChatWithModelProgress sends a request to a specific model with progress reporting.
func (b *ModelBroker) ChatWithModelProgress(ctx context.Context, modelRef string, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	b.mu.RLock()
	entry, ok := b.entries[modelRef]
	b.mu.RUnlock()

	if !ok || entry == nil {
		return nil, fmt.Errorf("model not found: %s", modelRef)
	}

	return entry.chatter.ChatWithProgress(ctx, messages, progress, opts...)
}

// UpdateHealth re-evaluates provider health based on metrics.
// Called periodically (e.g., every 30 seconds) by the daemon.
func (b *ModelBroker) UpdateHealth(ctx context.Context) error {
	if b.config.MetricsStore == nil {
		return nil // Metrics not enabled
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	allStats, err := b.config.MetricsStore.GetAllStats(ctx, 24)
	if err != nil {
		b.logger.Debug("UpdateHealth: GetAllStats failed", "error", err)
		return err
	}

	statsMap := make(map[string]*metrics.ProviderStats)
	for _, s := range allStats {
		key := fmt.Sprintf("%s/%s", s.ProviderID, s.ModelID)
		statsMap[key] = s
	}

	now := time.Now()
	for key, entry := range b.entries {
		stats, ok := statsMap[key]
		if !ok {
			// No metrics yet; assume healthy
			entry.status = ProviderStatusHealthy
			entry.lastStatusCheckTime = now
			continue
		}

		// Check thresholds
		if stats.ErrorRate > b.config.MaxErrorRate || stats.P95LatencyMs > b.config.MaxP95LatencyMS {
			if entry.status == ProviderStatusHealthy {
				entry.status = ProviderStatusDegraded
				b.logger.Warn("provider degraded",
					"provider", key,
					"error_rate", stats.ErrorRate,
					"p95_latency_ms", stats.P95LatencyMs,
				)
			}
		} else {
			if entry.status != ProviderStatusHealthy {
				entry.status = ProviderStatusHealthy
				b.logger.Info("provider recovered", "provider", key)
			}
		}

		entry.lastStatusCheckTime = now
	}

	return nil
}

// GetStatus returns a snapshot of broker health.
func (b *ModelBroker) GetStatus() BrokerStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var entries []ProviderStatusEntry
	for _, key := range b.entryKeys {
		be := b.entries[key]
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			continue
		}

		// Get current timeout estimate if available
		var timeout time.Duration
		if b.config.TimeoutCalc != nil {
			timeout = b.config.TimeoutCalc.Calculate(context.Background(), be.model.ProviderID, be.model.ModelID, 4096, 120*time.Second)
		}

		entry := ProviderStatusEntry{
			ProviderID:     parts[0],
			ModelID:        parts[1],
			Status:         be.status,
			CurrentTimeout: timeout,
		}

		// Populate metrics if available
		if b.config.MetricsStore != nil {
			stats, err := b.config.MetricsStore.GetStats(context.Background(), be.model.ProviderID, be.model.ModelID, 24)
			if err == nil && stats != nil {
				entry.ErrorRate = stats.ErrorRate
				entry.P95LatencyMs = stats.P95LatencyMs
				entry.TotalRequests = stats.RequestCount
			}
		}

		entries = append(entries, entry)
	}

	return BrokerStatus{
		Providers: entries,
	}
}

// Config returns the model configuration of the primary (first healthy) provider.
func (b *ModelBroker) Config() *ModelConfig {
	b.mu.RLock()
	defer b.mu.RUnlock()
	// Iterate in deterministic order to get consistent "first healthy" result
	for _, key := range b.entryKeys {
		entry := b.entries[key]
		if entry.status == ProviderStatusHealthy {
			return entry.chatter.Config()
		}
	}
	// Fall back to the first entry even if unhealthy
	if len(b.entryKeys) > 0 {
		return b.entries[b.entryKeys[0]].chatter.Config()
	}
	return &ModelConfig{}
}

// ChatterForModel returns a Chatter for a specific model reference.
// Returns nil if the model is not found in the broker.
// The returned Chatter can be used directly for chat operations.
func (b *ModelBroker) ChatterForModel(modelRef string) Chatter {
	b.mu.RLock()
	defer b.mu.RUnlock()

	entry, ok := b.entries[modelRef]
	if !ok || entry == nil {
		return nil
	}
	return entry.chatter
}

// Ensure ModelBroker implements Chatter
// isRetryableError returns true for transient errors that warrant retry.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	// Check for rate limit errors
	if IsRateLimitError(err) {
		return true
	}
	// Check for server errors (5xx) via structured APIError so HTTP 529
	// (Anthropic "Overloaded") and other 5xx codes are detected. The previous
	// substring match on "5" + "00"/"02"/"03"/"04" missed 529 and could false
	// positive on arbitrary error strings containing those digits.
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500 && apiErr.StatusCode < 600
	}
	return false
}

var _ Chatter = (*ModelBroker)(nil)
