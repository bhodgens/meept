# Gate Tests — Implementation Leaf (TESTS ONLY)

> **For the implementing agent:** You are the TEST implementer for this gate leaf.
> Write ONLY the test files listed below. Do NOT write or modify any
> implementation file. The tests MUST fail to compile (RED) naming exactly the
> new symbols — that is the correct end state. Do NOT commit. Do NOT git add.
> Do NOT use read_file on existing source — inspect with terminal cat/sed.
> After writing a file, do NOT read it back — write once and stop.

## Meta

- **Parent:** ./master.md
- **Scope:** Full unit test surface for the llama-hang-watchdog feature (Contracts 1-5 in master.md), written before any implementation exists.
- **Dependencies:** none
- **Estimated Context:** 70K
- **Concurrency Group:** A (dispatched alone, Phase 0)

## Goal

Author the gate tests that leaves 01-02 must turn GREEN. Every test asserts behavior from the master's contracts, not implementation details. Read the master's Interface Contracts section first — it is inlined in your dispatch context.

## Context

- Package under test: `internal/llm` (existing `health_checker.go`, `runtime_config.go`, `runtime_manager.go`).
- Existing test conventions to MIRROR (read with terminal cat):
  - `internal/llm/health_checker_test.go` — httptest servers, stdlib testing, no testify.
  - `internal/llm/health_checker_internal_test.go` — whitebox (`package llm`) access to unexported fields.
  - `internal/llm/runtime_config_test.go` — config resolution tables.
- RuntimeConfig construction in tests: copy the pattern from `runtime_config_test.go` (JSON5 fixture → resolved config).
- The canary endpoint is `{server-root}/v1/chat/completions`; health is `{server-root}` + HealthEndpoint (existing field).

## Files to create (exact paths)

1. `internal/llm/runtime_config_canary_test.go` (package llm)
2. `internal/llm/health_checker_canary_internal_test.go` (package llm)
3. `internal/llm/health_checker_verdict_internal_test.go` (package llm)
4. `internal/llm/runtime_manager_watchdog_internal_test.go` (package llm)

## Tasks

### Task 1: Config-surface tests (F1) — runtime_config_canary_test.go

