package llm

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// adapterArtifactNames are files produced by PEFT/TRL save_model that indicate
// a usable LoRA adapter directory (any one is sufficient).
var adapterArtifactNames = []string{
	"adapter_config.json",
	"adapter_model.safetensors",
	"adapter_model.bin",
	"pytorch_lora_weights.safetensors",
	"pytorch_lora_weights.bin",
}

// versionSuffix matches trailing -vN on adapter directory names.
var versionSuffix = regexp.MustCompile(`-v(\d+)$`)

// LoadedAdapter represents a loaded LoRA adapter and its metadata. PEFT
// weight tensors live on disk (trained by Python); the Go side validates
// artifacts and routes adapter paths at inference time.
type LoadedAdapter struct {
	Domain  string
	Path    string
	Model   interface{} // reserved for future native PEFT bindings
	ID      string      // registry id, e.g. "code-lfm2.5-8b-v2"
	Version int         // parsed from path suffix -vN (0 if unknown)
	Ready   bool        // true when PEFT artifacts exist on disk
}

// LFMLoader manages LFM2.5 model + adapter loading.
type LFMLoader struct {
	BaseModel string // "lfm2.5-8b" or "lfm2.5-1.2b"
	ModelPath string
	Adapters  map[string]*LoadedAdapter // domain -> best adapter
	Fallback  *LoadedAdapter            // "general" domain or first ready adapter
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

// LoadAllAdapters loads enabled adapters matching the base model. Highest
// -vN wins per domain. Incomplete adapter dirs are skipped. Fallback is set
// to "general" if present, else the first ready adapter by domain name.
func (l *LFMLoader) LoadAllAdapters(registry *AdapterRegistry) error {
	l.Adapters = make(map[string]*LoadedAdapter)
	l.Fallback = nil

	if registry == nil {
		return nil
	}

	for _, entry := range registry.Adapters {
		if !entry.Enabled {
			continue
		}
		if entry.Model != "" && l.BaseModel != "" && entry.Model != l.BaseModel {
			continue
		}

		adapter, err := l.loadAdapter(entry)
		if err != nil {
			l.logger.Warn("failed to load adapter", "id", entry.ID, "error", err)
			continue
		}
		if !adapter.Ready {
			l.logger.Warn("adapter artifacts missing; skipping",
				"id", entry.ID, "path", adapter.Path)
			continue
		}

		if existing, ok := l.Adapters[entry.Domain]; ok {
			if adapter.Version <= existing.Version {
				l.logger.Debug("skipping older adapter version",
					"id", entry.ID, "version", adapter.Version, "kept", existing.Version)
				continue
			}
		}
		l.Adapters[entry.Domain] = adapter
		l.logger.Info("loaded adapter",
			"id", entry.ID,
			"domain", entry.Domain,
			"version", adapter.Version,
			"path", adapter.Path,
		)
	}

	if gen, ok := l.Adapters["general"]; ok {
		l.Fallback = gen
	} else {
		var domains []string
		for d := range l.Adapters {
			domains = append(domains, d)
		}
		if len(domains) > 0 {
			first := domains[0]
			for _, d := range domains[1:] {
				if d < first {
					first = d
				}
			}
			l.Fallback = l.Adapters[first]
		}
	}

	return nil
}

func (l *LFMLoader) loadAdapter(entry AdapterEntry) (*LoadedAdapter, error) {
	path := entry.Path
	if path == "" {
		return nil, fmt.Errorf("llm: adapter path is empty")
	}
	clean := filepath.Clean(path)
	info, err := os.Stat(clean)
	if err != nil {
		if os.IsNotExist(err) {
			return &LoadedAdapter{
				Domain:  entry.Domain,
				Path:    clean,
				ID:      entry.ID,
				Version: parseAdapterVersion(clean, entry.ID),
				Ready:   false,
			}, nil
		}
		return nil, fmt.Errorf("llm: stat adapter path %s: %w", clean, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("llm: adapter path is not a directory: %s", clean)
	}

	return &LoadedAdapter{
		Domain:  entry.Domain,
		Path:    clean,
		ID:      entry.ID,
		Version: parseAdapterVersion(clean, entry.ID),
		Ready:   hasAdapterArtifacts(clean),
		Model:   nil,
	}, nil
}

func hasAdapterArtifacts(dir string) bool {
	for _, name := range adapterArtifactNames {
		if st, err := os.Stat(filepath.Join(dir, name)); err == nil && !st.IsDir() {
			return true
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := strings.ToLower(e.Name())
		if strings.HasSuffix(n, ".safetensors") || strings.HasSuffix(n, ".bin") {
			return true
		}
	}
	return false
}

func parseAdapterVersion(path, id string) int {
	base := filepath.Base(path)
	if m := versionSuffix.FindStringSubmatch(base); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	if m := versionSuffix.FindStringSubmatch(id); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}
