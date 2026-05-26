package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// ModelInfo contains information about a configured model.
type ModelInfo struct {
	Provider     string   `json:"provider"`
	Model        string   `json:"model"`
	FullName     string   `json:"full_name"`
	BaseURL      string   `json:"base_url"`
	ContextLimit int      `json:"context_limit"`
	MaxOutput    int      `json:"max_output"`
	Capabilities []string `json:"capabilities"`
	IsDefault    bool     `json:"is_default"`
	InputCost    float64  `json:"input_cost"`
	OutputCost   float64  `json:"output_cost"`
}

// ProviderInfo contains information about a provider.
type ProviderInfo struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	API      string   `json:"api"`
	BaseURL  string   `json:"base_url"`
	Models   []string `json:"models"`
	HasCreds bool     `json:"has_credentials"`
}

// ModelService handles model configuration operations.
type ModelService struct {
	configPath string
	credStore  *llm.CredentialStore
	stateDir   string
}

// NewModelService creates a model service.
func NewModelService(configPath, stateDir string) (*ModelService, error) {
	var credStore *llm.CredentialStore
	if stateDir != "" {
		var err error
		credStore, err = llm.NewCredentialStore(stateDir)
		if err != nil {
			return nil, fmt.Errorf("failed to create credential store: %w", err)
		}
	}
	return &ModelService{
		configPath: configPath,
		credStore:  credStore,
		stateDir:   stateDir,
	}, nil
}

// List returns all configured models.
func (s *ModelService) List(ctx context.Context) ([]ModelInfo, error) {
	cfg, err := llm.LoadProvidersConfigDefault()
	if err != nil {
		return nil, wrapError("model", "List", err)
	}

	defaultRef := cfg.Model
	var models []ModelInfo

	for providerID, provider := range cfg.Providers {
		for modelID, modelDef := range provider.Models {
			caps := make([]string, len(modelDef.Capabilities))
			copy(caps, modelDef.Capabilities)

			fullName := providerID + "/" + modelID
			models = append(models, ModelInfo{
				Provider:     providerID,
				Model:        modelID,
				FullName:     fullName,
				BaseURL:      provider.Options.BaseURL,
				ContextLimit: modelDef.ContextLimit,
				MaxOutput:    modelDef.MaxOutput,
				Capabilities: caps,
				IsDefault:    fullName == defaultRef,
				InputCost:    modelDef.InputCost,
				OutputCost:   modelDef.OutputCost,
			})
		}
	}

	return models, nil
}

// Providers returns all available providers.
func (s *ModelService) Providers(ctx context.Context) ([]ProviderInfo, error) {
	cfg, err := llm.LoadProvidersConfigDefault()
	if err != nil {
		return nil, wrapError("model", "Providers", err)
	}

	var providers []ProviderInfo
	for providerID, provider := range cfg.Providers {
		modelIDs := make([]string, 0, len(provider.Models))
		for modelID := range provider.Models {
			modelIDs = append(modelIDs, modelID)
		}

		hasCreds := false
		if s.credStore != nil {
			_, hasCreds = s.credStore.Get(providerID)
		}

		providers = append(providers, ProviderInfo{
			ID:       providerID,
			Name:     providerID,
			API:      provider.API,
			BaseURL:  provider.Options.BaseURL,
			Models:   modelIDs,
			HasCreds: hasCreds,
		})
	}

	return providers, nil
}

// GetDefault returns the default model.
func (s *ModelService) GetDefault(ctx context.Context) (*ModelInfo, error) {
	cfg, err := llm.LoadProvidersConfigDefault()
	if err != nil {
		return nil, wrapError("model", "GetDefault", err)
	}

	if cfg.Model == "" {
		return nil, wrapError("model", "GetDefault", ErrNotFound)
	}

	parts := strings.SplitN(cfg.Model, "/", 2)
	if len(parts) != 2 {
		return nil, wrapError("model", "GetDefault", fmt.Errorf("invalid model ref: %s", cfg.Model))
	}

	providerID, modelID := parts[0], parts[1]
	provider, ok := cfg.Providers[providerID]
	if !ok {
		return nil, wrapError("model", "GetDefault", ErrNotFound)
	}

	modelDef, ok := provider.Models[modelID]
	if !ok {
		return nil, wrapError("model", "GetDefault", ErrNotFound)
	}

	caps := make([]string, len(modelDef.Capabilities))
	copy(caps, modelDef.Capabilities)

	return &ModelInfo{
		Provider:     providerID,
		Model:        modelID,
		FullName:     cfg.Model,
		BaseURL:      provider.Options.BaseURL,
		ContextLimit: modelDef.ContextLimit,
		MaxOutput:    modelDef.MaxOutput,
		Capabilities: caps,
		IsDefault:    true,
		InputCost:    modelDef.InputCost,
		OutputCost:   modelDef.OutputCost,
	}, nil
}

