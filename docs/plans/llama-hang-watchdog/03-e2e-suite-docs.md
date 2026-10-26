# E2E Suite + Docs — Implementation Leaf

> **For the implementing agent:** Gate evidence for this leaf is the e2e suite
> itself (matrix row F10). Follow the repo e2e policy exactly: suite dir +
> manifest path_map entry in the SAME commit-ready change. Do NOT commit.
> Do NOT git add. No read_file→write_file; write once and stop.

## Meta

- **Parent:** ./master.md
- **Scope:** Hermetic e2e suite `runtime-hang` proving end-to-end hang detection → restart → event; docs (workflows + AGENTS.md updates per the maintenance rule).
- **Dependencies:** leaf 02 REVIEWED
- **Estimated Context:** 60K
- **Concurrency Group:** D

## Goal

A hermetic e2e suite where a fake llama-server answers `/health` 200 forever but wedges `/v1/chat/completions` (blocks, never responds). The daemon must: reach VerdictHung within canary threshold, fire the restart (fake server process killed + respawned by the runtime supervisor), publish `runtime.health` verdict=hung on the bus, and recover to healthy when the respawned fake answers completions again.

## Context

- E2E tier conventions: `docs/workflows/e2e-testing.md` + `e2e/manifest.json` + existing suites (copy the closest: `e2e/suites/daemon-lifecycle/` or `runtime-guards/` — check which spawns a fake runtime; `cat e2e/manifest.json` first).
- Build tag `e2e`; harness in `e2e/harness/` (daemon spawn, scratch home, bus subscribe helpers — reuse, do not reinvent).
- Pre-commit check 18/18 (`pre-commit-e2e`) requires the manifest `path_map` entry for `internal/llm/` → `runtime-hang` to land with the suite.

## Tasks

### Task 1: Fake wedged llama-server

**Files:** Create `e2e/suites/runtime-hang/fakeserver/main.go` (or reuse an existing fake-runtime helper if the harness has one — check `e2e/harness/` first and prefer it).

Behavior: flag-controlled phases — `--wedge-completions` makes `/v1/chat/completions` block forever (channel never fires) while `/health` and `/props` return 200; on restart (fresh process) it serves both normally. Print the phase to stdout so the test can synchronize.

### Task 2: Suite test

**Files:** Create `e2e/suites/runtime-hang/hang_test.go` (build tag `e2e`, package per existing suite convention).

Flow:
1. Harness-spawn daemon with a lifecycle config pointing the local runtime's spawn_command at the fake server binary with `--wedge-completions`, `canary.interval_seconds: 1`, `canary.timeout_seconds: 1`, `canary.threshold: 2` (fast test cadence).
2. Subscribe to bus topic `runtime.health` before the daemon starts (harness helper).
3. Assert within ~30s: event verdict=hung for the endpoint key; daemon log contains the hung ERROR line; the fake server process was restarted (supervisor kill → respawn; assert new PID or the respawned server's healthy completions answering a subsequent chat turn).
4. Assert recovery event verdict=healthy after the respawned fake answers completions.
5. Cleanup per harness conventions (EXIT trap equivalents — the harness owns daemon teardown; do not leak the fake server).

### Task 3: Manifest + docs

**Files:**
- `e2e/manifest.json`: add suite `runtime-hang` + path_map entries `internal/llm/health_checker.go`, `internal/llm/runtime_manager.go` → `runtime-hang` (keep existing mappings; append).
- `docs/workflows/runtime-hang-watchdog.md` (new): what the canary is, verdict tri-state, config surface (Contract 1 block), recovery behavior, operator runbook (log lines to grep, `runtime.health` topic, doctor visibility).
- `internal/llm/AGENTS.md`: add a "Hang detection (canary + self-watchdog)" section — invariants: canary never touches Resolver/slot gate/ledger; HUNG drives restart ONLY through the existing bool callback; watchdog re-arms via Start() and never spawns a second run.
- Root `AGENTS.md`: one-line summary under the runtime-lifecycle invariants pointing at the package doc (maintenance rule: same commit).
- `docs/configuration/` runtime lifecycle page: document the `canary` config keys if that page enumerates health_check fields (grep first; only touch if it does).

## Self-Verification Checklist

- [ ] `make e2e-fast-area AREA=runtime-hang` GREEN
- [ ] `make e2e-fast` unaffected suites still GREEN (at minimum `daemon-lifecycle`, `runtime-guards`)
- [ ] manifest.json parses (`python3 -m json.tool e2e/manifest.json`)
- [ ] No fake-server or daemon process leaked after the run (`pgrep -f fakeserver` empty)
- [ ] Docs name real symbols/paths (file:line verified)

**DO NOT COMMIT.**

**Deviations from spec:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Suite is hermetic (no network, no real model, no cloud)
- [ ] Fast cadence config used (whole suite < 60s)
- [ ] Manifest entry + suite in the same change (check 18/18 will block otherwise)
- [ ] AGENTS.md root + internal/llm updated consistently (root maintenance rule)
- [ ] Cleanup leaves no orphans; harness conventions followed

Output: APPROVED or gaps with file:line.
