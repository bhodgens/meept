# 00T-tests - Test-Gate Leaf (tests for 01-delegation)

> **For the implementing agent (test implementer):** You are DISPATCHED
> FIRST, in Phase 0, BEFORE any implementation leaf. Write tests ONLY. Do NOT
> write or modify implementation files (`internal/agent/delegate*.go`,
> `internal/tools/builtin/delegate*.go`, `internal/codemode/*workflow*`, etc.).
> The tests MUST fail RED for missing-implementation reasons (undefined
> symbols, empty packages), not syntax errors. Run each once to confirm it
> fails for the RIGHT reason. Do NOT commit, do NOT git add — the
> orchestrator validates and commits. If a feature's behavior is ambiguous,
> quote the leaf spec verbatim in the test comment and do not weaken it.

TEST-GATE VARIANT leaf — see skill `hierarchical-planning`
`references/tests-first-gate.md`. Tests only; fail RED; the control agent
(orchestrator) runs the delta protocol.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Complete unit + hermetic-e2e test surface proving the six
  delegation-seam features fail closed before any implementation exists.
- **Dependencies:** this branch's contracts B1..B6 (orchestrator.md)
- **Estimated Context:** 60K-90K
- **Concurrency Group:** A (Phase 0)
- **Audit references:** dsh-audit/subagent.md GAPs 1a, 1b, 2, 4, 3(+4)

## Goal

Write the full test gate for 01-delegation: in-package unit tests for the
pure internal helpers that the master explicitly requires unit coverage for
(fork-seed prefix, model-route selection, descriptor round-trip), plus a
hermetic e2e suite (`e2e/suites/delegate-seam/`) exercising the model-facing
seam over the live RPC/WS surface (async spawn + settlement, fork seed,
steer/interrupt/list, per-child route, workflow tool, cold resume). Every
test asserts observable behavior and state transitions — never implementation
detail, never tautologies. Unit files live in-package with `_test.go`;
feature-surface tests live ONLY in the e2e tier (root AGENTS.md e2e policy).
The delegate-seam suite and its `path_map` manifest entry land in this leaf
(file paths below), RED.

## Context

- `internal/agent/delegate.go` (NEW, B1) — seam core. Will not exist yet; the
  tests name its symbols and fail RED on undefined.
- `internal/agent/delegate_fork.go` (NEW, B2), `internal/agent/delegate_descriptor.go`
  (NEW, B6), `internal/agent/modelselect/` (NEW, B4) — pure helpers.
- `internal/tools/builtin/delegate_control.go` (NEW, B3) — control tools.
- `internal/codemode/sandbox.go:124` — goja VM the workflow tool reuses.
- `e2e/suites/delegate-seam/` (NEW) — the hermetic e2e tier suite.
- `e2e/manifest.json` — add `path_map` keys for `delegate-seam` and any new
  package (`internal/agent/modelselect/`) mapping to the suite.
- dsh reference (behavior source): `/tmp/deepseek-harness/packages/subagent/
  subagent-fork-in-process/src/index.ts` (seed = completed-turn prefix),
  `tool-subagent-control/src/index.ts` (send_message/interrupt), 
  `subagent/model-selection.ts` (route merge+assert+preflight), 
  `workflow/workflow-ptc/src/index.ts:106-112` (caps defaults), 
  `workflow/tool-workflow/src/index.ts:378-408` (result shape), 
  `subagent/descriptor` (durable per-child descriptor, cold-resume).

## Verified Anchors (grep-confirmed at authoring time)

