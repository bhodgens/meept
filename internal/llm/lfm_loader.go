package llm

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

// versionSuffix matches trailing -vN on adapter directory names.
var versionSuffix = regexp.MustCompile(`-v(\d+)$`)

// runtimeLoRAField is the chat-body key mlx_lm.server reads to apply LoRA
// weights. Verified against mlx_lm/server.py: body.get("adapters") is passed
// to ModelProvider.load, which calls mlx_lm.utils.load(adapter_path=...) →
// tuner.utils.load_adapters → model.load_weights. Neither that server nor
// llama-server reads "adapter_path".
const runtimeLoRAField = "adapters"

// runtimeLoRAWeights is the tensor file load_adapters opens
// (adapter_path / "adapters.safetensors"). PEFT's adapter_model.safetensors
// is a different format and is not applied by this runtime.
const runtimeLoRAWeights = "adapters.safetensors"

// errAdapterWeightsMissing means the directory is not in the format the
// runtime applies. Callers treat it as not-ready, not as a hard failure.
var errAdapterWeightsMissing = errors.New("llm: adapter weights not in runtime format")

// AppliedLoRA is the weight handle stored on LoadedAdapter.Model after
// loadAdapter has read the tensors the runtime applies. Field is the
// chat-body key stampRuntimeAdapter writes so inference calls that path.
type AppliedLoRA struct {
	Dir     string
	Tensors []string
	Field   string
}

// LoadedAdapter represents a LoRA adapter bound to the runtime apply path.
// Ready is true only when Model holds an AppliedLoRA (weights were read).
// A directory that merely contains PEFT filenames is not ready.
type LoadedAdapter struct {
	Domain  string
	Path    string
	Model   any    // *AppliedLoRA when weights were read; nil otherwise
	ID      string // registry id, e.g. "code-lfm2.5-8b-v2"
	Version int    // parsed from path suffix -vN (0 if unknown)
	Ready   bool   // true only when Model is a loaded AppliedLoRA
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
		if !adapter.Ready || adapter.Model == nil {
			l.logger.Warn("adapter weights not applied; skipping",
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

	applied, err := applyRuntimeLoRA(clean)
	if err != nil {
		if errors.Is(err, errAdapterWeightsMissing) {
			return &LoadedAdapter{
				Domain:  entry.Domain,
				Path:    clean,
				ID:      entry.ID,
				Version: parseAdapterVersion(clean, entry.ID),
				Ready:   false,
				Model:   nil,
			}, nil
		}
		return nil, err
	}
	return &LoadedAdapter{
		Domain:  entry.Domain,
		Path:    clean,
		ID:      entry.ID,
		Version: parseAdapterVersion(clean, entry.ID),
		Ready:   true,
		Model:   applied,
	}, nil
}

// applyRuntimeLoRA reads the tensors mlx_lm.server applies when the chat
// body carries runtimeLoRAField. A missing or PEFT-only directory returns
// errAdapterWeightsMissing. A present but unreadable weight file is a hard
// error so a corrupt adapter is not reported as merely absent.
func applyRuntimeLoRA(dir string) (*AppliedLoRA, error) {
	if err := validateMLXAdapterConfig(dir); err != nil {
		return nil, err
	}
	weights := filepath.Join(dir, runtimeLoRAWeights)
	names, err := parseSafetensorsTensors(weights)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errAdapterWeightsMissing
		}
		return nil, fmt.Errorf("llm: read adapter weights %s: %w", weights, err)
	}
	return &AppliedLoRA{
		Dir:     dir,
		Tensors: names,
		Field:   runtimeLoRAField,
	}, nil
}

// stampRuntimeAdapter writes the chat-body field the runtime applies.
// Empty dir writes nothing. This is the inference-time half of
// applyRuntimeLoRA: the loader reads the tensors, the request names the
// directory so mlx_lm.server calls load_adapters.
func stampRuntimeAdapter(payload map[string]any, dir string) {
	if payload == nil || dir == "" {
		return
	}
	payload[runtimeLoRAField] = dir
}

func validateMLXAdapterConfig(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "adapter_config.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errAdapterWeightsMissing
		}
		return fmt.Errorf("llm: read adapter config: %w", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("llm: parse adapter config: %w", err)
	}
	ft, _ := cfg["fine_tune_type"].(string)
	if ft == "full" {
		return nil
	}
	if _, ok := cfg["num_layers"]; !ok {
		return errAdapterWeightsMissing
	}
	params, ok := cfg["lora_parameters"].(map[string]any)
	if !ok {
		return errAdapterWeightsMissing
	}
	for _, key := range []string{"rank", "scale", "dropout"} {
		if _, ok := params[key]; !ok {
			return errAdapterWeightsMissing
		}
	}
	return nil
}

// parseSafetensorsTensors reads a safetensors header and returns tensor
// names. It does not log tensor bytes. The header is an 8-byte little-endian
// length followed by JSON; data offsets must fall inside the file.
func parseSafetensorsTensors(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	var nbuf [8]byte
	if _, err := io.ReadFull(f, nbuf[:]); err != nil {
		return nil, fmt.Errorf("header length: %w", err)
	}
	n := binary.LittleEndian.Uint64(nbuf[:])
	const maxHeader = 32 << 20
	if n == 0 || n > maxHeader {
		return nil, fmt.Errorf("header length %d out of range", n)
	}
	if info.Size() < int64(n)+8 { //nolint:gosec // G115: n is clamped to maxHeader (32 MiB) above, so int64(n)+8 cannot overflow
		return nil, fmt.Errorf("truncated header")
	}
	hdr := make([]byte, n)
	if _, err := io.ReadFull(f, hdr); err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(hdr, &raw); err != nil {
		return nil, fmt.Errorf("header json: %w", err)
	}
	delete(raw, "__metadata__")
	if len(raw) == 0 {
		return nil, fmt.Errorf("no tensors")
	}
	names := make([]string, 0, len(raw))
	var maxEnd int64
	for name, msg := range raw {
		var spec struct {
			DataOffsets [2]int64 `json:"data_offsets"`
		}
		if err := json.Unmarshal(msg, &spec); err != nil {
			return nil, fmt.Errorf("tensor %s: %w", name, err)
		}
		if spec.DataOffsets[1] < spec.DataOffsets[0] {
			return nil, fmt.Errorf("tensor %s offsets inverted", name)
		}
		if spec.DataOffsets[1] > maxEnd {
			maxEnd = spec.DataOffsets[1]
		}
		names = append(names, name)
	}
	if 8+int64(n)+maxEnd > info.Size() {
		return nil, fmt.Errorf("truncated tensor data")
	}
	sort.Strings(names)
	return names, nil
}

// hasAdapterArtifacts reports whether dir holds weights the runtime applies.
// Filename presence is not enough: PEFT adapter_model.safetensors and a
// truncated adapters.safetensors both return false.
func hasAdapterArtifacts(dir string) bool {
	_, err := applyRuntimeLoRA(dir)
	return err == nil
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