// SetDefault sets the default model.
func (s *ModelService) SetDefault(ctx context.Context, provider, model string) error {
	cfg, err := llm.LoadProvidersConfigDefault()
	if err != nil {
		return wrapError("model", "SetDefault", err)
	}

	// Validate the model exists
	providerCfg, ok := cfg.Providers[provider]
	if !ok {
		return wrapError("model", "SetDefault", ErrNotFound)
	}
	if _, ok := providerCfg.Models[model]; !ok {
		return wrapError("model", "SetDefault", ErrNotFound)
	}

	cfg.Model = provider + "/" + model

	return s.writeConfig(cfg)
}

// Remove removes a model from the configuration.
func (s *ModelService) Remove(ctx context.Context, provider, model string) error {
	cfg, err := llm.LoadProvidersConfigDefault()
	if err != nil {
		return wrapError("model", "Remove", err)
	}

	providerCfg, ok := cfg.Providers[provider]
	if !ok {
		return wrapError("model", "Remove", ErrNotFound)
	}

	// Check if this is the default model
	if cfg.Model == provider+"/"+model {
		cfg.Model = ""
	}

	delete(providerCfg.Models, model)

	// Remove provider if no models left
	if len(providerCfg.Models) == 0 {
		delete(cfg.Providers, provider)
	}

	return s.writeConfig(cfg)
}

// GetCredential returns the API key for a provider (masked).
func (s *ModelService) GetCredential(ctx context.Context, providerID string) (string, error) {
	if s.credStore == nil {
		return "", wrapError("model", "GetCredential", ErrUnavailable)
	}

	key, ok := s.credStore.Get(providerID)
	if !ok {
		return "", wrapError("model", "GetCredential", ErrNotFound)
	}

	// Return masked key for security
	if len(key) > 8 {
		return strings.Repeat("*", len(key)-4) + key[len(key)-4:], nil
	}
	return strings.Repeat("*", len(key)), nil
}

// GetCredentialRaw returns the raw API key (for config export).
func (s *ModelService) GetCredentialRaw(ctx context.Context, providerID string) (string, error) {
	if s.credStore == nil {
		return "", wrapError("model", "GetCredentialRaw", ErrUnavailable)
	}

	key, ok := s.credStore.Get(providerID)
	if !ok {
		return "", wrapError("model", "GetCredentialRaw", ErrNotFound)
	}
	return key, nil
}

// SetCredential stores an API key for a provider.
func (s *ModelService) SetCredential(ctx context.Context, providerID, apiKey string) error {
	if s.credStore == nil {
		return wrapError("model", "SetCredential", ErrUnavailable)
	}

	if err := s.credStore.Set(providerID, apiKey); err != nil {
		return wrapError("model", "SetCredential", err)
	}
	return nil
}

// DeleteCredential removes an API key.
func (s *ModelService) DeleteCredential(ctx context.Context, providerID string) error {
	if s.credStore == nil {
		return wrapError("model", "DeleteCredential", ErrUnavailable)
	}

	if err := s.credStore.Delete(providerID); err != nil {
		return wrapError("model", "DeleteCredential", err)
	}
	return nil
}

// writeConfig writes the configuration to disk.
// Note: This is delegated to the HTTP ConfigService for now since it handles JSON5.
func (s *ModelService) writeConfig(cfg *llm.ProvidersConfig) error {
	return wrapError("model", "writeConfig", fmt.Errorf("use ConfigService for config modification"))
}