- internal/tools/builtin/platform.go:271-425 — `DelegateTaskTool` + `DelegateResult` + `Execute` (the tool being superseded/expanded)
- internal/agent/registry.go:940-947 — `RunAgent` -> `loop.RunOnce` (the sync child the seam makes async)
- internal/agent/dispatcher.go:4758-4766 — `SteerActiveAgent` -> `queue.Steer`
- internal/agent/queue.go:227-262 — `MessageQueue.Steer(content, source)`
- internal/agent/isolation.go:65-106 — `BuildSpawnContext(iso, brief, artifacts, memoryIDs, parent}`; `IsolationSharedTranscript` (fork seed homes here)
- internal/agent/conversation.go:518 — `Conversation.GetMessages() []llm.ChatMessage`
- internal/agent/registry.go:484-504 — `modelRefForAgent` precedence chain (model: > model_alias: > same-id alias > global default)
- internal/acp/session.go:303 — `(*Session).Cancel`; :262 — `(*Session).Send`
- internal/acp/manager.go:273,281 — `GetOrCreate` / `GetOrCreatePersona`
- pkg/id/id.go:18 — `id.Generate(prefix string) string` (predictable-ID-safe child ids)
- internal/codemode/sandbox.go:3-14,124-160 — no-fs/net/timer confinement, `New(opts)`

## Interface Contracts (From Parent)

### What This Test Leaf Exposes

Test files that, once implementation lands, become the GREEN regression
suite for B1..B6. They are the acceptance criterion for every impl leaf.

### What This Test Leaf Consumes

- B1..B6 (orchestrator.md) — full signatures (restated once in the parent; use them verbatim)
- M-C1 DelegateRequest/DelegateResult (master C1)

## Tasks

### T1: e2e suite scaffold + manifest (RED package)

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T1 | Create the hermetic delegate-seam e2e suite dir + manifest path_map entry, with a first RED scenario | Create: e2e/suites/delegate-seam/delegate_seam_test.go, e2e/suites/delegate-seam/suite_test.go; Modify: e2e/manifest.json (path_map) | `make e2e-fast-area AREA=delegate-seam` | Scenario fails: `delegate_task` returns no `child_id` (async not built) | Scenario passes: spawn returns a child handle and settlement arrives | Given a fresh scratch daemon with the delegate-seam manifest registered; When a test delegates a task and awaits the returned child; Then the child handle has `child_id`, status `running`, and a settlement later carries the final response |

Repo e2e scaffold (all suites): `e2e` build tag files under
`e2e/suites/delegate-seam/`; scratch MEEPT_HOME via `harness.WithPreBootHook`;
assert over the live RPC (`chat.submit`) and WS (`agent.delegate.result`) —
never against mocks of the daemon. Concrete RED assertions in this leaf are
the contract; the implementation leaf 01 makes them GREEN.

1. Write `suite_test.go` (harness boot) + `delegate_seam_test.go` scenarios.
2. `make e2e-fast-area AREA=delegate-seam`
   Expected RED: "delegate_task: unknown child_id / DelegateManager undefined" (missing impl).
3. Manifest: add `"delegate-seam"` suite + `path_map` for
   `internal/agent/` (already present) and any new package
   `internal/agent/modelselect/`; keep existing rows.

Given/When/Then acceptance is above.

### T2: F1 — async spawn + child id + join unit pins

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T2 | In-package unit pins for B1 child-registry types | Create: internal/agent/delegate_test.go | `go test -p 2 ./internal/agent/ -run TestChildRecord -count=1` | `ChildRecord`/`ChildState`/`DelegateManager` undefined | Pins pass | Given a `ChildRecord` built via Manager.Spawn; When its State transitions running→completed; Then `Lookup(childID)` returns the record with State completed and the Result set |

Write a table for `ChildState` transitions (running→completed, running→interrupted,
running→failed), `Children(parentSessionID)` filtering, and `Lookup` on an
unknown child returning `ok=false`. Keep these tests against the B1 struct
shape only — no live registry (e2e covers the seam); they are the fast unit
gate.

### T3: F2 — fork-seed prefix unit pins

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T3 | Unit-test `completedTurnPrefix` slices the parent's completed-turn prefix and drops the in-flight turn | Create: internal/agent/delegate_fork_test.go | `go test -p 2 ./internal/agent/ -run TestCompletedTurnPrefix -count=1` | `completedTurnPrefix` undefined | Slices to the last completed turn; in-flight turn excluded; empty when none completed | Given a parent Conversation with 3 completed assistant turns + an in-flight tool-call turn; When `completedTurnPrefix(conv)` computes the seed; Then the seed ends at the last `turn/end` and excludes the unbalanced in-flight assistant/tool messages |

