package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Context discovery (llm-resilience-forest tree 05, leaf 01; DECISIONS.md
// D13): fills model context lengths from the provider endpoints that expose
// them. OpenAI and Anthropic do NOT expose context length on any endpoint,
// so deliberately NO fetcher exists for either provider (D13) — their
// models keep catalog/config values.
//
// Shape mirrors PricingSyncer (pricing_sync.go): a syncer struct with
// per-provider fetchers, an injected HTTP client, an interval, and a merge
// step that NEVER mutates config-file-declared values unless the
// allow_context_override flag is set (catalog/config = fallback; remote
// truth wins when explicitly allowed).

// ContextDiscoveryConfig configures the context-length discovery syncer.
type ContextDiscoveryConfig struct {
	// Enabled turns discovery on. Default false (zero-value OFF; no
	// network traffic when off).
	Enabled bool
	// Interval is the re-sync cadence. Zero means the 6h default.
	Interval time.Duration
	// AllowContextOverride lets a discovered value replace a NON-ZERO
	// catalog/config value. Explicit models.json5 context_limit values
	// always win regardless of this flag.
	AllowContextOverride bool
}

// ContextDiscovery fetches per-model context lengths from provider
// endpoints and merges them into the model registry per the precedence
// rule (master Contract 3).
type ContextDiscovery struct {
	cfg    ContextDiscoveryConfig
	client *http.Client

	mu        sync.RWMutex
	fetchers  map[string]Fetcher // providerID -> fetcher
	endpoints map[string]epCreds // providerID -> base URL + api key
	resolver  *Resolver          // write target for the merge (may be nil)
	logger    *slog.Logger
}

// epCreds holds the resolved endpoint for one provider's fetcher.
type epCreds struct {
	baseURL string
	apiKey  string
}

// Fetcher fetches context lengths for one provider. Keys are
// "provider/model" ids matching registry ids. baseURL is the provider's
// resolved endpoint base (leaf 03 registers its fetcher against this
// signature — master Contract 1 pins it).
type Fetcher func(ctx context.Context, baseURL, apiKey string) (map[string]int, error)

// NewContextDiscovery creates a new context-length discovery syncer.
func NewContextDiscovery(cfg ContextDiscoveryConfig, client *http.Client) *ContextDiscovery {
	if cfg.Interval <= 0 {
		cfg.Interval = 6 * time.Hour
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &ContextDiscovery{
		cfg:       cfg,
		client:    client,
		fetchers:  make(map[string]Fetcher),
		endpoints: make(map[string]epCreds),
		logger:    slog.Default(),
	}
}

// SetLogger overrides the default logger. Call before Start.
func (d *ContextDiscovery) SetLogger(l *slog.Logger) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if l != nil {
		d.logger = l
	}
}

// Client returns the syncer's HTTP client for fetcher closures that build
// their own requests.
func (d *ContextDiscovery) Client() *http.Client {
	return d.client
}

// Enabled reports whether discovery is on.
func (d *ContextDiscovery) Enabled() bool {
	return d.cfg.Enabled
}

// RegisterFetcher registers a fetcher under a provider ID (master
// Contract 1; leaf 03 codes against this exact signature). Registering
// for a provider without an endpoint is harmless: Sync skips it.
func (d *ContextDiscovery) RegisterFetcher(providerID string, f Fetcher) {
	if f == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fetchers[providerID] = f
}

// SetEndpoint records the base URL (and optional API key) a provider's
// fetcher should be called with. The daemon wiring resolves these from
// provider config; the local-models endpoint comes from the
// RuntimeManager's llama.cpp endpoint key.
func (d *ContextDiscovery) SetEndpoint(providerID, baseURL, apiKey string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.endpoints[providerID] = epCreds{baseURL: baseURL, apiKey: apiKey}
}

// SetResolver attaches the resolver whose model sets the merge writes
// into (audit R3 write path). Pass nil to write only the display catalog.
// No-op on a nil receiver to honor the typed-nil setter rule (AGENTS.md).
func (d *ContextDiscovery) SetResolver(r *Resolver) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.resolver = r
}

