# Resolver Sticky Endpoint Preference - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Per-session preferred-endpoint memory in the Resolver so
  alias rotation preserves GPU affinity.
- **Dependencies:** none (endpoint identity `EndpointKey` already exists
  in internal/llm)
- **Estimated Context:** 50K
- **Concurrency Group:** Wave 2

## Goal

When an alias rotates (quota block, timeout, health), the next candidate
is picked by static ordering — the session lands on a different GPU even
though its old endpoint may have recovered or another healthy replica
holds its KV cache. This leaf makes the Resolver try a session's last
successful endpoint first.

## Context

- `internal/llm/resolver.go`: `ResolveForAlias` / `RotateToNextModel` /
  `HasHealthyModels` own candidate ordering. `EndpointKey` = host +
  credential fingerprint (defined for timeout cooldowns — see
  `Resolver.endpointBlocks`, internal/llm/AGENTS.md "Endpoint-level
  cooldown identity"). Blocks state lives ON THE RESOLVER — put the
  sticky map there too.
- Request-scoped resolution travels via `llm.WithResolvedModel`; alias
  selection is request-scoped, never mutated on shared state — the sticky
  map is a READ-side preference, not shared-state mutation.
- The serving client knows the EndpointKey that served a call; the
  resolver does not see successes directly. Find the actual reporting
  seam during exploration: either the client's success path can call
  `RememberSessionEndpoint` (it has providerID/modelID + sessionID from
  chatOptions), or ProviderManager's chatWithFailover path reports it.
  Pin ONE seam and document it.

## Interface Contracts (From Parent)

### Contract 4

```go
// File: internal/llm/resolver.go
package llm

// RememberSessionEndpoint(sessionID, endpointKey string) — record the
//   endpoint that just served sessionID successfully. Overwrites on every
//   success. No-op when sessionID == "".
// PreferredEndpoint(sessionID) (string, bool) — the remembered endpoint,
//   if any, consulted BEFORE candidate ordering.
// Integration rule: during alias resolution, if PreferredEndpoint returns
//   a key whose endpoint belongs to a HEALTHY candidate of this alias
//   (not quota-blocked, not endpoint-cooldown-blocked, not alias-blocked),
//   that candidate is tried FIRST. Otherwise normal ordering runs
//   unchanged. Blocks and refusal policy take precedence over stickiness.
// Storage: map[string]string + LRU order slice, cap 1024 sessions,
//   resolver-mutex-guarded, in-memory only, never persisted.
```

### What This Leaf Exposes

The two methods above plus the ordering hook inside alias resolution.
Sticky behavior is observable: two calls from one session against a
multi-endpoint alias land on the same endpoint while it stays healthy.

## Tasks

1. **RED:** extend an existing resolver test file with
   `TestStickyEndpointPreference`: build a Resolver with a two-entry
   alias; `RememberSessionEndpoint("s1", keyA)`; assert
   `PreferredEndpoint("s1") == keyA`; assert resolution order puts the
   keyA candidate first; assert unknown session falls back to normal
   order; assert a quota-blocked keyA candidate is skipped (normal order
   resumes); assert cap eviction at 1024 (optional: shrink cap via a
   test hook if present, else skip the eviction assertion and note it).
   Fails first.
2. **GREEN:** implement map + LRU + the ordering hook. Keep the existing
   block-precedence order: quota blocks > endpoint blocks > alias blocks
   > stickiness.
3. **Wire the success reporting seam:** at the chosen choke point (see
   Context), call `RememberSessionEndpoint` on every successful chat that
   carries a session scope and a known EndpointKey. Log at Debug.
4. **Verify:** `go build ./...`; `go test -p 2 ./internal/llm/...`;
   confirm quota/endpoint/alias block tests still pass untouched.

## Self-Verification Checklist

- [ ] `go build ./...` clean
- [ ] `go test -p 2 ./internal/llm/...` passes (existing block-precedence
      tests green, untouched)
- [ ] Stickiness never promotes a blocked or unhealthy endpoint
- [ ] Map is mutex-guarded, LRU-bounded, never persisted
- [ ] No session content logged; endpointKey/sessionID logging at Debug only
- [ ] No line-number corruption, no TODOs
- [ ] Do NOT commit

## Review Checklist (for the orchestrator)

- Block precedence order preserved exactly (quota > endpoint > alias >
  sticky).
- One reporting seam, documented in code (not split across client and
  manager).
- Read-side preference only: resolution remains request-scoped
  (WithResolvedModel), no shared mutable resolution state.
