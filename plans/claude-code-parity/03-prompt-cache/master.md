# Tree 03: Provider-Side Prompt Cache Architecture

## Goal

Add provider-side prompt caching (Anthropic cache_control markers + OpenAI-compatible prefix stability) with a static/dynamic boundary in the prompt builder. Reduces input token costs 60-90% on cache hits for long system prompts.

## Architecture Overview

Two adapter paths:
1. **Anthropic adapter** (`internal/llm/anthropic.go`): Insert `cache_control: {"type": "ephemeral"}` markers on system prompt blocks. Split system prompt at the boundary marker into static (cacheable) and dynamic (per-session) blocks.
2. **OpenAI-compatible adapter** (`internal/llm/client.go` or generic adapter): Rely on prefix stability. The boundary marker ensures the stable prefix doesn't change between turns. OpenAI-compatible APIs (OpenRouter, vLLM, llama.cpp) cache automatically when the prefix is identical.

The prompt builder (`internal/agent/prompt/` or `internal/context/`) inserts a `PromptCacheBoundary` marker between static sections (constitution, capabilities, anti-patterns, tool definitions) and dynamic sections (memory, task context, session-specific guidance).

## Interface Contracts

See SHARED-CONVENTIONS.md §6 for `PromptCacheBoundary` and `CacheScope` definitions.

### System Prompt Block Structure (Anthropic)
```go
type SystemPromptBlock struct {
    Text       string     `json:"text"`
    CacheScope CacheScope `json:"-"` // internal, not serialized
}

// Converted to Anthropic API format:
// Static blocks get: "cache_control": {"type": "ephemeral"}
// Dynamic blocks get: no cache_control
```

### Prefix Hash (OpenAI-compatible)
```go
// The cache key builder hashes the static prefix separately:
// key = SHA256(staticPrefix) + ":" + SHA256(dynamicSuffix)
// This allows the local response cache to hit on static prefix
// even when dynamic content changes.
```

## Child Index

| # | Leaf | Est. Context | Dependencies | Files Touched |
|---|------|-------------|--------------|---------------|
| 01 | boundary-and-builder | 70K | none | ~8 files |
| 02 | adapter-integration | 75K | 01 | ~6 files |

## Dispatch Protocol

1. Dispatch leaf 01 first (defines boundary marker, updates prompt builder).
2. After 01 reviews, dispatch leaf 02 (adapter integration).
3. Review in-session. Commit after review.

## Review Checklist

- [ ] PromptCacheBoundary marker inserted between static and dynamic sections
- [ ] Static sections are stable across turns (no timestamps, session IDs, or changing content)
- [ ] Anthropic adapter adds cache_control to static blocks
- [ ] OpenAI-compatible path maintains prefix stability
- [ ] Cache key builder hashes static prefix separately
- [ ] memorySnapshot freeze (existing) still works with new boundary
- [ ] No cache fragmentation (session-variant content is AFTER boundary)
- [ ] No debug artifacts, no TODOs, no placeholder values

## Coding Conventions

See SHARED-CONVENTIONS.md §2-§3.

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| 01-boundary-and-builder | COMPLETE | PromptCacheBoundary, CacheScope, prefix-aware key |
| 02-adapter-integration | COMPLETE | Anthropic cache_control, boundary stripping |

## Integration Test Plan

1. `go build ./internal/llm/... ./internal/agent/...`
2. `go test ./internal/llm/... -race -run TestCache`
3. Verify boundary marker appears in constructed system prompts
4. Verify Anthropic API request includes cache_control on static blocks
5. Verify cache key builder produces stable prefix hash across turns

---

# Leaf 03-01: Boundary Marker + Prompt Builder Integration

## DISPATCH INSTRUCTION
Implement all tasks below. Do NOT commit. Do NOT run git add. Write code, run tests, report results only. See SHARED-CONVENTIONS.md for coding standards.

