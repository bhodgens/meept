package llm

import (
	"encoding/json"
	"fmt"
	"os"
)

// AdapterRegistry holds a list of trained LoRA adapter entries. This type
// is defined locally in the llm package to avoid an import cycle with
// internal/config. Version and GeneratedAt preserve the provenance fields
// written by scripts/generate_adapter_config.py so they are not silently
// dropped on load.
type AdapterRegistry struct {
	Adapters    []AdapterEntry `json:"adapters"`
	Version     int            `json:"version"`
	GeneratedAt string         `json:"generated_at"`
}

// AdapterEntry describes a single adapter for loading purposes.
type AdapterEntry struct {
	ID          string `json:"id"`
	Domain      string `json:"domain"`
	Model       string `json:"model"`
	Path        string `json:"path"`
	CreatedAt   string `json:"created_at"`
	TrainingMD5 string `json:"training_md5"`
	Enabled     bool   `json:"enabled"`
}

// LoadAdapterRegistry reads the adapter registry JSON file. If the file does
// not exist, an empty registry is returned (no error).
func LoadAdapterRegistry(registryPath string) (*AdapterRegistry, error) {
	data, err := os.ReadFile(registryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &AdapterRegistry{}, nil
		}
		return nil, fmt.Errorf("llm: read adapter registry %s: %w", registryPath, err)
	}

	var registry AdapterRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("llm: unmarshal adapter registry: %w", err)
	}
	return &registry, nil
}
