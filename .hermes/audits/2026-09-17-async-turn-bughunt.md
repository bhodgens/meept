# Bughunt 2026-09-17 — async turn migration + refusal fallback

Scope: commits 0014bee2..HEAD (async-turn-migration leaves 01-07 + relay
fix + refusal-fallback feature by sibling session), plus meept-bench
leaf-02 work. Baseline: build + 14 packages -race green at start.

## Execution notes

All 5 dispatched auditor scopes failed with provider 429 (zai 5-hour
pool exhausted, resets 2026-09-17 17:30). Per the rate-limit protocol
the audit fell back to PARENT-DIRECT (in-session) coverage. Scopes
1/4/5 substantially covered by parent pre-reads; scope 3 (refusal
fallback) partially covered; scope 2 (clients) partially covered.
Re-dispatch when the quota window resets for full coverage.

## Findings

### F1 (HIGH, FIXED aa4d5103) — relay correlation destroyed by funnel completion
internal/agent/handler.go: the funnel's completeTurn(req.TurnID) deleted
the registry entry immediately after the parked ack emission. The
task_completed_relay then resolved TurnIDForTask(taskID) to "" and minted
a FRESH turn id — the bench's turn_id filter could never match the real
result, and every async task turn graded its own ack text as the final
reply (bench: 0/9, fails in 2-6s, verdict from content-checking nothing).
Fix: funnel defers completeTurn when syncTaskID != ""; the relay recovers
the originating id via TurnRegistry.TurnIDForTask, emits under it, then
completes the edge. Pins:
TestPublishTaskTerminalRelay_UsesOriginatingTurnID /
_UntrackedTaskGetsFreshID; async-ack event test re-pinned to parked.

### F2 (MED-HIGH, FIXED 18c34d66) — turn.terminal dropped on full subscriber buffer
publishTurnTerminal used non-blocking Publish: drop-on-full subscriber
buffer. TUI EventStream channel = 50 slots; progress floods
(worker/task/step/agent events) fill it between poll ticks. A dropped
turn.terminal leaves an awaiting client permanently un-resolved — the
TUI's stalled re-arm can never see the result because the registry edge
was already consumed by the relay. C-09 critical-topic precedent
(security.*/approval.*) applies. Fix: bus.PublishBlockingT (new typed
wrapper) + publishTurnTerminal switched to it (5s-bounded send, once per
turn). Note: a sibling's typed-bus-topics conversion (a384204f)
restructured this function mid-audit; fix re-applied on the typed form.

### F3 (MED, FIXED 5120221c / af85cbb, pre-bughunt) — relay correlation (superseded by F1's deeper fix)
The 2026-09-16 bench-gate 0/9: relay events under fresh ids. F1's fix is
the durable version; leaf-02's parked-skip in bench ChatAsync remains
correct and required.

### F4 (LOW, OPEN-LEDGER) — CLI `since` clock domain
cmd/meept/chat_async.go:190 seeds the poll cursor with client-local
time.Now(); proxy busEvents use daemon-side timestamps. Same machine
today (LOW), but a cross-host daemon would need NTP discipline.
Preferred long-term shape: a turn-scoped subscription created before
submit with sequence-based cursor (the TUI's in-process router already
avoids the issue entirely).

### F5 (LOW, OPEN-LEDGER) — parked-status overloading
'parked' now means both quota-parked and async-ack. Clients treat parked
as keep-waiting. Consequence: a quota-parked turn (up to 24h park) shows
as pending with refreshed liveness on every surface — intended, but the
TUI/GUI pending line gives no quota-specific reason on the async path
(the GUI's quota text only renders for the quota event path). Candidate
follow-up: carry a reason field on the parked event.

### F6 (OPEN, by design) — sync_chat_enabled=true interactions
Sync wait (110s) + watchdog (120s stale-after): a sync turn polls the
task store, NOT the registry, so no Touch fires during the wait — the
watchdog reaps the turn at 120s and emits a reaped event while the sync
wait may still return the real result at ~110s. Client gets the stub
then a reaped-failed event: acceptable (legacy path), documented, but
worth revisiting if legacy mode stays in use.

## Coverage ledger (what was NOT audited)

- Scope 2 deep pass (TUI models/turn.go 459 lines, chat model wiring,
  Flutter provider state machine) — partial only (parked-skip verified
  on bench; TUI/GUI parked handling spot-checked).
- Scope 3 deep pass (refusal-fallback) — parent read the detection
  markers (conservative 4-marker list), error-body gating (non-200
  only), NonRetryable classification, one-hop give-up rules, and the
  disclosure lifecycle (fresh-turn clear at loop.go:2530); NOT audited:
  streaming-path detection coverage, budget/token accounting across the
  fallback call, alias-health interaction.
- meept-bench checkers' internal logic.
- Re-dispatch scopes 2+3 when the provider quota resets (17:30
  2026-09-17).

## Verification

- go build ./... clean; go vet clean (scope packages)
- go test -race -count=1: internal/agent (79s), bus, rpc, comm/http
  (50s), daemon (77s), metrics, tui pkgs, cmd/meept — all ok
- meept-bench: build + daemonclient/runner/results tests ok