**Parent:** 03-prompt-cache/orchestrator.md
**Scope:** Define the PromptCacheBoundary marker, classify prompt sections as static/dynamic, update the prompt builder to insert the boundary, and update the cache key builder for prefix-aware hashing.
**Dependencies:** None
**Estimated Context:** ~70K

## Interface Contract

This leaf exposes:
- `PromptCacheBoundary` constant
- `CacheScope` type and constants
- `ClassifyPromptSections(sections []string) (static []string, dynamic []string)`
- Updated prompt builder that inserts boundary
- Updated `CacheKeyBuilder` with prefix-aware hashing

## Tasks

### Task 1: Define boundary marker and cache scope types

**File:** `internal/llm/prompt_cache.go` (new)

```go
package llm

// PromptCacheBoundary separates static (cross-session cacheable) content
// from dynamic (per-session) content in the system prompt.
// Everything BEFORE this marker is stable across turns and sessions.
// Everything AFTER contains session-specific content (memory, task context).
//
// WARNING: Do not remove or reorder this marker without updating cache logic
// in the Anthropic adapter and cache key builder.
const PromptCacheBoundary = "__MEEPT_PROMPT_CACHE_BOUNDARY__"

// CacheScope indicates how a prompt section should be cached.
type CacheScope int

const (
    // CacheScopeNone — no caching (attribution headers, billing)
    CacheScopeNone CacheScope = iota
    // CacheScopeStatic — stable across sessions (constitution, capabilities, tools)
    CacheScopeStatic
    // CacheScopeSession — stable within a session (memory snapshot, task context)
    CacheScopeSession
)

// SystemPromptBlock is a section of the system prompt with cache metadata.
type SystemPromptBlock struct {
    Text       string
    CacheScope CacheScope
}
```

### Task 2: Classify prompt sections

**File:** `internal/llm/prompt_cache.go` (continued)

```go
// ClassifyPromptSections splits a system prompt into static and dynamic
// sections based on the PromptCacheBoundary marker.
func ClassifyPromptSections(sections []string) (static []string, dynamic []string) {
    boundaryFound := false
    for _, s := range sections {
        if s == PromptCacheBoundary {
            boundaryFound = true
            continue
        }
        if s == "" {
            continue
        }
        if !boundaryFound {
            static = append(static, s)
        } else {
            dynamic = append(dynamic, s)
        }
    }
    return static, dynamic
}

// BuildSystemPromptBlocks converts classified sections into cache-annotated blocks.
func BuildSystemPromptBlocks(sections []string) []SystemPromptBlock {
    static, dynamic := ClassifyPromptSections(sections)

    var blocks []SystemPromptBlock
    if len(static) > 0 {
        blocks = append(blocks, SystemPromptBlock{
            Text:       strings.Join(static, "\n\n"),
            CacheScope: CacheScopeStatic,
        })
    }
    if len(dynamic) > 0 {
        blocks = append(blocks, SystemPromptBlock{
            Text:       strings.Join(dynamic, "\n\n"),
            CacheScope: CacheScopeSession,
        })
    }
    return blocks
}
```

### Task 3: Insert boundary in prompt builder

**File:** `internal/agent/prompt/builder.go` or `internal/context/context_builder.go`

Read the existing prompt assembly code. Find where system prompt sections are concatenated. Insert `PromptCacheBoundary` between static and dynamic sections.

Static sections (before boundary):
- Constitution / baseline capabilities
- Baseline guidelines
- Anti-patterns (from Tree 02)
- Tool usage guidelines
- Evidence requirements
- Tool definitions / schemas

Dynamic sections (after boundary):
- Memory context (memorySnapshot)
- Task context
- Session-specific guidance
- Active context (skills, standing instructions)
- MCP instructions

The exact insertion point depends on how the builder assembles sections. Read the code to find the right place. The key requirement: everything that changes between sessions or turns goes AFTER the boundary.

### Task 4: Update cache key builder for prefix-aware hashing

**File:** `internal/llm/cache_key_builder.go`

Read the existing `CacheKeyBuilder`. Currently it hashes the entire message array as one blob. Update to hash the static prefix separately:

