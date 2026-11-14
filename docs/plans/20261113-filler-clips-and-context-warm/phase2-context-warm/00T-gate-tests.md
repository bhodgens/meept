# Leaf 00T — Phase 2 Gate Tests (tests only)

> **DISPATCH INSTRUCTION:** Write TESTS ONLY — no implementation files.
> Compile failures on named new symbols (`Client.Warm`, `voice.context_warm`)
> are expected RED; list them in the report. Do NOT commit.
> Do NOT run `git commit` or `git add`.

## Meta

- **Parent:** orchestrator.md (phase2-context-warm)
- **Depends on:** none (dispatch FIRST)
- **Estimated context:** ~50K
- **Requirements:** R7, R8, R9

## Goal

Test suite for the partial context-warm feature, authored against root.

## Scope

Test suite for the partial context-warm feature, authored against root
Contract C4 (Warm signature) and branch Contract C6 (config keys), copied
verbatim into the headers.

## Tasks

1. `internal/llm/warm_test.go` — against a fake OpenAI-compatible
   upstream (httptest, the pattern codex_sse_test.go uses): payload has
   `max_tokens:1`, non-streaming, session-affinity header equals the real
   turn's scope value; returned text never surfaces (no callback, no
   error propagation — assert quiet); quota-blocked fake upstream ⇒ Warm
   logs DEBUG and returns without retry storms; `golangci` clean.
2. Config test: `voice.context_warm` default false; disabled ⇒ the routing
   seam calls Warm zero times (assert via injected counter seam).
3. Debounce unit tests (routing file's package, white-box with the fake
   seam): (a) 4 partials inside one `turn_confirm_ms` window ⇒ at most 2
   warms with the LATEST text; (b) `last:true` cancels pending; (c) prompt
   under `context_warm_min_tokens` ⇒ no warm; (d) call `end` ⇒ pending
   cancelled.
4. `e2e/suites/context_warm/` + `e2e/manifest.json` entry (path_map for
   `internal/llm`, `internal/voice`): hermetic fake-upstream scenario
   proving request order warm(…,partial) → turn(final) shares a prefix and
   the flag-off baseline sees ONE request only.

## Interface Contract (exposes to siblings)

The RED list + the fake-upstream recorder helper (leaves 01/02 reuse it).

## Self-Verification Checklist

- [ ] `go vet ./internal/llm/...` fails ONLY on the named missing symbols
- [ ] `go test -p 2 ./e2e/... -run ContextWarm` compiles under tag, RED
- [ ] git status: no non-test .go file touched

## Review Checklist (for the orchestrator)

- [ ] Every phase2 Test Gate row has a named test.
- [ ] Contract strings match master.md C4 + branch C6 byte-for-byte.
