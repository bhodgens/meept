package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"sync"
	"time"
)

// LivePrice holds a dynamically fetched price for a model.
type LivePrice struct {
	ProviderID string
	ModelID    string
	InputCost  float64 // USD per million tokens
	OutputCost float64 // USD per million tokens
	Source     string  // "openrouter", "together", or "catalog"
	FetchedAt  time.Time
}

// PricingSyncerConfig configures the pricing syncer.
type PricingSyncerConfig struct {
	OpenRouterURL string        // URL for OpenRouter models endpoint
	TogetherURL   string        // URL for Together AI models endpoint
	SyncInterval  time.Duration // How often to re-sync (default: 6h)
	HTTPTimeout   time.Duration // Per-request timeout (default: 30s)
	Logger        *slog.Logger
}

// PricingSyncer fetches live model pricing from provider APIs.
type PricingSyncer struct {
	config PricingSyncerConfig
	client *http.Client
	mu     sync.RWMutex
	prices map[string]*LivePrice // key: "provider/model-id"
	logger *slog.Logger
}

// NewPricingSyncer creates a new pricing syncer.
func NewPricingSyncer(cfg PricingSyncerConfig) *PricingSyncer {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = 30 * time.Second
	}
	if cfg.SyncInterval == 0 {
		cfg.SyncInterval = 6 * time.Hour
	}
	return &PricingSyncer{
		config: cfg,
		client: &http.Client{Timeout: cfg.HTTPTimeout},
		prices: make(map[string]*LivePrice),
		logger: cfg.Logger,
	}
}

// openrouterResponse models the OpenRouter /api/v1/models response.
type openrouterResponse struct {
	Data []struct {
		ID      string `json:"id"`
		Pricing struct {
			Prompt     string `json:"prompt"`
			Completion string `json:"completion"`
		} `json:"pricing"`
	} `json:"data"`
}

// FetchOpenRouter fetches pricing from OpenRouter's models endpoint.
func (ps *PricingSyncer) FetchOpenRouter(ctx context.Context) (map[string]*LivePrice, error) {
	if ps.config.OpenRouterURL == "" {
		return nil, fmt.Errorf("openrouter URL not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ps.config.OpenRouterURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := ps.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching openrouter pricing: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var orResp openrouterResponse
	if err := json.Unmarshal(body, &orResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	prices := make(map[string]*LivePrice, len(orResp.Data))
	for _, m := range orResp.Data {
		promptPrice := parseFloat(m.Pricing.Prompt)
		completionPrice := parseFloat(m.Pricing.Completion)

		if promptPrice <= 0 && completionPrice <= 0 {
			continue
		}

		prices[m.ID] = &LivePrice{
			ModelID:    m.ID,
			InputCost:  promptPrice * 1_000_000, // per-token -> per-million
			OutputCost: completionPrice * 1_000_000,
			Source:     "openrouter",
			FetchedAt:  time.Now(),
		}
	}

	ps.logger.Info("Fetched pricing from OpenRouter", "models", len(prices))
	return prices, nil
}

// MergeWithCatalog merges fetched prices with the hardcoded catalog,
// preferring fetched prices and filling gaps from the catalog.
func (ps *PricingSyncer) MergeWithCatalog(fetched map[string]*LivePrice) []*LivePrice {
	result := make([]*LivePrice, 0)

	// Start with fetched prices
	for _, p := range fetched {
		result = append(result, p)
	}

	// Track which model keys are covered
	covered := make(map[string]bool)
	for k := range fetched {
		covered[k] = true
	}

	// Add catalog entries not already covered by fetched prices
	for providerID, models := range ProviderModels {
		for _, m := range models {
			key := providerID + "/" + m.ModelID
			if covered[key] || covered[m.ModelID] {
				continue
			}
			result = append(result, &LivePrice{
				ProviderID: providerID,
				ModelID:    m.ModelID,
				InputCost:  m.InputCost,
				OutputCost: m.OutputCost,
				Source:     "catalog",
				FetchedAt:  time.Now(),
			})
			covered[key] = true
		}
	}

	return result
}

// UpdatePrices updates the cached prices from fetched data.
func (ps *PricingSyncer) UpdatePrices(prices map[string]*LivePrice) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	maps.Copy(ps.prices, prices)
}

// GetPrice returns the cached price for a model key, or nil if not found.
func (ps *PricingSyncer) GetPrice(modelKey string) *LivePrice {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.prices[modelKey]
}

// Sync fetches prices from all configured providers and updates the cache.
func (ps *PricingSyncer) Sync(ctx context.Context) error {
	allFetched := make(map[string]*LivePrice)

	if ps.config.OpenRouterURL != "" {
		prices, err := ps.FetchOpenRouter(ctx)
		if err != nil {
			ps.logger.Warn("Failed to fetch OpenRouter pricing", "error", err)
		} else {
			maps.Copy(allFetched, prices)
		}
	}

	// Merge with catalog
	merged := ps.MergeWithCatalog(allFetched)

	// Update cache
	mergedMap := make(map[string]*LivePrice, len(merged))
	for _, p := range merged {
		key := p.ProviderID + "/" + p.ModelID
		mergedMap[key] = p
	}
	ps.UpdatePrices(mergedMap)

	ps.logger.Info("Pricing sync complete", "total_models", len(mergedMap))
	return nil
}

// StartPeriodicSync starts background sync at the configured interval.
// Returns a stop channel.
func (ps *PricingSyncer) StartPeriodicSync(ctx context.Context) chan struct{} {
	stop := make(chan struct{})

	// Initial sync in background
	go func() {
		if err := ps.Sync(ctx); err != nil {
			ps.logger.Warn("Initial pricing sync failed", "error", err)
		}
	}()

	go func() {
		ticker := time.NewTicker(ps.config.SyncInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := ps.Sync(context.Background()); err != nil {
					ps.logger.Warn("Periodic pricing sync failed", "error", err)
				}
			case <-stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	return stop
}

// parseFloat safely parses a price string.
func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}
