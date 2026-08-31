package config

// AdapterRegistry documents the JSON schema produced by
// scripts/generate_adapter_config.py and consumed by internal/llm.LoadAdapterRegistry.
// The llm package redefines these types locally to avoid an import cycle
// (llm cannot import config).

// AdapterRegistry is the on-disk registry of trained LoRA adapters.
type AdapterRegistry struct {
	Adapters    []AdapterEntry `json:"adapters"`
	Version     int            `json:"version"`
	GeneratedAt string         `json:"generated_at,omitempty"`
}

// AdapterEntry describes a single trained adapter.
type AdapterEntry struct {
	ID          string `json:"id"` // "code-lfm2.5-8b-v1"
	Domain      string `json:"domain"`
	Model       string `json:"model"` // "lfm2.5-8b"
	Path        string `json:"path"`  // "~/.meept/adapters/code/lfm2.5-8b-v1"
	CreatedAt   string `json:"created_at"`
	TrainingMD5 string `json:"training_md5"` // Dataset fingerprint
	Enabled     bool   `json:"enabled"`
}
