# llama-hang-watchdog — Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree.
> (1) Run Phase 0 (test gate) BEFORE any implementation dispatch,
> (2) dispatch implementation agents per leaf, (3) review in-session with the
> main model, (4) re-dispatch if incomplete, (5) commit after review.
> Do NOT implement code yourself.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 4 leaves (1 test-gate leaf + 3 implementation leaves)
- **Scope:** Detect a hung local llama-server (process alive, socket held, inference dead) and auto-recover it; close the silent health-checker-death class.
- **Repo:** /Users/caimlas/git/meept (branch main; shared with sibling sessions — explicit-path staging only)

## Goal

Observed 2026-10-25: the local 8B runtime on 127.0.0.1:8080 hung for 2h15m — process alive, listening socket held, RSS at full model size, 0.3% CPU, every HTTP endpoint (`/health`, `/props`, `/v1/models`) timing out. The existing `HealthChecker` (10s interval, 5s timeout, threshold 3, `auto_restart=true`) logged ZERO "Runtime became unhealthy" events for the era, so no restart ever fired. Two gaps:

1. **Probe depth:** `/health` 200 does not prove the inference path works (llama-server can serve HTTP while slots are wedged). Conversely, a live-but-silent checker never re-probes at all.
2. **Checker liveness:** the health run goroutine can stall or exit silently (audit finding F60 was one such class, fixed for Stop/Start generation guards); nothing today asserts the run is still ticking.

This tree adds: (a) an inference CANARY probe (tiny completion) on a slow cadence, producing a new HUNG verdict distinct from unhealthy/dead; (b) a checker self-watchdog that asserts the run goroutine is advancing and re-arms it if not; (c) HUNG → supervisor kill + RestartProvider (existing paths) + `runtime.hung` bus event + metrics counter; (d) hermetic e2e coverage per the repo's e2e policy (new feature tests go in `e2e/suites/`, not new unit files).

## Architecture

`internal/llm.HealthChecker` gains a canary lane: after the first post-spawn `/health` 200 (cold-load guard), every `canary.interval` it issues `POST /v1/chat/completions` (`max_tokens:1`, fixed prompt, own `http.Client` with `canary.timeout`). Three consecutive canary failures WITH a passing (or at least answering) `/health` = HUNG. HUNG fires the existing `OnHealthChange(false)` transition (so `attemptAutoRestart` runs unchanged) plus a distinct log/event so operators can tell hang-restarts from crash-restarts. A self-watchdog counter (`runTicks`, incremented in `checkOnce`) is asserted by the manager on its own ticker; a stalled run re-arms via `Start()`. Cloud endpoints and non-loopback runtimes are untouched (canary default-off unless lifecycle+loopback).

## Interface Contracts

### Contract 1: config surface (models.json5 lifecycle block)

```json5
"health_check": {
  // existing fields unchanged: interval_seconds, timeout_seconds, unhealthy_threshold
  "canary": {
    "enabled": true,           // default: true for loopback llama-cpp/mlx runtimes, false otherwise
    "interval_seconds": 60,    // canary cadence; 0 => 60
    "timeout_seconds": 15,     // per-probe budget; 0 => 15
    "threshold": 3,            // consecutive canary failures => HUNG; 0 => 3
    "prompt": "ping"           // fixed completion prompt; "" => "ping"
  }
}
```

```go
// internal/llm/runtime_config.go
type CanaryConfig struct {
    Enabled         *bool  `json:"enabled"` // nil => loopback-lifecycle default (true)
    IntervalSeconds int    `json:"interval_seconds"`
    TimeoutSeconds  int    `json:"timeout_seconds"`
    Threshold       int    `json:"threshold"`
    Prompt          string `json:"prompt"`
}
// HealthCheckConfig gains: Canary CanaryConfig `json:"canary"`
// RuntimeConfig gains resolved fields: CanaryEnabled bool, CanaryInterval,
// CanaryTimeout time.Duration, CanaryThreshold int, CanaryPrompt string
```

### Contract 2: HUNG verdict + events

```go
// internal/llm/health_checker.go
// Verdict is the checker's tri-state result, reported via HealthChangeCallback
// (extended, see Contract 3) and VerdictNow().
type Verdict int
const (
    VerdictHealthy Verdict = iota
    VerdictUnhealthy        // /health failing or process dead (existing semantics)
    VerdictHung             // /health answering but canary failing >= threshold
)
func (h *HealthChecker) VerdictNow() Verdict

// internal/bus topic (raw Publish, multi-shape allowed):
// topic "runtime.health", payload keys: endpoint_key string, verdict string
// ("healthy"|"unhealthy"|"hung"), model string, detail string, ts int64
// WS classification: agent_progress (NOT chat_message) per internal/comm rules.
```

