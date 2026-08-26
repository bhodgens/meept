package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// LocalModelsProviderID is the synthetic provider alias that pulled local
// models register under.
const LocalModelsProviderID = "local-models"

// RegisterLocalModel wires a pulled GGUF into the runtime manager under the
// local-models provider. Repeated calls merge models into one shared
// llama-server endpoint config so a single spawned process serves them all.
// The model file must exist and be non-empty.
func (m *RuntimeManager) RegisterLocalModel(rec ModelRecord) error {
	if rec.File == "" {
		return fmt.Errorf("runtime: local model %q has empty file path", rec.Name)
	}
	st, err := os.Stat(rec.File)
	if err != nil {
		return fmt.Errorf("runtime: stat model file: %w", err)
	}
	if st.IsDir() {
		return fmt.Errorf("runtime: local model %q path is a directory", rec.File)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.inUseModels == nil {
		m.inUseModels = make(map[string]struct{})
	}

	cfg, ok := m.configs[LocalModelsProviderID]
	if ok {
		// Managed llama.cpp runtimes auto-declare the llamacpp grammar
		// constraint mode.
		if cfg.ToolConstraint == "" {
			cfg.ToolConstraint = ToolConstraintForRuntime(cfg.Type)
		}
	}
	if !ok {
		modelKey := localModelKey(rec.Name)
		cfg = &RuntimeConfig{
			Type:         RuntimeLlamaCpp,
			ModelPath:    rec.File,
			ModelPaths:   map[string]string{modelKey: rec.File},
			ModelKeys:    []string{modelKey},
			EndpointKey:  "llama-cpp:127.0.0.1",
			AutoStart:    true,
			SpawnCommand: llamaSpawnCommand(modelKey, rec.File),
			// Managed llama.cpp runtimes auto-declare the llamacpp grammar
			// constraint mode.
			ToolConstraint: ToolConstraintForRuntime(RuntimeLlamaCpp),
		}
		m.configs[LocalModelsProviderID] = cfg
		m.providerEndpoint[LocalModelsProviderID] = "llama-cpp:127.0.0.1"
		m.inUseModels[LocalModelsProviderID+"/"+modelKey] = struct{}{}
		slog.Debug("runtime: registered local-models endpoint",
			"provider", LocalModelsProviderID, "model", rec.Name, "path", rec.File)
		return nil
	}

	modelKey := localModelKey(rec.Name)
	if _, dup := cfg.ModelPaths[modelKey]; !dup {
		cfg.ModelPaths[modelKey] = rec.File
		cfg.ModelKeys = append(cfg.ModelKeys, modelKey)
	}
	m.inUseModels[LocalModelsProviderID+"/"+modelKey] = struct{}{}
	// Keep ModelPath pointing at the first registered model (spawn default).
	if cfg.ModelPath == "" {
		cfg.ModelPath = rec.File
	}
	slog.Debug("runtime: merged local model into existing endpoint",
		"provider", LocalModelsProviderID, "model", rec.Name)
	return nil
}

// localModelKey derives a stable model key from name/repo id:
// "org/repo-file.gguf" -> "org--repo-file".
func localModelKey(name string) string {
	key := strings.TrimSuffix(name, ".gguf")
	return strings.ReplaceAll(key, "/", "--")
}

// llamaSpawnCommand builds the llama-server spawn args for one model entry.
func llamaSpawnCommand(modelKey, modelPath string) []string {
	sum := sha256.Sum256([]byte(modelKey))
	tag := hex.EncodeToString(sum[:])[:8]
	return []string{
		"llama-server",
		"--model", modelPath,
		"--alias", "meept-" + tag,
	}
}
