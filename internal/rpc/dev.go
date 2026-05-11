package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// DevHandler provides developer/debugging RPC methods.
type DevHandler struct {
	mu           sync.RWMutex
	modelsConfig *config.ModelsConfig
	modelsList   []modelEntry
	currentIdx   int
	llmClient    *llm.Client
}

// modelEntry holds a flattened model entry from the config.
type modelEntry struct {
	Index        int      `json:"index"`
	Provider     string   `json:"provider"`
	Model        string   `json:"model"`
	FullName     string   `json:"full_name"`
	BaseURL      string   `json:"base_url"`
	APIKey       string   `json:"-"` // Don't expose
	ContextLimit int      `json:"context_limit"`
	MaxOutput    int      `json:"max_output"`
	Temperature  float64  `json:"temperature"`
	Capabilities []string `json:"capabilities"`
}

// NewDevHandler creates a new dev handler.
func NewDevHandler() *DevHandler {
	h := &DevHandler{}
	h.loadModels()
	return h
}

// loadModels loads and flattens the models configuration.
func (h *DevHandler) loadModels() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	cfg, err := config.LoadModelsConfigDefault()
	if err != nil {
		return err
	}

	h.modelsConfig = cfg
	h.modelsList = []modelEntry{}

	// Flatten providers and models into a list
	idx := 0
	for providerName, provider := range cfg.Providers {
		for modelName, model := range provider.Models {
			entry := modelEntry{
				Index:        idx,
				Provider:     providerName,
				Model:        modelName,
				FullName:     fmt.Sprintf("%s/%s", providerName, modelName),
				BaseURL:      provider.Options.BaseURL,
				APIKey:       provider.Options.APIKey,
				ContextLimit: model.ContextLimit,
				MaxOutput:    model.MaxOutput,
				Temperature:  model.Temperature,
				Capabilities: model.Capabilities,
			}
			h.modelsList = append(h.modelsList, entry)
			idx++
		}
	}

	// Sort by provider, then model name for consistent ordering
	sort.Slice(h.modelsList, func(i, j int) bool {
		if h.modelsList[i].Provider != h.modelsList[j].Provider {
			return h.modelsList[i].Provider < h.modelsList[j].Provider
		}
		return h.modelsList[i].Model < h.modelsList[j].Model
	})

	// Re-assign indices after sorting
	for i := range h.modelsList {
		h.modelsList[i].Index = i
	}

	// Find and set current model
	currentFullName := cfg.Model
	for i, m := range h.modelsList {
		if m.FullName == currentFullName {
			h.currentIdx = i
			break
		}
	}

	// Initialize LLM client with current model
	if len(h.modelsList) > 0 {
		h.initLLMClient()
	}

	return nil
}

// initLLMClient initializes the LLM client with the current model.
func (h *DevHandler) initLLMClient() {
	if h.currentIdx >= len(h.modelsList) {
		return
	}

	entry := h.modelsList[h.currentIdx]

	// Build capabilities map
	caps := make(map[string]bool)
	for _, c := range entry.Capabilities {
		caps[c] = true
	}

	modelConfig := &llm.ModelConfig{
		BaseURL:      entry.BaseURL,
		ModelID:      entry.Model,
		APIKey:       entry.APIKey,
		MaxTokens:    entry.MaxOutput,
		Temperature:  entry.Temperature,
		ContextLimit: entry.ContextLimit,
		Capabilities: caps,
		ProviderID:   entry.Provider,
	}

	h.llmClient = llm.NewClient(modelConfig)
}

// RegisterDevMethods registers all dev methods on the server.
func (h *DevHandler) RegisterDevMethods(server *Server) {
	server.RegisterHandler("dev.list_models", h.handleListModels)
	server.RegisterHandler("dev.current_model", h.handleCurrentModel)
	server.RegisterHandler("dev.switch_model", h.handleSwitchModel)
	server.RegisterHandler("dev.test_llm", h.handleTestLLM)
	server.RegisterHandler("dev.config", h.handleConfig)
	server.RegisterHandler("dev.reload", h.handleReload)
}

// handleListModels returns all available models.
func (h *DevHandler) handleListModels(ctx context.Context, params json.RawMessage) (any, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Build response with current marker
	models := make([]map[string]any, len(h.modelsList))
	var currentModel string

	for i, m := range h.modelsList {
		isCurrent := i == h.currentIdx
		if isCurrent {
			currentModel = m.FullName
		}

		models[i] = map[string]any{
			"index":         m.Index,
			"provider":      m.Provider,
			"model":         m.Model,
			"full_name":     m.FullName,
			"base_url":      m.BaseURL,
			"context_limit": m.ContextLimit,
			"max_output":    m.MaxOutput,
			"capabilities":  m.Capabilities,
			"current":       isCurrent,
		}
	}

	return map[string]any{
		"models":        models,
		"current_model": currentModel,
		"count":         len(models),
	}, nil
}