Mirror dsh `completedTurnPrefix` (fork index.ts:46-53): the seed is the log
through the last completed turn/end; an unbalanced current step cannot
replay. Cases: empty log → empty; balanced single turn → that turn; trailing
in-flight turn → dropped; message ordering/roles preserved. These run against
a fabric-lite `Conversation` (build via `AddUserMessage`/`AddAssistantMessage`
if the symbol is present, else seam-typed stub — prefer the real
conversation.go API).

### T4: F4 — model-route selection unit pins

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T4 | Unit-test `ResolveChildRoute` merge/assert and `PreflightRoute` | Create: internal/agent/modelselect/modelselect_test.go | `go test -p 2 ./internal/agent/modelselect/ -count=1` | package/symbols undefined (no impl) | Merge honors parent/configured/request precedence; provider+model supplied together; allowlist enforced | Given parent route X, configured child default Y, and a request for Z when a policy allows Z; When `ResolveChildRoute` runs; Then it returns Z; and a request for W outside the policy returns an error |

Cover dsh model-selection.ts rules: provider+model must be supplied
together; empty string rejected; policy nil permits only the parent route;
route change without effort clears the configured route-owned effort; no
selection requested → configured (may be nil). Plus `PreflightRoute` error
for an unresolvable route (nil provider/model; alias absent from the
resolver). Assert error strings are non-empty, not exact text (message text
is not frozen).

### T5: F3 — child-control e2e (steer, interrupt, list)

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T5 | e2e: `subagent_send_message` steers a running child; `subagent_interrupt` cancels; `subagent_list_children` enumerates | Create: (in T1 suite files) e2e/suites/delegate-seam/delegate_control_test.go | `make e2e-fast-area AREA=delegate-seam` | tools `subagent_send_message`/`subagent_interrupt`/`subagent_list_children` not registered → spawn tool error | a running child receives a steer at its next step; interrupt returns `accepted:true` and the child transitions to interrupted; list returns the parent's children | Given two delegated children spawned by one parent; When the parent calls `subagent_send_message` on child A and `subagent_interrupt` on child B then `subagent_list_children`; Then A's next result reflects the steer, B is interrupted, and list shows both with their states |

dsh control semantics (tool-subagent-control/index.ts:29-112): a working
agent receives the message at its next step; an idle agent starts a new
turn; interrupt is fire-and-return, returns `accepted:true`, and an external/ACP
child stops permanently. Assert the WS `agent.delegate.result` settlement and
state transitions, not the specific tool string renders.

### T6: F5 — workflow tool e2e + confinement

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T6 | e2e: `workflow` tool runs a confined script that fans out via B1 agents and returns a JSON value; a cap trip is FAIL-CLOSE | Create: (in T1 suite) e2e/suites/delegate-seam/delegate_workflow_test.go | `make e2e-fast-area AREA=delegate-seam` | tool `workflow`/`orchestrate` not registered | a `pipeline`/`agent` script drives ≥1 child, returns the JSON value; a script calling `parallel`+a cap ceiling returns a fatal error (no partial success) | Given a workflow script with `agent()` calls returning JSON; When the tool runs it (foreground); Then it returns `{kind:"foreground", runId, agentsStarted≥1, result:<json>}`; and a second script that trips `max_total_agents` returns a fatal error with a nonzero stop reason |

Assert bounds: a script attempting `parallel()` with more items than
`max_items_per_call` fails loudly (fail-close, never partial); the goja
confinement rejects `require("fs")`/`process` (no fs/net/timer) — assert the
tool returns an isError for a script using a Node API, mirroring the codemode
sec note (sandbox.go:3-14).

