# LM Studio First-Class Provider - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** lmstudio provider registry entry + /v1/models discovery +
  model registration + docs page.
- **Dependencies:** none (parallel with 01; codes against 01's pinned
  RegisterFetcher signature — see Task 3; if 01 has not landed when
  this leaf runs, leave a TODO-free registration seam call behind a
  build-tag-free nil check and note it in Deviations)
- **Estimated Context:** 60K
- **Concurrency Group:** A
- **Decision references:** D12, D13

## Goal

LM Studio gets the Ollama treatment (D12):

1. **Registry entry** (internal/llm/provider_registry.go): id
   `lmstudio`, name "LM Studio", TransportOpenAIChat, AuthEnvVar with
   no APIKeyEnvVar set (the Ollama pattern — there is no AuthNone
   const),
   BaseURL http://localhost:1234/v1, Supports [CapStreaming, CapTools,
   "local"], DocURL https://lmstudio.ai/docs/api.
2. **Discovery**: GET {base}/v1/models → {"data":[{"id": "..."}]} —
   standard OpenAI list shape (NOTE: /v1/models carries NO context
   metadata at all). Each id registers as
   `lmstudio/<sanitized-id>` with ContextWindow from the entry's
   metadata when present (LM Studio exposes context length via its
   REST v0 layer, NOT the OpenAI-compat list: GET /api/v0/models
   entries carry `max_context_length` (top-level) and
   `loaded_instances[].config.context_length` (when loaded) — probe
   BOTH: parse /v1/models for ids; GET /api/v0/models for metadata
   (json tag `max_context_length` — a `context_length` tag would
   silently parse zeros) and merge by id; prefer a loaded instance's
   `context_length` over `max_context_length` when both exist;
   tolerate absence).
3. **Reasoning/tool translation**: confirm lmstudio lands in the same
   translation case as ollama/local in reasoning_translate.go:65 (the
   case list `ProviderIDOllama, "qwen", "local", ...` — ADD lmstudio
   if the default branch would mis-translate; test the choice).
4. **Docs**: provider setup page (endpoint config, discovery behavior,
   example models.json5 entry).

## Context

Key files:
- `internal/llm/provider_registry.go:100-120` — the Ollama entry to
  copy field-for-field; ProviderIDOllama constant at line 17.
- `internal/llm/models_catalog.go:87-101` — Ollama catalog entries
  (ProviderIDOllama map entries with ProviderID fields) — decide
  whether LM Studio ships catalog entries (loaded models vary; likely
  NO static catalog, discovery-only — state the decision + rationale
  in code; Ollama HAS catalog entries, but its model set is stable
  while LM Studio's is user-loaded. Discovery-only is the defensible
  default; document it).
- `internal/llm/reasoning_translate.go:65` — the translation case list.
- `internal/llm/register_local_model.go` — `localModelKey` (:86-89) is a
  TRANSFORM precedent (TrimSuffix .gguf + ReplaceAll / → --), NOT
  validation — there is NO id validation anywhere today (no charset
  check, no `../` rejection in models.go either). Write NEW validation;
  keep localModelKey's charset as the target.
- Docs: `docs/configuration/llm.md` — providers/models reference
  (one LM Studio section mirroring the Ollama section's shape).

## Interface Contracts (From Parent)

### What This Leaf Exposes

Exactly master Contract 2:

```go
// internal/llm/provider_registry.go
const ProviderIDLMStudio = "lmstudio"
// registry entry per Goal-1 (field-for-field Ollama shape)
// Discovery fetcher (wired when leaf 01's registration point exists):
//   Fetcher returning map["lmstudio/<id>"]contextLength (0 when unknown)
// Model registration: discovered ids → lmstudio/<sanitized-id>, kind
// openai-chat, streaming+tools per the registry entry.
```

### What This Leaf Consumes

```go
// From 01 (when landed): ContextDiscovery.RegisterFetcher(providerID, f)
// Existing: TransportOpenAIChat machinery (zero new transport code)
```

## Tasks

### Task 1: Registry entry + constant

**Objective:** First-class provider exists.

**Files:**
- Modify: `internal/llm/provider_registry.go`
- Test: `internal/llm/provider_registry_test.go` (entry present,
  fields correct, transport resolves)

**Step 1:** Failing test: registry lookup "lmstudio" returns the entry
with every field per Goal-1. **Step 2:** FAIL. **Step 3:** Add the
entry beside Ollama's. **Step 4:** PASS.

### Task 2: Model discovery + registration

**Objective:** Running LM Studio's models appear as lmstudio/<id>.

**Files:**
- Create: `internal/llm/provider_lmstudio.go` (discovery client)
- Test: `internal/llm/provider_lmstudio_test.go` (httptest fixtures:
  /v1/models list; /api/v0/models metadata; hostile ids rejected)

**Step 1:** Failing tests: list parse → ids; /api/v0 merge fills
context lengths where present; id sanitization (an id with
`../`/whitespace/control chars is skipped + logged — reuse/extend the
register_local_model validation); server down → empty result, no
error. **Step 2:** FAIL. **Step 3:** Implement. **Step 4:** PASS.

### Task 3: Discovery wiring + reasoning translation check

**Objective:** The fetcher registers with ContextDiscovery; translation
correct.

**Files:**
- Modify: the daemon wiring where ContextDiscovery constructs (leaf 01
  lands it) — register the LM Studio fetcher with the resolved base
  URL from provider config.
- Modify: `internal/llm/reasoning_translate.go` ONLY IF the test shows
  mis-translation (decide by test, not guess)
- Test: reasoning_translate_test.go case for lmstudio; wiring smoke.

**Step 1:** Failing tests: fetcher registered when provider enabled;
translation for a reasoning-enabled lmstudio model behaves as the
ollama/local class does. **Step 2:** FAIL. **Step 3:** Implement.
**Step 4:** PASS.

### Task 4: Docs

**Files:**
- Modify: `docs/configuration/llm.md` (the providers reference; no
  separate providers page exists) — LM Studio section:
  install/serve one-liner, base URL default, discovery behavior,
  models.json5 example entry (provider lmstudio, model id from the
  loader), context-length note (discovered when exposed, else 0 →
  tree 05 leaf 01's precedence).
- Modify: `AGENTS.md` Key Components ONLY if the package table needs
  it (it should not — no new package); provider addition noted if the
  providers list appears anywhere in AGENTS.md.

**Verify:** docs mirror the Ollama section's structure.

## Self-Verification Checklist

- [ ] Registry entry field-for-field matches Ollama's shape (tested)
- [ ] Discovery tolerant: server down / partial metadata / hostile ids
- [ ] Reasoning translation verified by test (changed or consciously unchanged, documented)
- [ ] No new deps
- [ ] gofmt/vet/analyzers clean; `go test ./internal/llm/... -count=1`

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Contract matches master Contract 2 exactly (constant name, entry fields)
- [ ] Discovery-only (no static catalog) decision documented in code
- [ ] Docs section complete and accurate

Output: APPROVED or specific gaps with file + line references.

## Notes

- LM Studio's OpenAI-compat layer serves /v1/chat/completions with
  streaming; tools support depends on the loaded model's JIT — the
  registry entry ADVERTISES CapTools (the endpoint accepts the wire
  format); per-model capability gating remains config's job. Note this
  nuance in the docs.
- Port 1234 is LM Studio's default; users change it — the provider
  BaseURL must be overridable the same way Ollama's is (verify the
  override mechanism when writing the docs task).