```go
// BuildPrefixAwareKey creates a cache key that separates the stable system
// prompt prefix from dynamic content. This allows cache hits when only
// dynamic content changes (e.g., new user message, updated memory).
func (b *CacheKeyBuilder) BuildPrefixAwareKey(modelID string, systemPrompt []string, messages []Message) string {
    static, dynamic := ClassifyPromptSections(systemPrompt)

    staticHash := sha256Hex(strings.Join(static, "\n\n"))
    dynamicHash := sha256Hex(strings.Join(dynamic, "\n\n"))
    messagesHash := b.hashMessages(messages)

    return fmt.Sprintf("%s:%s:%s:%s", modelID, staticHash[:16], dynamicHash[:16], messagesHash[:16])
}
```

Keep the existing `BuildKey` method for backward compatibility. Add `BuildPrefixAwareKey` as the new default, with a config flag to fall back to the old behavior.

### Task 5: Ensure memorySnapshot stability

**File:** Wherever `memorySnapshot` is frozen (likely `internal/agent/conversation.go` or similar)

Read the existing memorySnapshot freeze logic. Verify it's compatible with the boundary marker — the snapshot should be in the dynamic section (after boundary), and it should be frozen at session start so it doesn't change between turns.

If the snapshot is currently in the static section, move it after the boundary.

### Task 6: Tests

**File:** `internal/llm/prompt_cache_test.go` (new)

- `TestClassifyPromptSections` — static before boundary, dynamic after
- `TestClassifyPromptSectionsNoBoundary` — all static if no boundary present
- `TestBuildSystemPromptBlocks` — correct block structure
- `TestBuildPrefixAwareKey` — stable prefix hash across turns with same static content
- `TestBuildPrefixAwareKeyDynamicChange` — different key when dynamic content changes
- `TestBuildPrefixAwareKeyStaticChange` — different key when static content changes

## Self-Verification Checklist

- [ ] `go build ./internal/llm/... ./internal/agent/...` compiles
- [ ] `go test ./internal/llm/... -race -run TestPrompt` passes
- [ ] Boundary marker appears in constructed system prompts
- [ ] Static sections are truly stable (no timestamps, session IDs)
- [ ] Cache key builder produces stable prefix hash
- [ ] Backward compatibility: old BuildKey still works
- [ ] No unused imports or functions

## Review Checklist (for orchestrator)

- [ ] PromptCacheBoundary is a unique string unlikely to appear in prompt content
- [ ] Classification correctly handles: no boundary, empty sections, boundary at start/end
- [ ] Static sections don't include any per-session or per-turn content
- [ ] memorySnapshot is in dynamic section (after boundary)
- [ ] Cache key builder backward compatible
- [ ] No debug artifacts, no TODOs, no placeholder values

---

# Leaf 03-02: Adapter Integration (Anthropic + OpenAI-Compatible)

## DISPATCH INSTRUCTION
Implement all tasks below. Do NOT commit. Do NOT run git add. Write code, run tests, report results only. See SHARED-CONVENTIONS.md for coding standards.

**Parent:** 03-prompt-cache/orchestrator.md
**Scope:** Wire prompt cache blocks into the Anthropic adapter (cache_control markers) and ensure OpenAI-compatible adapter maintains prefix stability.
**Dependencies:** Leaf 01 (PromptCacheBoundary, SystemPromptBlock, ClassifyPromptSections)
**Estimated Context:** ~75K

## Interface Contract

This leaf exposes:
- Anthropic adapter sends `cache_control` on static system prompt blocks
- OpenAI-compatible adapter sends system prompt as a single string with stable prefix
- Both adapters use `BuildSystemPromptBlocks()` from leaf 01

## Tasks

### Task 1: Anthropic adapter — cache_control on system prompt

**File:** `internal/llm/anthropic.go`

Read the existing Anthropic API request construction. Find where the system prompt is set. Update to use `BuildSystemPromptBlocks()`:

