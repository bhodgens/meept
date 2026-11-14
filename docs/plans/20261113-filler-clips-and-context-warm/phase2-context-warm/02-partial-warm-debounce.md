# Leaf 02 - Partial → Warm Routing + Debounce

> **DISPATCH INSTRUCTION:** Implement this leaf end-to-end with TDD in the
> isolation worktree (path in your dispatch context). Do NOT run `git commit`
> or `git add`. Make the 00T gate tests green without editing them.

## Meta

- **Parent:** orchestrator.md (phase2-context-warm)
- **Depends on:** 00T TESTS_VALIDATED (codes against the C4 signature; the
  runtime path needs 01 merged for the suite to go green - the orchestrator
  sequences the final run after both leaves are REVIEWED)
- **Estimated context:** ~60K
- **Parallel group:** Wave A (with 01; file-disjoint)
- **Requirements:** R7, C6

## Goal

Route live voice partials into `Client.Warm` with debounce, gated by
`voice.context_warm` (default false) and by the endpoint-support predicate
from leaf 01.

## Scope

New file `internal/daemon/voice_warm.go` holding the whole router, plus ONE
hook line in `VoiceWiring.routeFrame`. Config keys in `internal/config/schema.go`
and `config/meept.json5`.

## Collision warning (read first)

`VoiceWiring.routeFrame` (`internal/daemon/voice_wiring.go`) is also edited by
Phase 1 leaf 02. Confirm Phase 1 leaf 02 is committed before you touch that
file. Keep everything else in your new file so the diff to `voice_wiring.go`
is one call plus the cancel-on-end lookup.

## Ingress facts (verified - do not re-derive)

- Only `last:true` prompt frames are acted on today:
  `voice_wiring.go:1154` (dispatch) and `:1192` (binding). A `last:false`
  partial falls through to the preview path, so your hook goes BEFORE the
  `last:true` branches and returns without touching them.
- The partial's text is raw STT text. The real turn's messages are rendered
  by the agent/employee turn builder, not by the voice path. You must obtain
  them from that builder (the same seam the turn uses), not assemble them
  yourself.

## Tasks

1. Config: add `ContextWarm bool` + `ContextWarmMinTokens int` (default 60)
   to the voice block next to `TurnConfirmMs` (`schema.go:5500`), plus
   template keys and comments in `config/meept.json5` (voice block near
   line 1358). Global-only; per-employee tri-state is NOT in scope - say so
   in the report.
2. Hook: in `routeFrame`, for `f.Event == voice.EventPrompt` with
   `f.Last == nil || !*f.Last`, look up the live call and hand `(ccid, text)`
   to the router. Nil/unknown ccid, no live voice session, or flag off:
   return immediately (byte-identical timing).
3. Build the warm request from the SAME state the imminent turn will use:
   - resolve the call's employee + session + alias exactly as the turn does,
   - render the message list through the turn builder's assembly (the
     system prompt, persona, history, and the partial as the user turn),
   - pass the SAME `llm.ChatOption` set the turn will pass, including
     `tools` and the accounting `sessionID` (the affinity header and
     `prompt_cache_key` both derive from it).
   A divergence here silently kills the cache hit, so add a test asserting
   warm-prefix equals turn-prefix for the same state.
4. Token floor: use `llm.EstimateChatTokens(messages)`
   (`internal/llm/tokenizer.go:23`). Skip the warm when the estimate is below
   `voice.context_warm_min_tokens`. Do not hand-roll a `len/4` estimator.
5. Debounce state machine, per (session, employee), map guarded:
   - pending window = the resolved `voice.turn_confirm_ms` (absent or 0: a
     fixed 900 ms default, matching the child's own default),
   - a newer partial replaces pending text (latest wins), and never more than
     one warm is in flight,
   - on `last:true` or call `end`: stop scheduling new warms and drop the
     pending timer. Do NOT cancel a warm that is already in flight - let it
     finish under its own 1200 ms deadline. Cancelling mid-flight aborts the
     server's prefill and leaves a partial slot, and the real turn is already
     waiting behind it.
6. Endpoint gate: call leaf 01's support predicate once per call and cache
   the answer on the router's per-call state. A non-reuse endpoint schedules
   nothing.
7. Teardown: grep every call-end site (hangup, employee unpark, reaper,
   `releaseCall`) and make sure each one drops the router's per-call state.
   A stale entry must never warm a later call on a recycled ccid.
8. Telemetry: a cheap counter or DEBUG line per attempted/skipped warm with a
   reason (`off`, `no-endpoint-support`, `too-short`, `debounced`). No new bus
   topic, no `llm_calls` columns.

## Interface Contract (exposes to siblings)

None downstream. Consumes `Client.Warm` (C4) plus the endpoint predicate
through package-level seam vars (func values) so the 00T counter seam works
without linking the real client.

## Self-Verification Checklist

- [ ] `go build ./internal/daemon/...`
- [ ] `go test -p 2 ./internal/daemon/... -count=1` green (with 01 merged)
- [ ] Disabled-flag test: the hook is a pure no-op (assert zero calls on the
      injected seam and unchanged dispatch timing).
- [ ] Prefix-equality test: warm messages equal the turn's messages for the
      same state.
- [ ] Debounce tests: 4 partials in one window imply at most 2 warms with the
      latest text; `last:true` stops further warms; `end` drops pending.
- [ ] Endpoint-gate test: a non-reuse endpoint schedules zero warms.
- [ ] `gofmt -l` empty.

## Review Checklist (for the orchestrator)

- [ ] Opt-in default false honored (`docs/agents/opt-in-defaults.md`).
- [ ] No new bus topic invented; no WS class added.
- [ ] The hook cannot change dispatch ordering or timing for `last:true`.
- [ ] No in-flight warm is ever cancelled mid-flight.
- [ ] Every call-end site drops router state (grep, do not trust the happy
      path).