- `TestCanaryConfigDefaults`: lifecycle JSON5 with `"canary": {}` on a loopback llama-cpp endpoint resolves to CanaryEnabled=true, CanaryInterval=60s, CanaryTimeout=15s, CanaryThreshold=3, CanaryPrompt="ping".
- `TestCanaryConfigExplicit`: explicit values (interval 30, timeout 5, threshold 2, prompt "hi", enabled false) survive resolution verbatim.
- `TestCanaryDisabledNonLoopback`: a non-loopback baseURL (e.g. https://api.example.com/v1) with `canary` ABSENT resolves CanaryEnabled=false. With `"enabled": true` explicit, it stays true (operator override wins).
- Assert via the resolved `RuntimeConfig` fields named in Contract 1.

### Task 2: Canary probe behavior (F2, F3, F4, F9) — health_checker_canary_internal_test.go

Use `httptest.NewServer` with a mux: `/health` → 200; `/v1/chat/completions` → programmable handler.

- `TestCanaryRequestShape`: capture the request; assert method POST, path `/v1/chat/completions`, body JSON has keys model/messages/max_tokens/stream, max_tokens==1, stream==false, messages[0].content == configured prompt.
- `TestCanaryEmptyChoicesIsFailure`: 200 with `{"choices":[]}` counts as a canary failure (drive the checker's canary tick directly; assert the internal consecutive-failure counter or the verdict after threshold ticks).
- `TestCanaryNon200IsFailure`: 500 response = failure; 200 with unparseable body = failure.
- `TestCanaryArmsOnlyAfterFirstHealthOK`: fresh checker, /health returns 503; run canary-interval ticks; assert the completions handler was never hit. Then flip /health to 200, tick, assert one completion request arrives.
- `TestCanaryBypassesLedger`: with a metrics recorder wired (copy the recorder fixture from existing internal/llm tests if present; else skip-guard with t.Skip("no recorder fixture")), assert canary ticks add zero llm_calls rows. If no recorder seam exists in-package, instead assert the canary client is constructed without a Recorder field (whitebox struct check) and document in the test comment.
- Drive canary ticks deterministically: construct the HealthChecker with CanaryInterval=10ms and call the unexported canary check method directly (whitebox) OR sleep-tick with the run loop; prefer direct method invocation for determinism.

### Task 3: Verdict surface (F5, F6, F7) — health_checker_verdict_internal_test.go

- `TestVerdictHungAfterThreshold`: /health 200, completions hang (handler blocks on a channel until test end). After CanaryThreshold canary failures, `VerdictNow() == VerdictHung`.
- `TestVerdictUnhealthyWhenHealthFails`: /health 500 (or refused) drives VerdictUnhealthy at the existing HealthThreshold — unchanged semantics.
- `TestVerdictHealthyRecovery`: from HUNG, completions start answering 200+choices; next canary tick returns VerdictHealthy and resets the failure counter.
- `TestBoolCallbackUnchangedOnHung`: register `OnHealthChange(func(bool))`; on transition into HUNG the bool callback receives false exactly once (restart path keeps working); on recovery, true exactly once.
- `TestVerdictCallbackPayload`: register `OnHealthVerdict`; on HUNG assert callback got VerdictHung with non-empty detail; assert ordering: verdict callback fires for the same transition as the bool callback (no dropped transitions).

### Task 4: Self-watchdog (F8) — runtime_manager_watchdog_internal_test.go

- `TestRunTicksAdvance`: start a checker with HealthInterval=5ms against a live httptest server; after ~50ms `RunTicks()` strictly increased; snapshot-compare twice.
- `TestWatchdogRearmsStalledRun`: whitebox — start checker, then simulate a stalled run by replacing the run goroutine's effect (simplest deterministic approach: Stop() the checker so running=false while the endpoint still registers a live process probe returning true; then invoke the manager's watchdog tick function directly and assert (a) ERROR log emitted containing "health run stalled" (capture via slog handler fixture), (b) checker running==true again, (c) RunTicks resumes advancing).
- Keep the watchdog tick function unexported but callable from the internal test (e.g. `m.watchdogTickOnce(now)`); name it in a comment so leaf 02 implements that seam.

## Self-Verification Checklist

- [ ] All 4 files written at exact paths; package declarations correct (`package llm` for all — internal tests)
- [ ] `go vet ./internal/llm/ 2>&1` fails ONLY with undefined: CanaryConfig / Verdict / VerdictHung / VerdictUnhealthy / VerdictHealthy / RunTicks / VerdictNow / OnHealthVerdict / CanaryEnabled / CanaryInterval / CanaryTimeout / CanaryThreshold / CanaryPrompt / watchdogTickOnce (or the exact symbol names in master Contracts) — no syntax errors, no other undefined symbols
- [ ] No implementation file touched (`git status --short` shows only the 4 new test files)
- [ ] Every test maps to a gate matrix row F1-F9 (state the mapping in each test's doc comment)

**DO NOT COMMIT.** The orchestrator handles all git operations after validation.

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Control Agent)

- [ ] vet output names ONLY the contracted new symbols (RED for the right reason)
- [ ] Assertions are behavioral (request shape, verdict transitions, callback counts), not whitebox field poking beyond the documented seams
- [ ] No tautologies; no test depends on wall-clock sleeps longer than 200ms total
- [ ] Task 2/3/4 patterns mirror existing test-file conventions
- [ ] git status shows exactly 4 new files

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- If a needed fixture (slog capture, metrics recorder) does not exist in-package, write the minimal fixture inside the new test file and note it for leaf 02 reuse.
- Do not invent extra contract symbols; if a test seems to need one, write the test against the contracted symbol set and flag it in Deviations.