// Sync fetches from every registered fetcher and merges the results per
// the precedence rule. A single provider's failure is logged and skipped
// (PricingSyncer error tolerance), never a hard error.
func (d *ContextDiscovery) Sync(ctx context.Context) error {
	d.mu.RLock()
	type job struct {
		providerID string
		fetch      Fetcher
		creds      epCreds
	}
	jobs := make([]job, 0, len(d.fetchers))
	for id, f := range d.fetchers {
		jobs = append(jobs, job{providerID: id, fetch: f, creds: d.endpoints[id]})
	}
	logger := d.logger
	d.mu.RUnlock()

	all := make(map[string]int)
	for _, j := range jobs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if j.creds.baseURL == "" {
			// No endpoint resolved for this provider (e.g. the provider
			// is not configured) — nothing to fetch from.
			continue
		}
		ctxs, err := j.fetch(ctx, j.creds.baseURL, j.creds.apiKey)
		if err != nil {
			logger.Warn("context discovery: provider fetch failed",
				"provider", j.providerID, "error", err)
			continue
		}
		maps.Copy(all, ctxs)
	}

	d.applyDiscovered(all)
	return nil
}

// Start runs the ticker loop at the configured interval. The initial sync
// happens in the background (PricingSyncer pattern); the loop exits when
// ctx is cancelled.
func (d *ContextDiscovery) Start(ctx context.Context) {
	go func() {
		if err := d.Sync(ctx); err != nil {
			d.mu.RLock()
			logger := d.logger
			d.mu.RUnlock()
			logger.Warn("context discovery: initial sync failed", "error", err)
		}
	}()

	go func() {
		ticker := time.NewTicker(d.cfg.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := d.Sync(ctx); err != nil {
					d.mu.RLock()
					logger := d.logger
					d.mu.RUnlock()
					logger.Warn("context discovery: periodic sync failed", "error", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// applyDiscovered merges fetched values into the resolver model sets and
// the display catalog per the precedence rule (master Contract 3):
//
//  1. models.json5 explicit context_limit wins ALWAYS.
//  2. Discovered value wins when the catalog/config value is 0/absent.
//  3. Discovered value wins over a non-zero catalog value ONLY when
//     allow_context_override is true.
func (d *ContextDiscovery) applyDiscovered(discovered map[string]int) {
	if len(discovered) == 0 {
		return
	}
	d.mu.RLock()
	resolver := d.resolver
	logger := d.logger
	override := d.cfg.AllowContextOverride
	d.mu.RUnlock()
	if resolver != nil {
		resolver.SetContextLimits(discovered, override, logger)
	}
	refreshCatalogContexts(discovered, override, logger)
}

// resolvedContext applies the precedence rule for one model. Returns the
// effective value and whether a delta actually occurred.
func resolvedContext(current int, discovered int, override bool) (int, bool) {
	// Rule 1: an explicit non-zero config/catalog value wins ALWAYS...
	if current != 0 && !override {
		return current, false
	}
	// ...except under allow_context_override (rule 3), where the
	// discovered value replaces even a non-zero catalog value.
	if current != 0 && override {
		if discovered == 0 || discovered == current {
			return current, false
		}
		return discovered, true
	}
	// Rule 2: current is 0/absent — discovered fills the gap (when the
	// provider actually exposed one).
	if discovered != 0 {
		return discovered, true
	}
	return current, false
}

// refreshCatalogContexts updates the static display catalog
// (ProviderModels — the TUI model picker's source) with the same rule so
// display and runtime agree. Writes go through SetCatalogContextWindow
// (catalogMu-guarded, slice-replacing), so concurrent readers of the old
// slice are unaffected.
func refreshCatalogContexts(discovered map[string]int, override bool, logger *slog.Logger) {
	for key, discoveredVal := range discovered {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			continue
		}
		providerID := parts[0]
		modelID := parts[1]
		entry, ok := GetModel(providerID, modelID)
		if !ok {
			continue
		}
		newVal, changed := resolvedContext(entry.ContextWindow, discoveredVal, override)
		if !changed {
			continue
		}
		if SetCatalogContextWindow(providerID, modelID, newVal) {
			logger.Info("context window updated", "provider", providerID, "model", modelID,
				"from", entry.ContextWindow, "to", newVal, "surface", "catalog")
		}
	}
}

// ---------------------------------------------------------------------------
// BUILT-IN fetchers
// ---------------------------------------------------------------------------

// ollamaTagsResponse models Ollama /api/tags.
type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// ollamaShowResponse models Ollama /api/show. context_length appears
// top-level on recent builds; older builds nest it under model_info
// (e.g. "qwen2.context_length").
type ollamaShowResponse struct {
	ContextLength int            `json:"context_length"`
	ModelInfo     map[string]any `json:"model_info"`
}

// FetchOllamaContexts lists models via /api/tags then queries /api/show
// per model for its context length. Malformed JSON is tolerated: it
// yields an empty result with a logged warning (leaf Task 1), while
// transport-level failures surface as errors for Sync to log-and-skip.
func FetchOllamaContexts(ctx context.Context, client *http.Client, logger *slog.Logger, baseURL, apiKey string) (map[string]int, error) {
	tagsURL := strings.TrimSuffix(baseURL, "/") + "/api/tags"
	tags, err := getJSONTolerant[ollamaTagsResponse](ctx, client, logger, tagsURL, apiKey)
	if err != nil {
		return nil, fmt.Errorf("listing ollama models: %w", err)
	}

	out := make(map[string]int, len(tags.Models))
	for _, m := range tags.Models {
		if m.Name == "" {
			continue
		}
		showURL := strings.TrimSuffix(baseURL, "/") + "/api/show"
		body, err := json.Marshal(map[string]string{"model": m.Name})
		if err != nil {
			continue
		}
		show, err := postJSON[ollamaShowResponse](ctx, client, showURL, apiKey, body)
		if err != nil {
			// Malformed/failed per-model show: log and keep going.
			logger.Warn("context discovery: ollama /api/show failed",
				"model", m.Name, "error", err)
			continue
		}
		if v := ollamaContextFromShow(show); v > 0 {
			out[ProviderIDOllama+"/"+m.Name] = v
		}
	}
	return out, nil
}

// ollamaContextFromShow extracts the context length from an /api/show
// payload: top-level context_length first, then the nested
// model_info.*.<suffix>.context_length variant.
func ollamaContextFromShow(show ollamaShowResponse) int {
	if show.ContextLength > 0 {
		return show.ContextLength
	}
	for k, raw := range show.ModelInfo {
		if !strings.HasSuffix(k, ".context_length") {
			continue
		}
		switch v := raw.(type) {
		case float64:
			if v > 0 {
				return int(v)
			}
		case int:
			if v > 0 {
				return v
			}
		}
	}
	return 0
}

// openRouterModelsResponse models the OpenRouter /api/v1/models response
// (same fetch as PricingSyncer.FetchOpenRouter; the context field is
// entries[].context_length).
type openRouterModelsResponse struct {
	Data []struct {
		ID            string `json:"id"`
		ContextLength int    `json:"context_length"`
	} `json:"data"`
}

// FetchOpenRouterContexts parses context lengths from OpenRouter's models
// endpoint. Keys are registry ids: openrouter/<openrouter-id>. Malformed
// JSON is tolerated: empty result + logged warning (leaf Task 1).
func FetchOpenRouterContexts(ctx context.Context, client *http.Client, logger *slog.Logger, baseURL, apiKey string) (map[string]int, error) {
	url := baseURL
	if url == "" {
		url = "https://openrouter.ai/api/v1/models"
	}
	resp, err := getJSONTolerant[openRouterModelsResponse](ctx, client, logger, url, apiKey)
	if err != nil {
		return nil, fmt.Errorf("fetching openrouter models: %w", err)
	}
	out := make(map[string]int, len(resp.Data))
	for _, m := range resp.Data {
		if m.ID == "" || m.ContextLength <= 0 {
			continue
		}
		out[providerIDOpenRouter+"/"+m.ID] = m.ContextLength
	}
	return out, nil
}

// providerIDOpenRouter is the registry id for OpenRouter (provider_registry.go
// declares the canonical entry with this literal id).
const providerIDOpenRouter = "openrouter"

// localModelsPropsResponse models llama.cpp server /props.
type localModelsPropsResponse struct {
	DefaultGenerationSettings struct {
		NCtx int `json:"n_ctx"`
	} `json:"default_generation_settings"`
}

// FetchLocalModelsContexts fetches the llama.cpp server's default n_ctx
// via /props. The endpoint serves ONE default n_ctx per server while
// multiple GGUFs merge into one endpoint, so the value applies as the
// default for every model key listed. Keys are
// LocalModelsProviderID + "/" + modelKey.
func FetchLocalModelsContexts(ctx context.Context, client *http.Client, logger *slog.Logger, baseURL string, modelKeys []string) map[string]int {
	out := make(map[string]int, len(modelKeys))
	if len(modelKeys) == 0 || baseURL == "" {
		return out
	}
	propsURL := strings.TrimSuffix(baseURL, "/") + "/props"
	props, err := getJSON[localModelsPropsResponse](ctx, client, propsURL, "")
	if err != nil {
		// Malformed/unreachable props: tolerated, empty result.
		logger.Warn("context discovery: llama.cpp /props fetch failed", "error", err)
		return out
	}
	nCtx := props.DefaultGenerationSettings.NCtx
	if nCtx <= 0 {
		return out
	}
	for _, key := range modelKeys {
		out[LocalModelsProviderID+"/"+key] = nCtx
	}
	return out
}

// jsonDecodeError marks a malformed-JSON failure distinctly from
// transport failures, so fetchers can tolerate it (empty result + logged
// warning) while still surfacing unreachable-server errors to Sync.
type jsonDecodeError struct{ err error }

func (e *jsonDecodeError) Error() string { return e.err.Error() }
func (e *jsonDecodeError) Unwrap() error { return e.err }

// getJSONTolerant fetches and decodes a JSON GET. A malformed body is
// tolerated: zero value + logged warning + nil error. Transport and
// HTTP-status failures propagate for Sync's log-and-skip handling.
func getJSONTolerant[T any](ctx context.Context, client *http.Client, logger *slog.Logger, url, apiKey string) (T, error) {
	var zero T
	out, err := getJSON[T](ctx, client, url, apiKey)
	if err != nil {
		if _, ok := errors.AsType[*jsonDecodeError](err); ok {
			logger.Warn("context discovery: malformed JSON response", "url", url, "error", err)
			return zero, nil
		}
		return zero, err
	}
	return out, nil
}

// getJSON fetches and decodes a JSON GET.
func getJSON[T any](ctx context.Context, client *http.Client, url, apiKey string) (T, error) {
	var out T
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return out, fmt.Errorf("creating request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return out, fmt.Errorf("reading response: %w", err)
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, &jsonDecodeError{err: fmt.Errorf("parsing response: %w", err)}
	}
	return out, nil
}

// postJSON posts a JSON body and decodes a JSON response.
func postJSON[T any](ctx context.Context, client *http.Client, url, apiKey string, body []byte) (T, error) {
	var out T
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return out, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return out, fmt.Errorf("reading response: %w", err)
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return out, fmt.Errorf("parsing response: %w", err)
	}
	return out, nil
}