### T7: F6 — descriptor cold-resume e2e + unit

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T7 | e2e: interrupted delegation resumes from its durable descriptor after a child settles; unit: descriptor version round-trip | Create: e2e/suites/delegate-seam/delegate_resume_test.go, internal/agent/delegate_descriptor_test.go | `make e2e-fast-area AREA=delegate-seam` + `go test -p 2 ./internal/agent/ -run TestChildDescriptor -count=1` | descriptor types undefined | interrupted child `Resume(childID)` re-spawns from its descriptor (mode/provider/model/seed) and completes; descriptor JSON round-trips with version=1 | Given an interrupted child whose descriptor persisted mode/model/seed; When the daemon calls `DelegateManager.Resume(childID)`; Then the child re-runs from that descriptor (fresh seed replayed) and its final result appears as completed |

dsh `manager.coldResume` (manager.ts:1349-1404): resume a persisted child
from its durable descriptor without loading a live Agent. Unit test pins the
JSON round-trip (Version, Mode, Provider, AgentModel, SeedPayload) with an
unknown/missing version rejected. E2E asserts the resume over the WS surface.

### T8: Helper — extra RED unit pins you must add for any edge leaf 01..06 discovers

If, while writing the six above, you find a pure helper the leaf specs or
handling require but a unit file is not listed, add ONE in-package test for
it (e.g. seed-empty→fresh), name it `Test<Symbol>`, and add it to the Test
Gate matrix via the report. Do NOT add new unsurfaced e2e scenarios beyond
the seven above without noting them in your report so the orchestrator can
amend the matrix (delta protocol).

## Self-Verification Checklist (test implementer)

- [ ] All tasks above written; every scenario compiled & RED for the right reason (missing impl, not syntax)
- [ ] No implementation files touched or created (only `_test.go` + suite + manifest)
- [ ] Each unit test asserts behavior/state, not internals; no tautologies
- [ ] e2e exercises the live RPC/WS surface (scratch harness), not mocks
- [ ] Manifest `path_map` includes the delegate-seam suite + any new package
- [ ] No `t.Skip`, no stubbed-away assertions; no weakening of the stated Given/When/Then
- [ ] Do NOT commit; do NOT git add

**DO NOT COMMIT.** The orchestrator validates against the Test Gate matrix
and commits the gate.

## Completion Report Format

1. **Files touched:** every created/modified path (`_test.go`, `_suite.go`, manifest).
2. **Verify command + output:** for each task, the command and its RED tail (must show missing-impl failure, exit non-zero).
3. **Deviations / extra tests:** list any added-than-planned tests so the orchestrator can amend the matrix.

## Review Checklist (for the control agent — orchestrator, IN-SESSION)

- [ ] Tests RED for missing-implementation reasons only (no syntax errors)
- [ ] Every unit test mapped to a Test Gate matrix row (F1-F6) via its name; matrix fully VALIDATED before commit
- [ ] e2e suite runs against a scratch MEEPT_HOME harness over live RPC/WS; manifest path_map present
- [ ] Delta protocol applied: any plan-vs-test delta converged UP (lesser artifact updated to greater); deletions only with written rationale
- [ ] No assertion tautologies, no `assert x==x`, no mocks of the daemon surface it claims to exercise
- [ ] Test files ASCII-only, gofmt-clean

Output: VALIDATE (commit as `test(delegate): gate tests for 01-delegation (F1-F6)`) or a list of gaps.

## Notes

- Root AGENTS.md: new feature tests go in the hermetic e2e tier; the only in-package unit files are those the master/branch explicitly require (pure helpers). Keep that split — do not add unit files for the seam's user-visible surface.
- RED run guard: `go test` on an undefined symbol compiles and fails with an undefined-reference error — that is the expected RED, not a build-blocker. If a whole package is missing, drop it out of one `-run` scope rather than letting the compile error mask the other scenarios.
- The scratch daemon reaper (e2e/harness/daemon.go) cleans scratch bins; never reintroduce ownerless `os.MkdirTemp("", "meept-e2e-bin...")`.
