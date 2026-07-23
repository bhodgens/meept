---
name: master.md
description: Root orchestrator for config extraction plan
version: 1.0.0
author: Hermes Agent
license: MIT
metadata:
  hermes:
    tags: [configuration, hardcoded-values, defaults]
---

# Config Extraction Plan — Root Orchestrator

## Goal

Extract hardcoded configuration values into the config schema to improve deployment flexibility.

**Targets:**
- HTTP base URL: `https://localhost:8081` → configurable
- Ollama endpoint: `http://localhost:11434` → configurable  
- SQLite busy timeout: 5000ms → configurable
- Prompt truncation: 8000 chars → configurable
- TLS minimum version: TLS 1.2 → configurable

**Estimated effort**: ~1-2 hours (single leaf)

## Architecture Overview

Meept's configuration system uses a hierarchical JSON5 schema defined in `internal/config/schema.go`. Operational parameters are currently hardcoded as literals throughout the codebase, making deployment customization difficult. This plan extracts 5 critical operational parameters into the config schema with sensible defaults matching current behavior.

The config system supports:
- **Defaults**: Hardcoded Go constants that match current behavior
- **Overrides**: User-provided values in `~/.meept/meept.json5`
- **Validation**: Type-safe schema enforcement at load time
- **Propagation**: Options pattern for passing config to constructors

## Interface Contracts

### Exposed Configuration Fields

This leaf exposes new config fields in the following structs:

**SessionConfig** (`internal/config/schema.go`):
```go
type SessionConfig struct {
    SQLiteBusyTimeoutMs int `json:"sqlite_busy_timeout_ms" default:"5000"`
    SummaryPromptTruncationLimit int `json:"summary_prompt_truncation_limit" default:"8000"`
}
```

**EmbeddingConfig** (`internal/config/schema.go`):
```go
type EmbeddingConfig struct {
    OllamaBaseURL string `json:"ollama_base_url" default:"http://localhost:11434"`
    OllamaModel string `json:"ollama_model" default:"nomic-embed-text"`
    Dimension int `json:"dimension" default:"768"`
}
```

**Transport defaults** (`internal/transport/client.go`, `internal/memory/vector/embedding.go`):
- `DefaultHTTPBaseURL = "https://localhost:8081"`
- `DefaultOllamaBaseURL = "http://localhost:11434"`
- `DefaultOllamaModel = "nomic-embed-text"`

### Constructor Option Signatures

New options added to existing constructors:
- `session.WithBusyTimeoutMs(ms int)` — returns `StoreOption`
- `session.WithPromptTruncationLimit(limit int)` — returns `SummarizerOption`
- `transport.WithTLSMinVersion(version uint16)` — returns `HTTPOption`

### Behavior Contract

- **Backward compatibility**: Defaults match previous hardcoded values exactly
- **Zero breaking changes**: Existing deployments work without config updates
- **Override capability**: Users can customize via `meept.json5` if needed

## Child Index

| ID | Document | Type | Est. Context | Dependencies | Status |
|----|----------|------|--------------|--------------|--------|
| 01 | 01-extract-hardcoded-configs.md | Leaf | ~60K | None | PENDING |

## Dispatch Protocol

1. **Dispatch implementation agent** via `delegate_task`:
   - Read leaf document
   - Include: "Do NOT commit. Write code, run tests, report results."
   
2. **Review** (main model, in-session):
   - Verify all hardcoded values extracted
   - Check config schema updated
   - Run `go build ./...`

3. **Commit** (after review passes):
   - Stage changed files
   - `git commit -m "feat(config): extract hardcoded operational parameters"`

## Completion Tracking Table

| Leaf | Status | Iter | Completed | % | Notes |
|------|--------|------|-----------|---|-------|
| 01-extract-hardcoded-configs | COMPLETE | 1 | 2026-07-23T17:30 | 100% | 5 parallel subagents + daemon wiring. All tests pass. |

## Integration Test Plan

After completion:
- [ ] `go build ./...` succeeds
- [ ] Config schema includes new fields
- [ ] Defaults match previous hardcoded values
- [ ] Documentation updated if needed

## Open Questions

None — extraction follows established patterns with clear decisions:
- All defaults match current hardcoded values (backward compatible)
- Config field naming follows existing conventions (snake_case JSON, camelCase Go)
- Options pattern already used throughout codebase for constructor customization
- No trade-offs: extraction purely improves flexibility without changing behavior

No ambiguities or design forks exist.

## Coding Conventions

- Add fields to appropriate config struct sections
- Provide sensible defaults matching current hardcoded values
- Use camelCase for JSON field names
- Add comments explaining each field's purpose

## Review Checklist

- [ ] All 5+ hardcoded values identified and extracted
- [ ] Config schema updated with new fields
- [ ] Defaults match previous hardcoded values
- [ ] Code uses config values instead of literals
- [ ] `go build ./...` succeeds
- [ ] No debug artifacts
