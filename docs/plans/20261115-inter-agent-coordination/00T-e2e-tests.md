# Inter-Agent Coordination E2E Suite - Test Leaf (ROOT GATE)

> **For the implementing agent:** Write the e2e tests ONLY. Do NOT write or modify implementation files. Tests must fail RED for missing-implementation reasons. Do NOT commit. Do NOT git add. This is the ROOT test gate for the whole tree.

## Meta

- **Parent:** ../master.md
- **Scope:** The hermetic e2e suite `inter-agent-coord` covering the end-to-end contracts: turn state observed via WS, stopReason on delegate result, ACP plan update reaches WS, typed inbox round-trip, structured taint scan block/allow.
- **Dependencies:** none (gates everything; dispatched at root Phase 0 alongside branch gates)
- **Estimated Context:** 45K
- **Concurrency Group:** A (root)

## Goal

One e2e suite that proves the vocabulary works across surfaces: a delegate child's turn state reaches a WS subscriber; a delegate result carries stopReason; an ACP plan update surfaces via WS; send_agent_message with structured payload round-trips; the taint scan blocks a structured deny and passes a clean one.

## Context

- Suite dir: `e2e/suites/inter-agent-coord/` with `//go:build e2e` files.
- Manifest: `e2e/manifest.json` — add the suite + path_map entry (this leaf writes the suite; the manifest entry is added here or by 04-01 — do it here to keep the gate self-contained, and note it for 04-01).
- Harness: `e2e/harness` — scratch daemon via the standard harness (see e2e/suites/delegate-seam/ for the pattern).
- The suite exercises the PUBLIC surface: WS events, bus topics via the daemon, the send_agent_message/inbox tools, and the handoff boundary (via a collab driver or the exported PrepareHandoff).

## Verified Anchors

- `e2e/suites/delegate-seam/` — the closest existing suite (delegate lifecycle); copy its harness pattern.
- `e2e/harness/daemon.go` — scratch-bin daemon spawn.
- `e2e/manifest.json` — suite registration format.
- `internal/comm/http/server.go` — WS relay (the suite asserts agent.turn.state arrives over WS).

## Interface Contracts (From Parent)

### What This Leaf Exposes

- Suite `inter-agent-coord` (manifest entry + path_map).
- Test files referencing: `agent.TopicTurnState` / `agent.TurnStateEvent` (contract 01-01), `agent.DelegateResult.StopReason` (02-01), `acp.AgentProgressUpdate` (02-02), `employee.AgentInboxBody` (03-01), `agent.HandoffBoundary` (03-02).

### What This Leaf Consumes

- All contracts C1-C5. Tests fail to compile until the leaves land — that is the RED state.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Turn state observed end-to-end via WS | Create: `e2e/suites/inter-agent-coord/turn_state_test.go` | `make e2e-fast-area AREA=inter-agent-coord` | FAIL: topic undefined / not emitted | PASS: spawn delegate; WS subscriber sees running then idle on agent.turn.state | Given a daemon + WS subscriber; When a delegate child runs; Then TurnStateEvent running+idle received |
| T2 | Delegate result carries stopReason | Modify: `turn_state_test.go` or new `delegate_result_test.go` | same | FAIL: DelegateResult has no StopReason | PASS: delegate result event includes stop_reason=end_turn | Given a completed child; When result consumed; Then stop_reason present |
| T3 | ACP plan update reaches WS | Create: `e2e/suites/inter-agent-coord/acp_progress_test.go` | same | FAIL: no plan kind emitted | PASS: scripted ACP session/update plan → WS agent_progress with stage acp + plan detail | Given an acp persona session; When agent sends plan update; Then WS event observed |
| T4 | Typed inbox round-trip | Create: `e2e/suites/inter-agent-coord/inbox_test.go` | same | FAIL: no structured param | PASS: send_agent_message{to, structured} → inbox returns AgentInboxBody with structured | Given two agents; When structured message sent + drained; Then body parses with Structured set |
| T5 | Structured taint scan block/allow | Modify: `inbox_test.go` or new `scan_test.go` | same | FAIL: no structured branch | PASS: structured file_path with deny pattern blocks; clean passes | Given block-mode boundary; When handoff with structured deny; Then blocked with field name; clean → pass |

**Harness pattern (from delegate-seam):**
1. Build scratch daemon via `e2e/harness` (the harness builds meept-daemon + meept into `$TMPDIR/meept-e2e-bin*`).
2. Start daemon with a test config enabling agents + employees + ACP (for T3).
3. Connect a WS client; subscribe to the session.
4. Drive the surface under test (delegate spawn, acp prompt, send_agent_message).
5. Assert on received events.

**T1 expansion:** use the delegate RPC (or the delegate tool) to spawn a short-lived child; the WS subscriber (subscribed to agent_progress) collects events; assert two TurnStateEvent payloads (state=running, then state=idle) with agent_id matching.

**T3 expansion:** requires an ACP agent that emits a plan update. Use the `testdata/fakeagent` ACP agent (internal/acp/testdata/fakeagent) or a scripted persona; the fake must send a `session/update` with `sessionUpdate:"plan"`. Assert the WS agent_progress event carries stage "acp" and a detail derived from the plan.

## Self-Verification Checklist

- [ ] Suite compiles with `//go:build e2e` (RED until leaves land)
- [ ] Manifest entry added (suite + path_map)
- [ ] Tests assert public behavior (WS events, tool results), not internals
- [ ] No implementation files touched

**DO NOT COMMIT.**

## Completion Report Format

1. **Files touched:** [paths + manifest]
2. **Verify command + output:** [last 20 lines — the RED failures]
3. **RED confirmation:** [undefined-symbol / missing-event failures]
4. **Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Every F1/F4/F5/F6/F7/F8 e2e row covered (T1-T5 map to them)
- [ ] Tests fail RED for missing-implementation reasons only
- [ ] Manifest entry valid
- [ ] No implementation code in test files

Output: APPROVED or gaps.