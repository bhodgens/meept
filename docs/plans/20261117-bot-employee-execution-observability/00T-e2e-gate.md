# Cross-branch execution-observability e2e gate - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Write TESTS ONLY. Do NOT write implementation files. Tests must fail RED for
> missing-implementation reasons. Do NOT commit - the orchestrator handles all
> git operations after review. Do NOT use read_file on existing source files;
> explore with search_files or terminal cat. After writing a file, do NOT read
> it back. Report what you built, files touched, and deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** Hermetic e2e suite proving result/state events and bot inbox end-to-end
- **Dependencies:** none (test gate; runs before all implementation leaves)
- **Estimated Context:** 55K
- **Concurrency Group:** A
- **Audit references:** issue #140 acceptance criteria 1-4

## Goal

Create `e2e/suites/execution-observability/` with hermetic scratch-MEEPT_HOME
daemons and a live WS collector. Never mock the bus or the relay. Cover four
scenarios: bot/employee result event over WS, turn-state edges, bot roster
addressability with next-run delivery, and topology freshness.

## Context

- `e2e/suites/inter-agent-coord/turn_state_test.go` - worked example of the
  scratch-daemon + WS collector pattern; copy its harness shape, not its file.
- `e2e/manifest.json` - suite registry; add one suite entry and scenario names.
- `internal/comm/http/server.go:740-805` - WS subscription list. `bot.result` and `employee.result`
  is 2-segment and needs an explicit pattern.
- `internal/agent/coordination.go:42-56` - frozen turn-state types.

## Verified Anchors

- `e2e/manifest.json:1412` - `inter-agent-coord` suite object (shape template)
- `e2e/suites/inter-agent-coord/turn_state_test.go:1-60` - suite header + imports
- `internal/comm/http/server.go:740-805` - WS topic subscription list
- `internal/agent/coordination.go:42-56` - TurnStateEvent + TopicTurnState

## Interface Contracts (From Parent)

### What This Leaf Exposes

- suite `execution-observability` with scenarios 01-04 in `e2e/manifest.json`

### What This Leaf Consumes

- C1 shared ExecutionResultEvent / `bot.result` + `employee.result` - owner 01-wire-contract/01
- C2 TurnStateEvent / `agent.turn.state` - existing
- C4 roster + drainer contract - owner 03-inbox/01

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Add suite + manifest entry | Create: `e2e/suites/execution-observability/observability_test.go`; Modify: `e2e/manifest.json` | `go test -tags e2e ./e2e/suites/execution-observability/... -run TestExecutionObservability -count=1` | package missing / symbols absent | compiles, fails on missing events | Given scratch daemon; When suite runs; Then manifest lists 4 scenarios |
| T2 | Assert result events over WS | same test file | as T1 -run `TestExecutionObservability_ResultEvent` | no `agent_progress` frame on a result topic | frames decoded into agent.ExecutionResultEvent | Given WS subscriber; When bot and employee rounds complete; Then a `bot.result` frame and a separate `employee.result` frame each carry status+stop_reason, no `kind` key |
| T3 | Assert turn-state edges | same test file | as T1 -run `TestExecutionObservability_TurnState` | no running/idle edges | both edges decoded | Given a round; When it executes; Then running then idle with stop_reason end_turn |
| T4 | Assert bot inbox round-trip | same test file | as T1 -run `TestExecutionObservability_BotInbox` | bot id rejected by send_agent_message | accepted + drained next run | Given a bot id; When send_agent_message targets it; Then next round drains and injects |

T1 worked shape:
1. Write the suite header with `//go:build e2e` and a `TestExecutionObservability` table over four subtests.
2. Run `go test -tags e2e ./e2e/suites/execution-observability/... -run TestExecutionObservability -count=1`.
   Expected RED: missing types/suite symbols, not syntax errors.
3. Add `execution-observability` to `e2e/manifest.json` `suites` with `status:"todo"`, `diff:"L"`, four `scenarios` entries, and a `path_map` alias for `internal/bot/`, `internal/employee/`, `internal/agent/`, `internal/comm/http/`.
4. Expected: suite compiles; failures are assertion timeouts on missing bus events.

Gate tests are RED by design. Do not weaken assertions to make them pass.

## Self-Verification Checklist

- [ ] Four scenarios authored, assertions on wire shapes not internals
- [ ] Manifest entry valid JSON and `python3 -m json.tool e2e/manifest.json` passes
- [ ] No implementation files written
- [ ] RED failures are missing-symbol / timeout, not syntax errors

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines of output
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Gate tests exercise real daemons and the WS relay, not mocks
- [ ] Manifest scenarios map 1:1 to F1-F4 in master Test Gate
- [ ] No implementation files changed; no gate test weakened

Output: APPROVED or specific gaps with file + line references.

## Notes

The root gate is cross-branch. It stays RED until branches 01-03 land; that is
expected. Branch-specific unit gates are separate leaves.
