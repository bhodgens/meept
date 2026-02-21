// Package adapters provides adapter management for local and API-based fine-tuning.
package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/caimlas/meept/internal/shadow"
)

// OllamaAdapter manages LoRA adapters for Ollama.
type OllamaAdapter struct {
	endpoint string
	client   *http.Client
	store    shadow.AdaptersStore
}

// NewOllamaAdapter creates a new Ollama adapter manager.
func NewOllamaAdapter(endpoint string, store shadow.AdaptersStore) *OllamaAdapter {
	return &OllamaAdapter{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		store: store,
	}
}

// ListModels lists available Ollama models.
func (a *OllamaAdapter) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", a.endpoint+"/api/tags", nil)
	if err != nil {
		return nil, err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]string, len(result.Models))
	for i, m := range result.Models {
		models[i] = m.Name
	}

	return models, nil
}

// CreateModelWithAdapter creates a model variant using a LoRA adapter.
// Note: Ollama doesn't directly support LoRA loading at runtime,
// so this creates a Modelfile with adapter weights baked in.
func (a *OllamaAdapter) CreateModelWithAdapter(ctx context.Context, baseName, adapterName, adapterPath string) error {
	// Create Modelfile content
	modelfile := fmt.Sprintf(`FROM %s
ADAPTER %s
`, baseName, adapterPath)

	// Create the model via Ollama API
	payload := map[string]string{
		"name":      adapterName,
		"modelfile": modelfile,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.endpoint+"/api/create", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create model: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create failed (%d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// DeleteModel removes a model from Ollama.
func (a *OllamaAdapter) DeleteModel(ctx context.Context, name string) error {
	payload := map[string]string{"name": name}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "DELETE", a.endpoint+"/api/delete", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete model: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete failed: %d", resp.StatusCode)
	}

	return nil
}

// ValidateAdapterPath checks if an adapter path exists and contains expected files.
func (a *OllamaAdapter) ValidateAdapterPath(adapterPath string) error {
	info, err := os.Stat(adapterPath)
	if err != nil {
		return fmt.Errorf("adapter path not found: %w", err)
	}

	if !info.IsDir() {
		// Could be a single file adapter
		return nil
	}

	// Check for common LoRA file patterns
	expectedFiles := []string{
		"adapter_config.json",
		"adapter_model.bin",
		"adapter_model.safetensors",
	}

	hasAdapterFile := false
	for _, f := range expectedFiles {
		if _, err := os.Stat(filepath.Join(adapterPath, f)); err == nil {
			hasAdapterFile = true
			break
		}
	}

	if !hasAdapterFile {
		return fmt.Errorf("adapter path does not contain expected adapter files")
	}

	return nil
}

// RegisterAdapter registers a new adapter and optionally loads it.
func (a *OllamaAdapter) RegisterAdapter(ctx context.Context, adapter *shadow.Adapter, load bool) error {
	// Validate the adapter path
	if err := a.ValidateAdapterPath(adapter.AdapterPath); err != nil {
		return fmt.Errorf("invalid adapter: %w", err)
	}

	// Save to store
	if err := a.store.SaveAdapter(ctx, adapter); err != nil {
		return fmt.Errorf("failed to save adapter: %w", err)
	}

	// Create model with adapter if requested
	if load {
		modelName := adapter.ModelBase + "-" + adapter.Name
		if err := a.CreateModelWithAdapter(ctx, adapter.ModelBase, modelName, adapter.AdapterPath); err != nil {
			return fmt.Errorf("failed to load adapter: %w", err)
		}
	}

	return nil
}

// ActivateAdapter activates an adapter by creating the corresponding Ollama model.
func (a *OllamaAdapter) ActivateAdapter(ctx context.Context, adapterID string) error {
	adapter, err := a.store.GetAdapter(ctx, adapterID)
	if err != nil || adapter == nil {
		return fmt.Errorf("adapter not found: %s", adapterID)
	}

	// Deactivate current active adapter first (if any)
	currentActive, _ := a.store.GetActiveAdapter(ctx, adapter.ModelBase)
	if currentActive != nil {
		// Remove the Ollama model for the old adapter
		oldModelName := currentActive.ModelBase + "-" + currentActive.Name
		_ = a.DeleteModel(ctx, oldModelName) // Ignore errors
	}

	// Create new model with adapter
	newModelName := adapter.ModelBase + "-" + adapter.Name
	if err := a.CreateModelWithAdapter(ctx, adapter.ModelBase, newModelName, adapter.AdapterPath); err != nil {
		return fmt.Errorf("failed to create adapter model: %w", err)
	}

	// Update store
	if err := a.store.SetActiveAdapter(ctx, adapterID); err != nil {
		return fmt.Errorf("failed to activate adapter: %w", err)
	}

	return nil
}

// GetActiveModelName returns the Ollama model name for the active adapter.
func (a *OllamaAdapter) GetActiveModelName(ctx context.Context, baseModel string) (string, error) {
	adapter, err := a.store.GetActiveAdapter(ctx, baseModel)
	if err != nil {
		return "", err
	}

	if adapter == nil {
		// No active adapter, use base model
		return baseModel, nil
	}

	return baseModel + "-" + adapter.Name, nil
}

// Ping checks if Ollama is reachable.
func (a *OllamaAdapter) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", a.endpoint+"/api/version", nil)
	if err != nil {
		return err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("ollama not reachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned status: %d", resp.StatusCode)
	}

	return nil
}
