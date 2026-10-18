# Affinity Header: Config + OpenAI-Compatible Client - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** `llm.session_affinity` config section + default-on affinity
  header in the OpenAI-compatible client (streaming and non-streaming).
- **Dependencies:** none
- **Estimated Context:** 60K
- **Concurrency Group:** Wave 1

## Goal

Make session headers the default on the OpenAI-compatible client instead
of a per-model opt-in sentinel. Header name and on/off are config knobs
(`llm.session_affinity`), so gateways that reject unknown headers can turn
it off without a code change.

## Context

- Config structs: `internal/config/schema.go` (LLM section — find it with
  search_files; mirror how `prompt_cache` config is declared and
  defaulted). After schema changes run `make docs-generate`.
- Client: `internal/llm/client.go`. `applyExtraHeaders(req, cfg,
  sessionID)` (client.go:353) merges config + client-level extras and
  substitutes the `${session_id}` sentinel. Streaming path calls it at
  client.go:2474, non-streaming at client.go:1654.
- Codex client is UNTOUCHED (its `session_id` header at codex.go:518 is
  wire-mandated).

## Interface Contracts (From Parent)

### Contract 1: Config

```go
// File: internal/config/schema.go (LLM section)
package config

type SessionAffinityConfig struct {
    Enabled         bool          // default true
    Header          string        // default "session_id"
    AnthropicHeader string        // default "session_id"
    IdleRelease     time.Duration // default 30m
    TeardownTimeout time.Duration // default 5s
}
// JSON5 key: llm.session_affinity, quoted keys. DefaultConfig() populates
// all five defaults. Empty Header after user override disables the
// OpenAI-compat header; empty AnthropicHeader disables the Anthropic one.
```

Leaf 03 consumes `AnthropicHeader`; leaf 06 consumes `IdleRelease` and
`TeardownTimeout`. Keep the struct exactly as frozen — siblings compile
against it.

### Contract 3: Client header

```go
// OpenAI-compatible client, streaming AND non-streaming:
//   if affinity.Enabled && affinity.Header != "" && sessionID != "" {
//       req.Header.Set(affinity.Header, sessionID)
//   }
// Precedence: an explicit extra_headers entry with the same key wins.
// Implement by checking existence BEFORE setting (Header.Get(k) == "" on
// the request after applyExtraHeaders runs), or set first then let
// applyExtraHeaders override — pick ONE order, document it in a comment.
// ${session_id} sentinel substitution unchanged.
// Empty sessionID => header omitted, never sent empty.
```

### What This Leaf Exposes

- `config.SessionAffinityConfig` with the five frozen fields and defaults.
- The resolved affinity settings reachable from the client layer: thread
  them the way `prompt_cache` config reaches `internal/llm` (follow that
  existing pattern — likely a field on the config snapshot the client
  already receives, or a setter with a typed-nil guard). Do NOT read
  global config from inside the client.
- Header presence on both client call paths when enabled + scoped.

## Tasks

1. **Config first (RED):** extend an existing config test (find the test
   file covering the LLM config section with search_files) with
   `TestSessionAffinityDefaults`: load `DefaultConfig()`, assert all five
   fields; parse a JSON5 snippet overriding `enabled` to false and
   `header` to `""`, assert disabled + empty header. Run — fails (struct
   missing).
2. **GREEN:** add the struct, the field on the LLM config struct, JSON5
   tags (quoted keys), defaults in `DefaultConfig()`. Run
   `make docs-generate` if the docs target regenerates config reference.
3. **Client (RED):** extend the existing client test file that covers
   `applyExtraHeaders` with `TestAffinityHeaderSent`: a client configured
   with affinity enabled + a ModelConfig, `Chat` against an httptest
   server, assert the request carries `Session-Id: <id>` when
   sessionID is set via WithTaskScope option; assert absence when the
   option is absent; assert an explicit `extra_headers` entry with the
   same key wins. Fails first.
4. **GREEN:** implement in `applyExtraHeaders` call vicinity — ONE
   implementation shared by both paths. If the streaming path cannot reach
   the same code, duplicate minimally with a comment cross-linking the two
   sites (mirrors the existing dual-path pattern in this client).
5. **Template comment:** add a commented-out `session_affinity` example
   block in `config/models.json5` (or the main template the LLM section
   ships in — match where other llm.* knobs are documented) with a
   two-line explanation.
6. **Verify:** `go build ./...`; `go test -p 2 ./internal/config/...
   ./internal/llm/...`; `go vet` on both packages.

## Self-Verification Checklist

- [ ] `go build ./...` clean
- [ ] `go test -p 2 ./internal/config/... ./internal/llm/...` passes
- [ ] Streaming AND non-streaming paths both send the header
- [ ] Explicit extra_headers entry beats the default header
- [ ] Empty sessionID => no header; disabled config => no header
- [ ] Codex client untouched (`git diff --stat internal/llm/codex.go`
      is empty)
- [ ] No line-number corruption, no TODOs
- [ ] Do NOT commit

## Review Checklist (for the orchestrator)

- Config struct fields and defaults match Contract 1 byte-for-byte.
- make docs-generate output committed if it changed.
- The resolved-config threading follows the prompt_cache pattern (no
  global config reads in internal/llm).
