# Model Catalog: Context Discovery + LM Studio - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root of tree 05 in the llm-resilience-forest)
- **Children:** 3 leaf documents under this node
- **Scope:** Context-length discovery from provider endpoints (catalog
  values remain fallback) + LM Studio as a first-class provider.

Read `../SHARED-CONVENTIONS.md` and `../DECISIONS.md` (D12, D13).
Independent of trees 01-04. Uses only existing llm-package seams.

## Goal

Two catalog gaps:

1. **Context length (D13).** ContextWindow is hardcoded per model
   (internal/llm/models_catalog.go). Providers DO expose truth:
   - Ollama: `/api/show` (per-model, context_length)
   - llama.cpp server: `/props` (ONE default n_ctx per server — apply
     to all `local-models/*` ids lacking explicit values; no per-model
     context metadata exists in its /v1/models)
   - LM Studio: `/api/v0/models` `max_context_length` (REST v0; the
     OpenAI-compat /v1/models list carries NO context metadata)
   - OpenRouter: `/api/v1/models` `context_length` field (the pricing
     sync already hits this endpoint — internal/llm/pricing_sync.go:62-74)
   - OpenAI/Anthropic: NOT exposed — catalog stays the source.
   Deliver a discovery pass that runs per-provider at a configurable
   cadence, fills ContextWindow ONLY where the catalog value is 0/absent
   or a `allow_context_override` flag is set (catalog = fallback; remote
   truth wins when explicitly allowed), caches results, and logs
   deltas. No runtime behavior change when discovery is off.

2. **LM Studio (D12).** First-class provider, Ollama treatment: registry
   entry (provider_registry.go — Transport TransportOpenAIChat, BaseURL
   http://localhost:1234/v1, local support tag), model discovery from
   its OpenAI-compatible /v1/models (ids; context metadata from
   /api/v0/models — see the corrected endpoint line above), doc page.
   LM Studio's API is OpenAI-chat-compatible (streaming + tools in
   recent versions).

**Ollama-precedent correction (audit 2026-09-01):** Ollama in meept
today is registry entry + static catalog ONLY — it has NO discovery
call (zero hits for /api/show|/api/tags in internal/, apart from the
unrelated shadow adapter). The discovery machinery is NEW in this tree,
mirroring PricingSyncer (per the Architecture section). "Same treatment
as Ollama" (D12) means the registry + catalog SHAPE, not existing
discovery behavior.

## Architecture

Discovery mirrors PricingSyncer (pricing_sync.go): a syncer struct with
per-provider fetchers, injected HTTP client, interval config, and a
merge step that NEVER mutates config-file-declared values unless the
override flag allows it. LM Studio rides existing OpenAI-chat transport;
its discovery fetcher is the same code path as generic
OpenAI-compatible /v1/models parsing.

## Interface Contracts

### Contract 1: Discovery merge rule

```go
// internal/llm/context_discovery.go (new)
type ContextDiscoveryConfig struct {
    Enabled               bool          // default false
    Interval              time.Duration // default 6h
    AllowContextOverride  bool          // default false: catalog wins unless this is true
}
type ContextDiscovery struct{ /* fetchers by providerID, cfg, http client, now */ }
func NewContextDiscovery(cfg ContextDiscoveryConfig, client *http.Client) *ContextDiscovery
// Sync(ctx) error — per registered provider with a fetcher: fetch map
// ["provider/model"]int, merge per the rule, record deltas in the log.
// Fetchers registered for: ollama, lmstudio, openrouter (+ llama.cpp
// local-models endpoint where props are reachable).
```

- Owner: 01-context-discovery.md
- Consumers: 03-lmstudio-provider.md (registers its fetcher)

### Contract 2: LM Studio provider entry

```go
// internal/llm/provider_registry.go:
const ProviderIDLMStudio = "lmstudio"
// {ID: lmstudio, Name: "LM Studio", Transport: TransportOpenAIChat,
//  AuthType: AuthEnvVar with APIKeyEnvVar unset (the exact Ollama-mirror
//  pattern — provider_registry.go:109-117; there is NO AuthNone const,
//  AuthType values are AuthAPIKey/AuthOAuthDevice/AuthOAuthExternal/AuthEnvVar,
//  provider_registry.go:54-59), BaseURL: "http://localhost:1234/v1",
//  Supports: [CapStreaming, CapTools, "local"]}
// Discovery: /v1/models → model ids ("provider/model" = lmstudio/<id>);
// each discovered model registers with ContextWindow from discovery
// when exposed, else catalog/0.
```

