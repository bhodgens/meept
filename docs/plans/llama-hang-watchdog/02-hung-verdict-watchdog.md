# HUNG Verdict + Self-Watchdog — Implementation Leaf

> **For the implementing agent:** Gate tests ALREADY EXIST and are committed
> (health_checker_verdict_internal_test.go, runtime_manager_watchdog_internal_test.go).
> Make them pass. Do NOT delete, skip, weaken, or rewrite any gate test. If one
> appears wrong, STOP and report. Do NOT commit. Do NOT git add. No
> read_file→write_file; write once and stop.

## Meta

- **Parent:** ./master.md
- **Scope:** Verdict tri-state + HUNG transition (Contract 2), verdict callback + bus event + metrics counter (Contract 3), self-watchdog re-arm (Contract 4), and the manager wiring that fires restarts on HUNG.
- **Dependencies:** leaf 01 REVIEWED (canary lane exists)
- **Estimated Context:** 90K
- **Concurrency Group:** C

## Goal

Turn canary evidence into the HUNG verdict and make silent checker death self-healing.

## Interface Contracts (From Parent)

master.md Contracts 2, 3, 4 — inlined in your dispatch context.

## Tasks

### Task 1: Verdict tri-state in HealthChecker (gate: health_checker_verdict_internal_test.go)

**Files:** Modify `internal/llm/health_checker.go`.

1. Add `Verdict` type + constants (VerdictHealthy/VerdictUnhealthy/VerdictHung) and `VerdictNow()` per Contract 2. Store the current verdict under h.mu; derive it in checkOnce: procDead-or-/health-failing → Unhealthy (existing threshold logic unchanged); canaryFails >= CanaryThreshold while /health answers → Hung; else Healthy.
2. HUNG fires the SAME transition path as unhealthy for the bool callback (false exactly once — gate F6). Recovery fires true exactly once. Keep `notifyTransition` semantics; extend it to also invoke the verdict callback with (Verdict, detail) — detail carries canaryFails, last canary error, endpoint key.
3. `OnHealthVerdict(cb)` per Contract 3; nil-safe; callbacks invoked OUTSIDE h.mu (copy the `go cb(...)` pattern notifyTransition already uses).
4. HUNG resets: canaryFails stays until a canary success resets it (leaf 01) — do not clear on transition.

### Task 2: Manager wiring — bus event + counter (gate: TestVerdictCallbackPayload)

**Files:** Modify `internal/llm/runtime_manager.go`; touch `internal/metrics` only if a counter helper is missing (prefer an existing counter pattern; if metrics has no generic counter seam, keep the counter in-package as an atomic and expose `RuntimeManager.HangRestartCount(endpointKey) uint64` — smaller diff wins; note choice in Deviations).

1. Where the manager wires `hc.OnHealthChange(m.makeHealthCallback(endpointKey))` (runtime_manager.go ~line 196), also wire `hc.OnHealthVerdict(...)`: on VerdictHung publish bus topic `runtime.health` (raw Publish; payload keys per Contract 2) and increment the hang counter; on VerdictHealthy after a Hung, publish recovery verdict too.
2. Bus access: the manager already receives a publisher seam or has one wired for runtime logs (`logToEndpoint`) — reuse the SAME seam; do not add a new constructor parameter unless no seam exists (note in Deviations if you must).
3. Restart path: UNCHANGED — the bool callback continues to drive attemptAutoRestart. HUNG reaches restart through it (gate F6 proves this).

### Task 3: Self-watchdog (gate: runtime_manager_watchdog_internal_test.go)

**Files:** Modify `internal/llm/health_checker.go` (RunTicks) + `internal/llm/runtime_manager.go` (tick).

1. `RunTicks() uint64`: atomic counter incremented once at checkOnce entry (before the generation guard return — a superseded run's tick still proves goroutine liveness; comment this choice).
2. Manager watchdog: one goroutine started in StartAll, stopped in StopAll, 60s ticker. Per endpoint with a live registered checker: sample RunTicks; no advance across 2 consecutive samples AND process-alive probe true AND checker should be running → ERROR log "health run stalled" (fields: endpoint_key, last_ticks, seconds_stalled), call `hc.Start(context.Background())` (documented re-arm path), publish `runtime.health` verdict=unhealthy detail="watchdog re-arm".
3. Expose the single-sample logic as `watchdogTickOnce(now time.Time)` (the gate test calls this seam directly — leaf 00T documented this name; keep it).
4. Watchdog state (last sample per endpoint) lives in a small map under m.mu; no I/O under the lock.

## Self-Verification Checklist

- [ ] `go test -count=1 ./internal/llm/ -run 'Verdict|Watchdog|RunTicks|BoolCallback'` GREEN
- [ ] `go test -count=1 -p 2 ./internal/llm/` GREEN (ALL gate tests now pass)
- [ ] `go test -count=1 -p 2 ./internal/daemon/` GREEN (no wiring regression)
- [ ] `go vet ./internal/llm/` + gofmt clean; `make analyzers` clean (mutexio especially — verdict/watchdog code touches h.mu/m.mu)
- [ ] No gate test file modified

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Bool-callback transition counts unchanged (exactly once per edge) — restart semantics preserved
- [ ] Verdict callback fires on the same edges; no dropped/duplicated transitions
- [ ] Watchdog re-arm uses Start()'s documented re-arm contract; no second run goroutine possible (running-flag gate)
- [ ] Bus topic is `runtime.health` raw Publish with exactly the Contract-2 keys; WS class stays agent_progress (no new topic prefix)
- [ ] No I/O under mutex; RunTicks increment placement commented
- [ ] attemptAutoRestart untouched

Output: APPROVED or gaps with file:line.