### Contract 3: callback compatibility

`HealthChangeCallback func(healthy bool)` MUST keep working unchanged (runtime_manager.go and tests bind it). Add alongside it:

```go
type HealthVerdictCallback func(v Verdict, detail string)
func (h *HealthChecker) OnHealthVerdict(cb HealthVerdictCallback)
```

The manager wires BOTH: the bool callback keeps driving `attemptAutoRestart`; the verdict callback drives the bus event + `runtime_hang_restarts_total` metrics counter (internal/metrics; counter, no new columns).

### Contract 4: self-watchdog

```go
// internal/llm/health_checker.go
func (h *HealthChecker) RunTicks() uint64   // monotonic; incremented once per checkOnce entry
// internal/llm/runtime_manager.go
// Manager ticker (60s): for each endpoint with a live checker, compare
// RunTicks() to the last sample. No advance across 2 consecutive samples AND
// process alive AND checker not Stopped => log ERROR "health run stalled",
// call hc.Start(context.Background()) (re-arm per its documented contract),
// publish runtime.health verdict=unhealthy detail="watchdog re-arm".
```

### Contract 5: canary request shape

`POST {server-root}/v1/chat/completions`, JSON body:

```json
{"model": "<endpoint model id as resolved>", "messages": [{"role": "user", "content": "<canary prompt>"}], "max_tokens": 1, "stream": false}
```

Success = HTTP 200 AND a parseable `choices` array (empty choices = failure). Any transport error, timeout, non-200, or unparseable body counts as one canary failure. The canary client is separate from the daemon's LLM client and never touches the slot gate, Resolver, or ledger (no llm_calls row — it is infrastructure, not a turn).

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|--------------|--------------|-------------|
| 00T | 00T-gate-tests.md | leaf (tests) | none | 70K | A |
| 01 | 01-canary-probe.md | leaf | 00T VALIDATED | 85K | B |
| 02 | 02-hung-verdict-watchdog.md | leaf | 01 REVIEWED | 90K | C |
| 03 | 03-e2e-suite-docs.md | leaf | 02 REVIEWED | 60K | D |

Serial chain by design: each leaf consumes the previous leaf's exact symbols. 00T is the tests-first gate; NO implementation leaf dispatches until its rows are VALIDATED.

## Dispatch Protocol

### Phase 0: Test Gate (MANDATORY — before ANY implementation dispatch)

1. Dispatch `00T-gate-tests.md` via `delegate_task`:
   - Goal: "Write ALL gate tests from 00T-gate-tests.md — tests only, no implementation"
   - Context: full leaf text + Contracts 1-5 INLINED + coding conventions below + this instruction: "Write tests ONLY. They must fail RED for missing-symbol reasons (compile errors naming CanaryConfig/VerdictHung/RunTicks are the correct RED state). Do NOT commit. Do NOT git add. No read_file→write_file (corruption); use terminal cat/sed to inspect, write once and stop."
2. Control-agent validation (orchestrator, in-session, never delegated):
   - Run `go vet ./internal/llm/` — expect compile failures naming ONLY the new symbols (CanaryConfig, Verdict, VerdictHung, RunTicks, OnHealthVerdict, resolved RuntimeConfig fields). Any OTHER compile error = test bug, re-dispatch.
   - Map every test to a Test Gate matrix row; apply the delta protocol (lesser artifact updates to the greater).
3. Commit gate tests: `test(llm): gate tests for llama-hang-watchdog (F1-F10)` (explicit paths).
4. Mark gate TESTS_VALIDATED; only then Phase 1.

### Phase 1: Dispatch 01-canary-probe.md

- Context: full leaf + Contracts 1, 5 + "Gate tests exist and are committed at internal/llm/health_checker_canary_internal_test.go + runtime_config_canary_test.go. Make them pass. Do NOT delete, skip, weaken, or rewrite any gate test. If a gate test appears wrong, STOP and report. Do NOT commit."
- Review in-session; run gate tests + full `go test -count=1 -p 2 ./internal/llm/`; commit on pass.

### Phase 2: Dispatch 02-hung-verdict-watchdog.md

- Same brief shape + Contracts 2, 3, 4 + gate files health_checker_verdict_internal_test.go, runtime_manager_watchdog_internal_test.go.
- Review; commit on pass.

### Phase 3: Dispatch 03-e2e-suite-docs.md

- Brief adds: e2e manifest rules (path_map entry + suite dir in the SAME commit, pre-commit check 18/18), docs requirements below.
- Review; run `make e2e-fast-area AREA=runtime-hang`; commit on pass.

## Review Checklist