- Owner: 03-lmstudio-provider.md
- Consumers: 01-context-discovery.md (fetcher registration order:
  leaf 03 depends on leaf 01's registration point OR defines its own
  fetcher and leaf 01 wires generic ones — see leaf notes)

### Contract 3: ContextWindow population rule (documentation)

```
Precedence (documented in docs/configuration/llm.md):
1. models.json5 explicit context_limit (authoritative when set)
2. discovered value (when allow_context_override OR catalog value is 0)
3. catalog default
```

- Owner: 02-docs-and-config.md
- Consumers: users

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-context-discovery.md | leaf | none | 70K | A |
| 02 | 02-docs-and-config.md | leaf | 01, 03 | 40K | C |
| 03 | 03-lmstudio-provider.md | leaf | none | 60K | A |

**Concurrency groups:** A = {01, 03} parallel EXCEPT the daemon wiring
site: leaf 01 Task 3 CREATES the ContextDiscovery wiring and leaf 03
Task 3 MODIFIES the same wiring file (it may not exist yet when 03
runs) — the wiring edit is the one shared file; if both leaves are in
flight, 03's wiring task waits for 01's merge (or 03 constructs its own
discovery instance beside 01's — one owner per file, named here).
C = {02} last.

## Dispatch Protocol

- Leaf 01: verify `go test ./internal/llm/ -run TestContextDiscovery -v`;
  httptest fixtures for each provider shape. Commit:
  `feat(llm): provider context-length discovery (tree 05 leaf 01)`.
- Leaf 03 (parallel with 01): verify provider registry + discovery
  tests; `make build` green. Commit:
  `feat(llm): LM Studio first-class provider (tree 05 leaf 03)`.
- Leaf 02: docs + config completeness; `make graphs` clean. Commit:
  `docs(llm): context discovery + LM Studio configuration (tree 05 leaf 02)`.
In-session review per leaf; max 3 re-dispatch cycles.

## Review Checklist

- [ ] Leaf tasks complete; tests pass; contracts satisfied
- [ ] Catalog values never silently overwritten (precedence rule tested)
- [ ] Discovery default OFF; zero behavior change when off
- [ ] LM Studio entry matches Ollama's shape exactly (same fields)
- [ ] No new deps without license note; gofmt/vet/analyzers clean
- [ ] Docs: precedence list, both provider docs pages

Output: APPROVED or specific gaps.

## Coding Conventions

Pass `../SHARED-CONVENTIONS.md` §1-§3. HTTP fixtures via httptest
(pricing_sync tests show the pattern — locate and mirror).

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-context-discovery.md | COMPLETE | 1 | dddb364e; APPROVED first review; SetContextLimits under r.mu (audit R3) |
| 02-docs-and-config.md | COMPLETE | 1 | 40e41b0e; keys verified against schema.go; precedence verbatim; no drift found |
| 03-lmstudio-provider.md | COMPLETE | 1 | 6f1a2e8a; commit gate paused run — orchestrator reviewed + committed; D12 discovery-only deviation recorded in DECISIONS.md |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./... && go test ./internal/llm/... -count=1`.
2. E2E: httptest server posing as LM Studio /v1/models → discovery
   populates ContextWindow for discovered models per the precedence
   rule (override off: catalog wins; catalog 0: discovered wins;
   override on: discovered wins).
3. Ollama fixture /api/show → same assertions.
4. OpenRouter sync still works (pricing sync untouched — run its tests).
5. `make analyzers`; `make graphs`; AGENTS.md review (new provider =
   Key Components impact check) in final commit.

## Structural Completeness Check (Before Dispatch)

`python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py docs/plans --strict-leaves | grep 05-catalog`

## Notes

- D12 DEVIATION TO RECORD (audit 2026-09-01): leaf 03's discovery-only
  catalog (no static LM Studio catalog entries) narrows D12's "registry
  entry, discovery, catalog" — defensible (loaded models vary), but per
  DECISIONS.md's footer rule the orchestrator must record it in
  DECISIONS.md as a dated deviation row when tree 05 completes.
- OpenAI/Anthropic expose no context length (D13) — a fetcher for them
  MUST NOT exist; the docs leaf states this so users know why their
  OpenAI models keep catalog values.
- LM Studio model ids contain the loaded-model name; VALIDATE them with
  NEW code (no existing id validation exists to reuse — localModelKey in
  register_local_model.go is only a rename transform; reject `../`,
  empty, and non-[a-z0-9-_.] chars) — a hostile local
  endpoint must not inject config keys.