// handleCurrentModel returns the current model details.
func (h *DevHandler) handleCurrentModel(ctx context.Context, params json.RawMessage) (any, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.modelsList) == 0 {
		return nil, fmt.Errorf("no models configured")
	}

	if h.currentIdx >= len(h.modelsList) {
		return nil, fmt.Errorf("invalid current model index")
	}

	m := h.modelsList[h.currentIdx]
	return map[string]any{
		"index":         m.Index,
		"provider":      m.Provider,
		"model":         m.Model,
		"full_name":     m.FullName,
		"base_url":      m.BaseURL,
		"context_limit": m.ContextLimit,
		"max_output":    m.MaxOutput,
		"capabilities":  m.Capabilities,
	}, nil
}

// handleSwitchModel switches to a different model.
func (h *DevHandler) handleSwitchModel(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		Index *int   `json:"index"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	var targetIdx = -1

	// Switch by index or name
	switch {
	case req.Index != nil:
		if *req.Index < 0 || *req.Index >= len(h.modelsList) {
			return map[string]any{
				"success": false,
				"message": fmt.Sprintf("invalid index %d, valid range is 0-%d", *req.Index, len(h.modelsList)-1),
			}, nil
		}
		targetIdx = *req.Index
	case req.Name != "":
		// Switch by name (provider/model or just model)
		for i, m := range h.modelsList {
			if m.FullName == req.Name || m.Model == req.Name {
				targetIdx = i
				break
			}
		}
		if targetIdx == -1 {
			return map[string]any{
				"success": false,
				"message": fmt.Sprintf("model not found: %s", req.Name),
			}, nil
		}
	default:
		return map[string]any{
			"success": false,
			"message": "must specify either 'index' or 'name'",
		}, nil
	}

	h.currentIdx = targetIdx
	h.initLLMClient()

	m := h.modelsList[targetIdx]
	return map[string]any{
		"success":  true,
		"model":    m.Model,
		"provider": m.Provider,
		"message":  fmt.Sprintf("switched to %s", m.FullName),
	}, nil
}

// handleTestLLM sends a test message to the current LLM.
func (h *DevHandler) handleTestLLM(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if req.Message == "" {
		req.Message = "Hello! Please respond with a short greeting."
	}

	h.mu.RLock()
	client := h.llmClient
	currentModel := ""
	if h.currentIdx < len(h.modelsList) {
		currentModel = h.modelsList[h.currentIdx].FullName
	}
	h.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("no LLM client configured")
	}

	start := time.Now()

	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: req.Message},
	}

	resp, err := client.Chat(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	duration := time.Since(start)

	return map[string]any{
		"response": resp.Content,
		"model":    currentModel,
		"tokens": map[string]int{
			"prompt":     resp.Usage.PromptTokens,
			"completion": resp.Usage.CompletionTokens,
			"total":      resp.Usage.TotalTokens,
		},
		"duration_ms": duration.Milliseconds(),
	}, nil
}

// handleConfig returns the current configuration.
func (h *DevHandler) handleConfig(ctx context.Context, params json.RawMessage) (any, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.modelsConfig == nil {
		return map[string]any{}, nil
	}

	// Return sanitized config (no API keys)
	providers := make(map[string]any)
	for name, p := range h.modelsConfig.Providers {
		models := make(map[string]any)
		for mName, m := range p.Models {
			models[mName] = map[string]any{
				"name":          m.Name,
				"capabilities":  m.Capabilities,
				"context_limit": m.ContextLimit,
				"max_output":    m.MaxOutput,
			}
		}
		providers[name] = map[string]any{
			"api":      p.API,
			"base_url": p.Options.BaseURL,
			"models":   models,
		}
	}

	return map[string]any{
		"default_model": h.modelsConfig.Model,
		"small_model":   h.modelsConfig.SmallModel,
		"providers":     providers,
	}, nil
}

// handleReload reloads the models configuration.
func (h *DevHandler) handleReload(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.loadModels(); err != nil {
		return map[string]any{
			"success": false,
			"error":   err.Error(),
		}, err
	}

	return map[string]any{
		"success":      true,
		"models_count": len(h.modelsList),
	}, nil
}
