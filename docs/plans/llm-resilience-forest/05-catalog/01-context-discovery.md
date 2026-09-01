# Context-Length Discovery - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** ContextDiscovery syncer: per-provider fetchers (Ollama,
  OpenRouter, llama.cpp local endpoint), precedence-ruled merge,
  config, cadence.
- **Dependencies:** none
- **Estimated Context:** 70K
- **Concurrency Group:** A
- **Decision references:** D13

## Goal

`internal/llm/context_discovery.go`: a syncer in the PricingSyncer mold
(internal/llm/pricing_sync.go — struct + per-provider Fetch* + interval;
READ that file's shape first and mirror it) that fills ModelConfig
.ContextWindow from provider endpoints:

| Provider | Endpoint | Field |
|----------|----------|-------|
| ollama | GET {base}/api/show {"model": id} | `context_length` (or model_info.*) |
| openrouter | GET https://openrouter.ai/api/v1/models | entries[].context_length (same fetch as pricing_sync FetchOpenRouter — REUSE its client/parse where possible) |
| llama.cpp (local-models) | GET {base}/props | default n_ctx (`default_generation_settings.n_ctx`) |

Merge precedence (master Contract 3 — TEST IT):
1. models.json5 explicit context_limit wins ALWAYS (the json5 key is
   `context_limit` on ModelDef, providers.go:51 — there is NO
   `context_window` key; ContextWindow exists only on ModelCatalogEntry
   in the static catalog, models_catalog.go:8; ModelConfig's consumer
   field is ContextLimit, models.go:220).
2. Discovered value wins when the catalog/config value is 0/absent.
3. Discovered value wins over a non-zero catalog value ONLY when
   `allow_context_override: true`.
Deltas logged (slog.Info "context window updated", provider, model,
from, to). Never returns hard error on one provider's failure — logs
and continues (mirror PricingSyncer's error tolerance).

## Context

Key files:
- `internal/llm/pricing_sync.go` — the syncer pattern, OpenRouter
  response struct (line ~62), FetchOpenRouter (line 74), the HTTP
  client injection + interval handling.
- `internal/llm/models_catalog.go` — ModelCatalogEntry.ContextWindow
  (line 8) and per-provider entries; GetModelsForProvider (line 246).
- `internal/llm/models.go:220` — the per-model config consumer field is `ContextLimit` (NOT ContextWindow — audit 2026-09-01). Catalog entries use ContextWindow; ModelConfig uses ContextLimit. The merge writes the discovered value into ContextLimit; leaf-02 docs must use the right name per surface.
- `internal/llm/register_local_model.go` — how local endpoints
  (llama-cpp:127.0.0.1) register; the props fetch needs the endpoint
  base URL the same way.
- Where fetched values must LAND (⚠️ audit R3 — READ CAREFULLY): there
  is NO mutable "registry the resolver reads ContextWindow from".
  `Resolver.allModels` is built once at construction (resolver.go:61);
  alias/default/small models are independent copies created per call
  via `ResolveModelRef`→`modelConfigFrom` (providers.go:316-343,365+).
  The plan's original trace target, model_picker.go:37, reads the
  STATIC DISPLAY catalog (ModelCatalogEntry.ContextWindow,
  models_catalog.go:8) — the TUI picker, not runtime. The real runtime
  consumer is `ModelConfig.ContextLimit` (models.go:220) in the context
  firewall (context_firewall.go:353 etc.). And the PricingSyncer does
  NOT enrich runtime models — it writes only its own cache
  (pricing_sync.go:161-169); enrichment into resolver models happens
  once at wiring time (resolver.go:873-905, components.go:668).
  THEREFORE: the merge must call a NEW `Resolver.SetContextLimits`
  method that mutates the resolver's model pointer sets under `r.mu`
  (the `SetPricingSyncer` wiring precedent) so per-call
  `modelConfigFrom` copies pick up fresh values; discovery also refresh
  the display catalog so the TUI picker agrees. Trace
  context_firewall.go:353 (not model_picker.go:37) as the consumer
  test.

## Interface Contracts (From Parent)

### What This Leaf Exposes

Exactly master Contract 1:

```go
// internal/llm/context_discovery.go
type ContextDiscoveryConfig struct {
    Enabled              bool
    Interval             time.Duration
    AllowContextOverride bool
}
type ContextDiscovery struct{ /* cfg, http client, now, fetchers map[string]Fetcher */ }
type Fetcher func(ctx context.Context, baseURL, apiKey string) (map[string]int, error)
func NewContextDiscovery(cfg ContextDiscoveryConfig, client *http.Client) *ContextDiscovery
func (d *ContextDiscovery) RegisterFetcher(providerID string, f Fetcher)
func (d *ContextDiscovery) Sync(ctx context.Context) error
func (d *ContextDiscovery) Start(ctx context.Context) // ticker loop; stop via ctx
```

Keys are "provider/model" ids matching registry ids. BUILT-IN
registrations: ollama, openrouter, local-models (the llama.cpp
local-models endpoint — key fetcher results by provider id
`local-models`, LocalModelsProviderID, register_local_model.go:14;
"llama-cpp" is NOT a registry provider id). NO OpenAI or
Anthropic fetcher (D13 — document in code comment).
llama.cpp /props returns ONE default n_ctx per server while multiple
GGUFs merge into one endpoint — apply it as the default for every
`local-models/*` model id lacking an explicit value; the base URL comes
from RuntimeManager endpoint resolution, not provider config.

### What This Leaf Consumes

```go
// Existing: ModelConfig.ContextLimit (runtime consumer), catalog
// ContextWindow (display), PricingSyncer patterns, NEW
// Resolver.SetContextLimits (audit R3)
```

## Tasks

### Task 1: Fetchers + parsers

**Objective:** Three fetchers against fixture servers.

**Files:**
- Create: `internal/llm/context_discovery.go` (fetcher section)
- Test: `internal/llm/context_discovery_test.go` (httptest fixtures per
  provider — mirror pricing_sync_test.go fixture style; locate it first)

**Step 1:** Failing tests: Ollama /api/show JSON (context_length int;
model_info nested variant) → map; OpenRouter entries[].context_length →
map; llama.cpp /props n_ctx → map keyed by the registered local model
id; malformed JSON → empty map + logged warning, no error.
**Step 2:** FAIL. **Step 3:** Implement parsers (stdlib encoding/json;
two-value assertions; no interface{} panics). **Step 4:** PASS.

### Task 2: Precedence merge

**Objective:** The rule from Contract 3, exhaustively tested.

**Files:**
- Modify: `internal/llm/context_discovery.go` (merge section)
- Test: `internal/llm/context_discovery_test.go`

**Step 1:** Failing table tests covering ALL six cells: (explicit
config × discovered, catalog-only × discovered, 0 × discovered) ×
(override off/on). Explicit config wins in every override mode;
catalog 0 accepts discovery always; non-zero catalog only under
override. **Step 2:** FAIL. **Step 3:** Implement merge into the model
registry write path traced in Context. **Step 4:** PASS.

### Task 3: Config + wiring

**Objective:** llm.context_discovery config; daemon constructs + starts
the syncer when enabled.

**Files:**
- Modify: `internal/config/schema.go` (ContextDiscoveryConfig in the
  llm block; defaults: enabled=false, interval=6h, override=false) +
  defaults site
- Modify: internal/daemon/ components wiring (next to PricingSyncer
  construction — locate it; mirror start/stop lifecycle)
- Test: `internal/config/schema_test.go` (defaults + parse);
  wiring smoke via `go build ./...` + existing daemon tests

**Verify:** disabled default → nothing constructed, zero network.
`go test ./internal/llm/... ./internal/config/... -count=1`.

## Self-Verification Checklist

- [ ] Three fetchers fixture-tested; malformed responses tolerated
- [ ] All six precedence cells tested
- [ ] Default OFF; no network when off (tested by absence of client calls)
- [ ] No OpenAI/Anthropic fetcher (D13 comment present)
- [ ] Delta logging on real changes
- [ ] gofmt/vet/analyzers clean

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Contract matches master Contract 1 exactly (RegisterFetcher signature pinned for leaf 03)
- [ ] Pricing sync untouched (its tests green)
- [ ] Write path confirmed: Resolver.SetContextLimits mutates model pointer sets under r.mu; context_firewall.go:353 reads updated ContextLimit (tested)

Output: APPROVED or specific gaps with file + line references.

## Notes

- Rate limits: interval default 6h is deliberately slow; the OpenRouter
  fetcher shares the pricing sync's politeness (single fetch per tick).
- If the resolver reads ContextWindow from a cached snapshot rather
  than the registry, the merge must invalidate/refresh that cache the
  same way config reloads do — trace it, do not assume.
