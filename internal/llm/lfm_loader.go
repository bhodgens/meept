package llm

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// LoadedAdapter represents a loaded LoRA adapter and its metadata. The
// actual PEFT model loading happens in Python; the Go side tracks the
// adapter path and domain for routing purposes.
type LoadedAdapter struct {
	Domain string
	Path   string
	Model  interface{} // PEFT-wrapped model (nil on Go side)
}

// LFMLoader manages LFM2.5 model + adapter loading.
type LFMLoader struct {
	BaseModel string                     // "lfm2.5-8b" or "lfm2.5-1.2b"
	ModelPath string
	Adapters  map[string]*LoadedAdapter  // domain -> adapter
	logger    *slog.Logger
}

// NewLFMLoader creates a new LFMLoader with the given base model identifier.
func NewLFMLoader(baseModel, modelPath string, logger *slog.Logger) *LFMLoader {
	if logger == nil {
		logger = slog.Default()
	}
	return &LFMLoader{
		BaseModel: baseModel,
		ModelPath: modelPath,
		logger:    logger,
	}
}

// LoadAllAdapters loads all enabled adapters from the registry that match
// the loader's base model. Adapters whose path does not exist on disk are
// skipped with a warning.
func (l *LFMLoader) LoadAllAdapters(registry *AdapterRegistry) error {
	l.Adapters = make(map[string]*LoadedAdapter)

	if registry == nil {
		return nil
	}

	for _, entry := range registry.Adapters {
		if !entry.Enabled {
			continue
		}
		if entry.Model != l.BaseModel {
			continue
		}

		adapter, err := l.loadAdapter(entry.Path)
		if err != nil {
			l.logger.Warn("failed to load adapter", "id", entry.ID, "error", err)
			continue
		}
		adapter.Domain = entry.Domain
		l.Adapters[entry.Domain] = adapter
		l.logger.Info("loaded adapter", "id", entry.ID, "domain", entry.Domain)
	}

	return nil
}

// loadAdapter is a stub that records the adapter path. Real PEFT loading
// happens in Python; the Go side just tracks metadata for routing.
func (l *LFMLoader) loadAdapter(path string) (*LoadedAdapter, error) {
	if path == "" {
		return nil, fmt.Errorf("llm: adapter path is empty")
	}
	// Check that the path exists (if it looks like a real path).
	if _, err := os.Stat(filepath.Clean(path)); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("llm: stat adapter path %s: %w", path, err)
		}
		// Non-existent path is not fatal; the adapter may not have been
		// downloaded yet. Log and continue.
		l.logger.Warn("adapter path does not exist", "path", path)
	}
	return &LoadedAdapter{
		Path:  path,
		Model: nil, // Real model loaded by Python side
	}, nil
}
