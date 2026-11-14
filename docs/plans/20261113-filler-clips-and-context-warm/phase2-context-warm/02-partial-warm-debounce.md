# Leaf 02 — Partial → Warm Routing + Debounce

> **DISPATCH INSTRUCTION:** Implement this leaf end-to-end with TDD in the
> isolation worktree (path in your dispatch context). Do NOT commit. Do NOT commit. Do NOT run `git commit`
> or `git add`. Make the 00T gate tests green without editing them.

## Meta

- **Parent:** orchestrator.md (phase2-context-warm)
- **Depends on:** 00T TESTS_VALIDATED (codes against C4 signature; runtime
  needs 01 merged for the full suite GREEN — orchestrator sequences the
  final test run after both leaves REVIEWED)
- **Estimated context:** ~60K
- **Parallel group:** Wave A (with 01; file-disjoint)
- **Requirements:** R7, C6

## Goal

Route live voice partials into `Client.Warm` with debounce, gated by.

## Scope

Route live voice partials into `Client.Warm` with debounce, gated by
`voice.context_warm` (default false).

## Tasks

1. Config: add `ContextWarm bool` + `ContextWarmMinTokens int` (default 60)
   to the voice block in `internal/config/schema.go` next to
   `TurnConfirmMs` (schema.go:5500) with tri-state per-employee override
   only if the existing employee_leg pattern makes it cheap — otherwise
   global-only and SAY SO in the report. Template keys + comments in
   `config/meept.json5` (voice block ~1358).
2. Ingress: the daemon's C1 prompt router handles `last:false` frames
   today (internal/voice/AGENTS.md: partials aggregate; voice_wiring.go /
   routeFrame path). Add a hook at the point where a partial's text is
   known to belong to a LIVE voice session:
   - resolve the employee's session + alias + rendered prefix EXACTLY as
     the imminent real turn will render it (reuse the turn-builder's
     message assembly; a divergence here silently kills the cache hit —
     add a test asserting warm-prefix == turn-prefix for the same state).
   - token floor: skip when est. tokens < ContextWarmMinTokens (rough
     len/4 estimate is fine; the estimator already exists in
     internal/llm token cache — use it if exported cheaply).
3. Debounce state machine (per call session, map guarded): pending timer =
   window `voice.turn_confirm_ms` (0/legacy mode: fixed 900 default); a
   newer partial replaces pending text (latest wins, at most 1 in-flight
   warm — 01's semaphore gives replace-discard for free); `last:true` or
   call `end` cancels pending + abandons in-flight (ctx cancel).
4. Session affinity identity: pass the SAME scope value the real turn uses
   (session_id plumbing) so llama.cpp keys both requests to one cache
   slot (sticky rotation rules in internal/llm/AGENTS.md still apply —
   warm must not create a NEW affinity scope).
5. Telemetry: `llm_calls` must show zero warm rows; add a counter
   `voice_context_warm_total{result}` dimension-free or reuse metrics
   pattern — keep it DEBUG-grade cheap.

## Interface Contract (exposes to siblings)

None downstream. Consumes `Client.Warm` (C4) through a package-level seam
var (func value) so 00T's injected counter works without linking the real
client.

## Self-Verification Checklist

- [ ] `go build ./internal/voice/... ./internal/daemon/...`
- [ ] `go test -p 2 ./internal/voice/... -count=1` green (with 01 merged)
- [ ] Disabled-flag test: routing hook is a pure no-op (byte-identical
      dispatch timing assertions from 00T)
- [ ] prefix-equality test warm-vs-turn passes
- [ ] `gofmt -l` empty

## Review Checklist (for the orchestrator)

- [ ] Opt-in default false honored (docs/agents/opt-in-defaults.md rule).
- [ ] No new bus topic invented for this (rule: use existing lanes; a
      counter metric is enough).
- [ ] Teardown paths (hangup, employee unpark, reaper) all cancel pending
      warms — grep every call-end site, don't trust the happy path.