- [ ] All tasks from the leaf implemented; gate tests pass GREEN and `git diff` shows NO implementer modification to gate test files (any diff = automatic NEEDS_FIXES)
- [ ] Contracts 1-5 satisfied exactly (symbol names, JSON keys, topic name, payload keys)
- [ ] `HealthChangeCallback func(bool)` untouched and still drives auto-restart
- [ ] Canary never writes an llm_calls ledger row; never touches Resolver/slot gate
- [ ] Cold-load guard: canary armed only after first /health 200 post-spawn
- [ ] Cloud/non-loopback endpoints: canary default-off; no behavior change
- [ ] e2e: new suite dir + e2e/manifest.json path_map entry in the same commit
- [ ] AGENTS.md (internal/llm + root summary) and docs/workflows updated in the same commit as code
- [ ] No debug artifacts, TODOs, placeholder values; no line-number corruption (`grep -rcE '^\s+[0-9]+\|'` zero)
- [ ] `go test -count=1 -p 2 ./internal/llm/ ./internal/daemon/` GREEN; `go vet` clean; analyzers (`make analyzers`) clean

## Coding Conventions

- **Language:** Go (repo toolchain; GOTOOLCHAIN pinned in Makefile — do not bump)
- **Naming:** exported PascalCase; tests `TestUnit_Behavior`; table-driven with testify-free stdlib style matching health_checker_test.go
- **Errors:** wrap with %w; no bare panic; no ignored errors (pre-commit enforced)
- **Mutex:** never hold across I/O (mutexio analyzer); canary HTTP call OUTSIDE h.mu, mirroring checkOnce's existing shape
- **Testing:** new feature tests → hermetic e2e tier (`e2e/suites/runtime-hang/`, build tag `e2e`); internal-whitebox tests for the checker may extend existing `*_internal_test.go` files or add `health_checker_canary_internal_test.go` (checker internals are an existing-file bugfix-class surface per AGENTS.md e2e policy)
- **Logging:** slog; component tags matching existing (`health-checker`, `runtime-manager`)
- **Format:** gofmt before reporting completion

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|--------------|
| 00T-gate-tests | PENDING | 0 | |
| 01-canary-probe | PENDING | 0 | |
| 02-hung-verdict-watchdog | PENDING | 0 | |
| 03-e2e-suite-docs | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: 01/02/03 stay PENDING until 00T reaches TESTS_VALIDATED.

## Integration Test Plan

After all leaves REVIEWED:
1. `make test` (full suite, -p 2) — GREEN, no regression vs baseline.
2. `make e2e-fast-area AREA=runtime-hang` — new suite GREEN.
3. `make analyzers && go vet ./...` — clean.
4. Manual smoke (local): point a scratch daemon at a fake server that answers /health but hangs /v1/chat/completions; confirm within canary.interval*threshold + margin: ERROR log "runtime hung", restart fires, `runtime.health` bus event with verdict=hung, counter increments.
5. AGENTS.md maintenance rule: root + internal/llm AGENTS.md reviewed in the final commit.

## Test Gate

Test leaf: 00T-gate-tests.md — dispatched in Phase 0, validated by the control agent (orchestrator, in-session).

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F1 | CanaryConfig schema + resolution defaults (nil enabled → loopback default; 0 values → 60/15/3) | 01 leaf, Contract 1 | runtime_config_canary_test.go: TestCanaryConfigDefaults, TestCanaryConfigExplicit, TestCanaryDisabledNonLoopback (committed 00399abd) | n/a — config unit surface | VALIDATED |
| F2 | Canary probe issues correct request shape (path, body keys, max_tokens:1, stream:false) | 01 leaf, Contract 5 | health_checker_canary_internal_test.go: TestCanaryRequestShape (committed 00399abd) | e2e fake server asserts body | VALIDATED |
| F3 | Canary success requires 200 + parseable choices (empty choices = failure) | 01 leaf, Contract 5 | health_checker_canary_internal_test.go: TestCanaryEmptyChoicesIsFailure, TestCanaryNon200IsFailure — assert via VerdictNow; go GREEN with leaf 02 (documented in file header) | same suite | VALIDATED |
| F4 | Cold-load guard: no canary before first /health 200 | 01 leaf | TestCanaryArmsOnlyAfterFirstHealthOK (committed 00399abd) | e2e slow-start fake server | VALIDATED |
| F5 | HUNG verdict: canary failing >= threshold while /health answers | 02 leaf, Contract 2 | health_checker_verdict_internal_test.go: TestVerdictHungAfterThreshold, TestVerdictUnhealthyWhenHealthFails, TestVerdictHealthyRecovery (committed 00399abd) | e2e: hung-completion server → verdict hung | VALIDATED |
| F6 | Callback compatibility: bool callback still fires and drives restart | 02 leaf, Contract 3 | TestBoolCallbackUnchangedOnHung (dualRecorder pins edges+ordering) | e2e: restart observed | VALIDATED |
| F7 | Verdict callback + bus event + metrics counter | 02 leaf, Contracts 2-3 | TestVerdictCallbackPayload (dualRecorder) | e2e: bus topic runtime.health observed | VALIDATED |
| F8 | Self-watchdog: RunTicks advances; stalled run re-armed | 02 leaf, Contract 4 | runtime_manager_watchdog_internal_test.go: TestRunTicksAdvance, TestWatchdogRearmsStalledRun (watchdogTickOnce seam; wdogLogCapture + gateWaitFor fixtures for leaf 02 reuse) | n/a — internal; e2e kill-checker variant optional | VALIDATED |
| F9 | Canary isolation: no llm_calls row, no Resolver/slot-gate touch | 01 leaf | TestCanaryBypassesLedger — behavior-driven llm_calls row count via existing store fixtures | n/a | VALIDATED |
| F10 | e2e suite runtime-hang + manifest entry | 03 leaf | n/a | e2e/suites/runtime-hang/ (hermetic fake llama-server: healthy /health, wedged completions) | PENDING |

