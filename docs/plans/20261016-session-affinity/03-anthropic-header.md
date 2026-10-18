# Anthropic Client Affinity Header - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Anthropic client: sentinel substitution in extra headers +
  default-on affinity header (both call paths).
- **Dependencies:** leaf 02 (config struct `SessionAffinityConfig` with
  `AnthropicHeader` field)
- **Estimated Context:** 40K
- **Concurrency Group:** Wave 2

## Goal

Close the Anthropic gap. Today `applyAnthropicExtraHeaders`
(internal/llm/anthropic.go:1343-1345) copies `config.ExtraHeaders`
verbatim: a `${session_id}` sentinel value would be sent as the literal
string `${session_id}`. The session ID exists at call time
(anthropic.go:299 uses it for metrics) but never reaches the wire.

## Context

- `internal/llm/anthropic.go`: `applyAnthropicExtraHeaders(httpReq)` at
  :1343, called at :1372 (non-streaming) and :1622 (streaming). The
  session ID arrives as a parameter on chat paths (see the function
  signatures near :299 where `SessionID:` is stamped for usage) — trace
  how `sessionID` reaches the request builders to find the substitution
  point.
- Config arrives via the same threading leaf 02 used for the
  OpenAI-compatible client — mirror that pattern; do NOT invent a second
  mechanism.
- The OpenAI-compat sentinel contract is `sessionIDHeaderSentinel`
  (internal/llm/client.go:344): exact full-value match only, empty after
  substitution omits the header. Match it exactly.

## Interface Contracts (From Parent)

### Contract 3 (Anthropic portion)

```go
// Anthropic client, streaming AND non-streaming:
//   1. Sentinel substitution in applyAnthropicExtraHeaders: a value
//      exactly equal to "${session_id}" is replaced with the current
//      session ID; empty result => header omitted.
//   2. Default affinity header:
//      if affinity.Enabled && affinity.AnthropicHeader != "" && sessionID != "" {
//          req.Header.Set(affinity.AnthropicHeader, sessionID)
//      }
//   Precedence: explicit extra_headers entry with the same key wins over
//   the default header (same rule as the OpenAI-compat client — reuse or
//   mirror its existence-check, do not diverge).
```

### What This Leaf Exposes

- `applyAnthropicExtraHeaders` accepting (or closing over) the session ID
  and performing sentinel substitution identical to
  `client.go:344` semantics.
- Default header on both call paths when enabled + scoped.
- No signature change to exported chat methods.

## Tasks

1. **RED:** extend the existing Anthropic client test file (find it with
   search_files — the tests covering header handling exist) with
   `TestAnthropicAffinityHeader`: httptest server asserting (a) default
   header present with scoped call, (b) absent when scope empty,
   (c) absent when disabled, (d) explicit `extra_headers` entry with the
   same key wins, (e) an `extra_headers` entry with value
   `"${session_id}"` resolves to the session ID, not the literal string.
   Fails first.
2. **GREEN:** thread sessionID + resolved affinity config to
   `applyAnthropicExtraHeaders` (signature change on the unexported
   method is fine), implement substitution + default header at both call
   sites.
3. **Verify:** `go build ./...`; `go test -p 2 ./internal/llm/...`;
   `go vet ./internal/llm/...`.

## Self-Verification Checklist

- [ ] `go build ./...` clean
- [ ] `go test -p 2 ./internal/llm/...` passes
- [ ] Both call paths (:1372, :1622) covered
- [ ] Sentinel substitution matches client.go semantics (exact full-value
      match, omit-when-empty)
- [ ] Empty session scope => no header
- [ ] No line-number corruption, no TODOs
- [ ] Do NOT commit

## Review Checklist (for the orchestrator)

- Same precedence rule as leaf 02 (cross-check the two implementations
  read identically).
- Anthropic request shape otherwise unchanged (no body changes).