```go
// In the request builder:
blocks := llm.BuildSystemPromptBlocks(systemPromptSections)
var systemBlocks []anthropic.TextBlockParam
for _, block := range blocks {
    param := anthropic.TextBlockParam{
        Type: "text",
        Text: block.Text,
    }
    if block.CacheScope == llm.CacheScopeStatic {
        param.CacheControl = &anthropic.CacheControlEphemeral{
            Type: "ephemeral",
        }
    }
    systemBlocks = append(systemBlocks, param)
}
req.System = systemBlocks
```

The exact types depend on the Anthropic SDK version in go.mod. Read the SDK to find the correct type names. The key: static blocks get `cache_control: {"type": "ephemeral"}`, dynamic blocks don't.

### Task 2: Anthropic adapter — handle cache metrics

**File:** `internal/llm/anthropic.go` or `internal/llm/metrics/`

Read the Anthropic API response. It includes `usage.cache_creation_input_tokens` and `usage.cache_read_input_tokens`. Log these metrics:

```go
if resp.Usage.CacheCreationInputTokens > 0 || resp.Usage.CacheReadInputTokens > 0 {
    slog.Debug("prompt cache",
        "cache_creation", resp.Usage.CacheCreationInputTokens,
        "cache_read", resp.Usage.CacheReadInputTokens,
        "input", resp.Usage.InputTokens,
    )
}
```

If the metrics store (`internal/llm/metrics/`) has a suitable counter, increment it.

### Task 3: OpenAI-compatible adapter — prefix stability

**File:** `internal/llm/client.go` or the generic/OpenAI adapter

Read the existing OpenAI-compatible request construction. The system prompt is typically sent as a single string in the messages array. Ensure:

1. The system prompt string is constructed by joining static sections first, then dynamic sections — so the prefix is stable.
2. The boundary marker is STRIPPED before sending (it's an internal marker, not prompt content).
3. The `memorySnapshot` freeze (existing) ensures the dynamic portion is stable within a session.

```go
// Strip boundary marker before sending to API
systemPrompt := strings.ReplaceAll(fullSystemPrompt, llm.PromptCacheBoundary+"\n\n", "")
```

No explicit cache_control needed — OpenAI-compatible APIs cache automatically when the prefix is identical.

### Task 4: Config flag for prompt caching

**File:** `internal/llm/` config or `internal/config/`

Add a config flag to enable/disable prompt caching:

```go
type PromptCacheConfig struct {
    Enabled bool `json:"enabled"` // default true
}
```

When disabled, the adapters send the system prompt as a single block without cache_control (backward compatible behavior).

Wire into `meept.json5`:
```json5
{
  "llm": {
    "prompt_cache": {
      "enabled": true
    }
  }
}
```

### Task 5: Tests

**File:** `internal/llm/anthropic_cache_test.go` (new)

- `TestAnthropicCacheControl` — static blocks get cache_control, dynamic don't
- `TestAnthropicCacheControlDisabled` — no cache_control when config disabled
- `TestBoundaryStripped` — boundary marker not sent to API
- `TestPrefixStability` — same static content produces same prefix across calls

## Self-Verification Checklist

- [ ] `go build ./internal/llm/...` compiles
- [ ] `go test ./internal/llm/... -race -run TestCache|TestAnthropic|TestPrefix"` passes
- [ ] Anthropic request includes cache_control on static blocks
- [ ] Boundary marker stripped from API requests
- [ ] Config flag enables/disables caching
- [ ] Cache metrics logged
- [ ] No unused imports or functions

## Review Checklist (for orchestrator)

- [ ] Anthropic SDK types match go.mod version
- [ ] cache_control only on CacheScopeStatic blocks
- [ ] Boundary marker never appears in API requests
- [ ] OpenAI-compatible path doesn't break (no cache_control, just stable prefix)
- [ ] Config flag defaults to enabled
- [ ] Backward compatible when disabled
- [ ] No debug artifacts, no TODOs, no placeholder values