Statuses: PENDING | DELTA | VALIDATED

Delta log:
- 2026-10-25, F1-F9 → VALIDATED: gate tests committed 00399abd; control-agent
  verified RED state names ONLY contracted symbols (72 compile errors, dedup
  set = Verdict/VerdictHung/VerdictUnhealthy/VerdictHealthy/VerdictNow/
  OnHealthVerdict/RunTicks/watchdogTickOnce/Canary* fields), gofmt clean,
  zero line-number corruption, zero tracked-file modifications.
- 2026-10-25, F3, tests-updated direction recorded: subagent asserts F3 via
  VerdictNow()==VerdictHung (contracted symbol) instead of unexported
  canaryFails — plan accepted the greater coverage shape; consequence: two F3
  tests turn GREEN at leaf 02, not leaf 01 (leaf 01 self-check adjusted).
- 2026-10-25, F1, plan-updated (leaf 01 review): shipped config/models.json5
  uses the RELATIVE "/health" endpoint, so IsLoopbackBaseURL at
  ValidateAndNormalize time returns false and the canary defaults OFF in real
  deployments. Fix folded into leaf 02: RegisterConfig (which stamps the
  absolute baseURL) must re-resolve CanaryEnabled for the nil-Enabled case
  from the stamped baseURL. Leaf 01 committed 3646f783 (review passed: build
  OK, vet undefined set = exactly leaf-02 symbols, canary counter separate,
  single run goroutine, no I/O under h.mu, IsLoopbackBaseURL reused).
- 2026-10-25, F1, open seam: ValidateAndNormalize sees only the lifecycle
  block; fixture declares the address in options.baseURL + health_check.endpoint.
  If leaf 01 resolves CanaryEnabled at RegisterConfig time instead, adjust the
  fixture per delta protocol (plan is authoritative for behavior, fixture
  follows the resolution seam).

## Open Questions

1. **Canary model id source** — use the runtime's resolved model id (log line "Resolved model configuration ... model_id=lfm-8b-q4") or a literal from the lifecycle block? Rec: resolved model id via the manager's existing snapshot (same source the F-D8 gate uses). Impact: wrong id → llama-server 404 → false HUNG. (Decided default recorded; confirm at 01 review.)
2. **HUNG → restart vs. slot-erase first** — llama.cpp's `POST /slots/0?action=erase` (already used by teardown) might unwedge a stuck slot without a 5-minute cold reload. Rec: ship restart-only in this tree (proven path); record slot-erase-first as a follow-up open item. Impact: restart costs a full model reload (~2-5 min); erase-first could recover in seconds but is unproven for this hang class.
3. **Watchdog scope** — re-arm only, or also root-cause the silent-death class now? Rec: re-arm + ERROR log with runTicks evidence; root-cause needs a reproduced stall (none captured yet). Impact: a recurring silent death would keep getting masked (acceptably — each re-arm logs).

## Notes

- Shared-repo discipline: sibling sessions are live in this repo. Stage explicit paths only; re-check `git log` mid-run; never `git add .`.
- The 2026-10-25 hang forensics (timeline, log excerpts, curl probes) are summarized in the Goal section; the raw evidence lives in the session log, not the repo.
- Pre-commit runs check 18/18 (e2e-affected); leaf 03 adds the suite + manifest entry in ONE commit to satisfy it.
