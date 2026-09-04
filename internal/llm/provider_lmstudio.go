package llm

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
)

// LM Studio discovery client (llm-resilience-forest tree 05, leaf 03).
//
// LM Studio exposes TWO disjoint endpoints:
//
//   - GET {base}/v1/models — the standard OpenAI list shape
//     ({"data":[{"id": ...}]}) with NO context metadata at all.
//   - GET {base}/api/v0/models — LM Studio's REST v0 layer, where entries
//     carry `max_context_length` (TOP-LEVEL on the entry; a
//     `context_length` json tag would silently parse zeros) and, when a
//     model is loaded, `loaded_instances[].config.context_length`.
//
// Discovery merges the two by id and prefers a loaded instance's
// context_length over max_context_length. Every failure mode — server
// down, non-2xx, malformed JSON — degrades to an empty/partial result
// with a logged warning, never an error (mirrors leaf 01's
// getJSONTolerant contract; discovery must never break the caller).
//
// Map keys are "lmstudio/<sanitized-id>", matching the resolver's
// "provider/model" registry ids and D13's precedence handling.

// lmStudioModelsResponse models the OpenAI-compat /v1/models list.
type lmStudioModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// lmStudioV0Config models the per-instance generation config block.
type lmStudioV0Config struct {
	ContextLength int `json:"context_length"`
}

// lmStudioV0Instance is one loaded instance of a model.
type lmStudioV0Instance struct {
	Config lmStudioV0Config `json:"config"`
}

// lmStudioV0Entry is one /api/v0/models entry. Note the json tag is
// `max_context_length` — TOP-LEVEL on the entry.
type lmStudioV0Entry struct {
	ID               string               `json:"id"`
	MaxContextLength int                  `json:"max_context_length"`
	LoadedInstances  []lmStudioV0Instance `json:"loaded_instances"`
}

// lmStudioV0Response models the /api/v0/models payload.
type lmStudioV0Response struct {
	Data []lmStudioV0Entry `json:"data"`
}

// FetchLMStudioContexts discovers LM Studio models and their context
// lengths. The apiKey parameter is accepted to satisfy the ContextDiscovery
// Fetcher closure shape (context_discovery.go) — LM Studio serves without
// auth locally, so it is unused beyond leaf 01's shared getJSON helpers
// (a non-empty value simply adds a Bearer header, matching Ollama's
// fetcher).
//
// Failures are tolerated, not surfaced:
//   - /v1/models unreachable/non-2xx/malformed → empty result + warning.
//   - /api/v0/models unreachable/non-2xx/malformed → ids kept, context 0.
//   - Hostile ids (see sanitizeLMStudioModelID) skipped + logged.
func FetchLMStudioContexts(ctx context.Context, client *http.Client, logger *slog.Logger, baseURL, apiKey string) (map[string]int, error) {
	if logger == nil {
		logger = slog.Default()
	}
	out := make(map[string]int)
	if baseURL == "" {
		return out, nil
	}

	// Endpoint 1: the OpenAI-compat list defines WHICH models exist.
	// Malformed JSON is tolerated (jsonDecodeError → empty + warning);
	// transport/status failures surface as errors, which the caller
	// treats as skip (leaf 01's Sync logs and continues) — but this
	// fetcher converts them to empty-and-nil so direct daemon use can
	// never fail either.
	listURL := strings.TrimSuffix(baseURL, "/") + "/v1/models"
	list, err := getJSONTolerant[lmStudioModelsResponse](ctx, client, logger, listURL, apiKey)
	if err != nil {
		logger.Warn("context discovery: lm_studio /v1/models fetch failed",
			"base_url", baseURL, "error", err)
		return out, nil
	}
	if len(list.Data) == 0 {
		return out, nil
	}

	// Endpoint 2: the REST v0 layer carries context metadata. A total
	// failure here degrades to context 0 for every id (partial-tolerance
	// is stronger than the leaf requires: it says tolerate absence).
	v0URL := strings.TrimSuffix(baseURL, "/") + "/api/v0/models"
	v0, err := getJSONTolerant[lmStudioV0Response](ctx, client, logger, v0URL, apiKey)
	if err != nil {
		logger.Warn("context discovery: lm_studio /api/v0/models fetch failed; contexts unknown",
			"base_url", baseURL, "error", err)
		v0 = lmStudioV0Response{}
	}

	meta := make(map[string]lmStudioV0Entry, len(v0.Data))
	for _, e := range v0.Data {
		meta[e.ID] = e
	}

	for _, m := range list.Data {
		id := sanitizeLMStudioModelID(m.ID)
		if id == "" {
			// Hostile id (empty, traversal, whitespace, control chars,
			// or charset violation after lowercasing): skip + log,
			// never register.
			logger.Warn("context discovery: skipping hostile LM Studio model id",
				"id", m.ID)
			continue
		}
		key := ProviderIDLMStudio + "/" + id
		if ctxLen := lmStudioContextFromEntry(meta[m.ID]); ctxLen > 0 {
			out[key] = ctxLen
		} else {
			// Presence with 0 = "discovered, context unknown" (D13's
			// catalog-fallback precedence then applies downstream).
			out[key] = 0
		}
	}
	return out, nil
}

// lmStudioContextFromEntry applies the merge rule for one v0 entry: a
// loaded instance's context_length wins over max_context_length when both
// exist; absence of either yields 0.
func lmStudioContextFromEntry(e lmStudioV0Entry) int {
	for _, inst := range e.LoadedInstances {
		if inst.Config.ContextLength > 0 {
			return inst.Config.ContextLength
		}
	}
	return e.MaxContextLength
}

// NewLMStudioFetcher returns a ContextDiscovery Fetcher (leaf 01's
// registered shape: func(ctx, baseURL, apiKey) (map[string]int, error))
// bound to the given HTTP client and logger. The daemon wiring (orchestrator
// task, NOT this leaf — components.go is owned elsewhere) registers it via
// ContextDiscovery.RegisterFetcher(ProviderIDLMStudio, NewLMStudioFetcher(...))
// after resolving the base URL from provider config, mirroring the
// ollama/openrouter/local-models registrations.
func NewLMStudioFetcher(client *http.Client, logger *slog.Logger) Fetcher {
	return func(ctx context.Context, baseURL, apiKey string) (map[string]int, error) {
		return FetchLMStudioContexts(ctx, client, logger, baseURL, apiKey)
	}
}

// sanitizeLMStudioModelID validates (and normalizes) a discovered model id
// for use as the "<provider>/<model>" registry key. This is NEW validation:
// register_local_model.go's localModelKey is only a rename transform
// (TrimSuffix .gguf + ReplaceAll / → --) — no charset or traversal checks
// exist anywhere in the local-model path. Rules:
//
//   - lowercase first (the OpenAI list can carry uppercase ids, e.g.
//     "Qwen2.5-7B-Instruct"; localModelKey's output charset is the target);
//   - reject empty, strings containing "../", whitespace, or control
//     characters;
//   - reject any character outside [a-z0-9-_._] — i.e. [a-z0-9-_.].
//
// Returns "" for rejected ids; callers skip + log.
func sanitizeLMStudioModelID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return ""
	}
	if strings.Contains(id, "../") {
		return ""
	}
	// "." and ".." pass the charset but are pure path segments: a key
	// like "lmstudio/.." climbs out of any path-aware consumer. Reject.
	if id == "." || id == ".." {
		return ""
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			// allowed
		default:
			return ""
		}
	}
	return id
}
