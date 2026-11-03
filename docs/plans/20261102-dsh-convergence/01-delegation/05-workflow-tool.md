# 05-workflow-tool - Model-Authored Orchestration Tool (Confined Runtime)

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests for this branch
> (00T-tests.md) ALREADY EXIST and are committed — your job is to make them
> pass; do NOT delete, skip, weaken, or rewrite any gate test (report
> suspected-wrong gate tests to the orchestrator instead). Do NOT commit —
> the orchestrator handles all git operations after review. Do NOT use
> read_file on existing source files — explore with search_files or terminal
> cat. After writing a file, do NOT read it back — write once and stop.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** A model-facing `workflow` (orchestration) tool that executes a
  confined JavaScript program whose helpers fan out subagents via the C1
  delegation seam and join them, returning a JSON value (or a background job
  id). Confinement reuses meept's goja codemode VM (master C3). Implements B5.
- **Dependencies:** 01-spawn-async (B1), 03-child-control (B3 join), 00T-tests
  (gate F5)
- **Estimated Context:** 90K (largest leaf — confinement + engine + tool)
- **Concurrency Group:** D (parallel with 04, after 01+03 REVIEWED)
- **Audit references:** dsh-audit C3 (workflow), tool-workflow + workflow engine + ptc-runtime

## Goal

dsh ships a model-facing `workflow` tool that runs a confined JavaScript
script with `agent()`/`pipeline()`/`parallel()`/`phase()`/`log()` helpers;
the script fans work out across many subagents and returns a JSON value, with
caps enforced FAIL-CLOSE (workflow-ptc/index.ts:106-112 + tool-workflow/index.ts).
The script has "no filesystem, network, timer, or Node.js APIs; the agents do
the work" (tool-workflow/index.ts:158-166). This leaf ports the shape into
meept: a `workflow` (or `orchestrate`) tool whose script body runs inside
meept's existing goja codemode VM (internal/codemode/sandbox.go — no fs/net/
timer/Node), with the delegated helpers implemented as goja `ToolBridge`
calls that route through B1 `DelegateManager.Spawn` and take the caps
FAIL-CLOSE (a cap trip is a fatal tool error, never a silently narrower
config). Returns `{kind:"foreground"|"background", runId, agentsStarted,
result}` (or `{kind:"background", jobId, runId}`). Consumers: 02-goals branch
MAY reuse the fan-out engine.

## Context

- `internal/codemode/sandbox.go:3-14,124-160` — the goja VM, its `New(opts)`,
  and its hard security contract: ONLY goja + goja_nodejs/eventloop are
  wired; `require()` has an empty native table that rejects every path,
  `process`/`fetch`/`XMLHttpRequest`/`WebSocket` are undefined. A future dev
  importing node-extension modules breaks the no-fs gate. The workflow
  engine MUST stay inside these bounds.
- `internal/codemode/` ToolBridge + Tool bridges (sandbox.go:69,122) — how a
  goja script calls into Go. The workflow helpers (`agent`, `pipeline`,
  `parallel`, `phase`, `log`, `args`) are implemented as ToolBridge calls, NOT
  Node APIs.
- `internal/agent/delegate.go` (01) — `DelegateManager.Spawn`/`Children`/`Join`.
- `internal/tools/builtin/` — where the `workflow` tool registers.
- dsh reference: `/tmp/deepseek-harness/packages/workflow/workflow-ptc/src/index.ts`
  (engine caps + start), `tool-workflow/src/index.ts` (tool schema + result
  shape + stopReason → isError), `workflow/workflow/src/runtime-types.ts`
  (WorkflowStartRequest/WorkflowRun), `ptc-runtime/ptc-runtime/src/index.ts`
  (REJECTED_BINDING_GLOBALS, reserved words, budget concepts).

## Verified Anchors (grep-confirmed at authoring time)

- internal/codemode/sandbox.go:124 — `func New(opts Options) (*Sandbox, error)` (the VM the engine reuses)
- internal/codemode/sandbox.go:3-14 — the no-fs/net/timer security contract (must not be loosened)
- internal/codemode/sandbox.go:69,122 — `Sandbox` struct + `ToolBridge` wiring seams
- internal/agent/delegate.go (01) — `DelegateManager.Spawn/Join/Children` (B1)
- internal/queue/queue.go / scheduler — `jobs` primitive for `run_in_background` (the daemon's existing job/queue seam; reuse it, do not build a second job registry)
- dsh workflow-ptc/index.ts:106-112 — engine Config defaults (maxConcurrentAgents=0->min(16,max(1,cores-2)), maxTotalAgents=1000, maxItemsPerCall=4096, syncTimeoutMs=5000)
- dsh tool-workflow/index.ts:378-408 — output oneOf {foreground|background}, result render cap 50000 (index.ts:61)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/tools/builtin/delegate_workflow.go  (NEW)
// Model-facing "workflow" tool:
//   parameters: { script (required string, top-level await, `return <value>`),
//                 meta { name, description, whenToUse?, phases? },
//                 args?, max_concurrent_agents?, max_total_agents?,
//                 max_items_per_call?, sync_timeout_ms?, run_in_background? }
//   outputs (oneOf):
//     { kind:"foreground", runId, agentsStarted, result:<json> }
//     { kind:"background", jobId, runId }
// Body hooks (script-visible): agent(prompt,{schema?,label?,phase?,provider?,model?}),
//   pipeline(items,...stages), parallel(thunks), phase(title), log(msg), args.
// Confinement: script runs in the codemode goja VM (no fs/net/timer/Node);
//   helpers are ToolBridge calls.
// Caps FAIL-CLOSE: a cap trip (max_total_agents, max_items_per_call) or
//   misused hook (unknown option, unsupported schema) ENDS the script as a
//   fatal error, never a narrower config.
// stop reasons: completed | cancelled | error -> cancelled/error become
//   tools.NewErrorResult (tool-workflow/index.ts:197-210).
// Result rendering capped (default 50000 chars, truncated with notice).

// internal/codemode/  (NEW files if the workflow engine needs its own seam —
//   REUSE the existing Sandbox/ToolBridge; only add workflow helper bridges)
// internal/tools/builtin/delegate_workflow.go wires the engine to the tool.
```

### What This Leaf Consumes

- B1 (01) — `DelegateManager.Spawn` (fan-out), `Join`/`Children` (join)
- B3 (03) — control semantics for interrupting a workflow's children on cancel (via the manager cancel hook)
- M-C3 (master) — the workflow contract {engine:"dsh-archetype", script, caps, run_in_background}
- codebase codemode — Sandbox/ToolBridge confinement contract

## Tasks

### T1: Parse + validate script/meta (host-side), FAIL-CLOSE on parse errors

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T1 | Host-side script parse + meta-vocabulary validation before any execution; `SCRIPT_PARSE`/`META_INVALID` are fatal | Create: internal/tools/builtin/delegate_workflow.go (tool + engine entry); Test: e2e delegate-seam (`TestDelegate_Workflow`) | `make e2e-fast-area AREA=delegate-seam` | tool not registered / parse not rejected | a script with `export const meta` or bad JS returns a `SCRIPT_PARSE`-class isError before running; a distractor `pipeline()` with a bad option trips META_INVALID | Given a malformed script (an `export const meta` statement, dsh workflow-ptc/index.ts:49-57); When the tool executes; Then it returns a fatal error (no child runs) |

T1 (expand):
1. Confirm the gate workflow e2e REDs.
2. Define `validateScript(script, meta)`:
   - Reject a body carrying a Claude-Code-style `export const meta` statement (dsh workflow-ptc/index.ts:49 META_STATEMENT regex).
   - Parse-check the body as async JS (host-side `goja` compile, as dsh uses `vm.Script` at :61) — a syntax error => `SCRIPT_PARSE` fatal, before any child.
   - Validate `meta` (name kebab-case non-empty, description non-empty, optional phases each `{title, detail?, provider?, model?}`) => `META_INVALID` fatal.
3. Register the tool with `Name()=="workflow"`, category `delegation`, `Description()` embedding the helper contract (agents do the work; caps fail-closed).

### T2: Confined engine — goja VM with ToolBridge helpers + FAIL-CLOSE caps

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T2 | Run the script in the codemode goja VM; provide `agent/pipeline/parallel/phase/log/args` as ToolBridge calls; caps trip fatally | Modify: internal/codemode/ (add workflow helper bridges or engine), internal/tools/builtin/delegate_workflow.go; Test: e2e delegate-seam | `make e2e-fast-area AREA=delegate-seam` | helpers undefined / Node APIs leak | `agent()` spawns a child via B1, resolves its completed result (or `null` on child failure); a script calling `require("fs")`/`process` returns an isError (no fs/net/timer); a cap trip fails loudly | Given a workflow script with `agent()` calls; When it runs foreground; Then it returns `{kind:"foreground", runId, agentsStarted≥1, result:<json>}` and a script using a Node API fails closed |

T2 (expand):
1. Reuse `internal/codemode` Sandbox (sandbox.go:124 `New(opts)`). Do NOT add node-extension modules; the no-fs gate (sandbox.go:3-14) is a hard invariant. Wire the workflow helpers as `ToolBridge` functions (sandbox.go:69,122) so the script's `agent(x)` maps to `DelegateManager.Spawn`+`Join`:
   - `agent(prompt, opts)`: `Spawn` a `fork`-or-`spawn` child, `Join` it, resolve to the JSON value (with `opts.schema` object-rooted, validate) or `null` if the child failed. `opts.provider/model` feed the child route (B4).
   - `pipeline(items, ...stages)`: per-item through the stages, no barrier; a stage throw nulls that item and skips its remaining stages.
   - `parallel(thunks)`: run zero-arg thunks concurrently, wait for ALL (barrier); a throwing thunk resolves null.
   - `phase(title)`, `log(msg)`, `args`: narration + the tool's `args` verbatim.
   - Misused hook (bad args, unsupported schema) ENDS the script as fatal — mirror dsh "Misused hooks ... end the whole script instead of producing null" (tool-workflow/index.ts:164-166).
2. Caps from workflow-ptc defaults (index.ts:106-112) — FAIL-CLOSE: `max_concurrent_agents` default `min(16, max(1, NumCPU()-2))`, `max_total_agents` 1000, `max_items_per_call` 4096, `sync_timeout_ms` 5000. An explicit value over the engine ceiling is `INVALID_ARGUMENT`, never clamped. A concurrent/total/item cap trip is `AGENT_CAP`/`ITEM_CAP` fatal (workflow/src/index.ts:108-118). HARD: the goja VM runs the script with the codemode memory/alloc accounting (bridge.go countAlloc path) so a runaway script is interrupted, not a hang.
3. Verify via the workflow + confinement e2e scenarios (incl. a `require("fs")` script that must isError).

### T3: foreground vs background run

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T3 | `run_in_background` returns a job id immediately (or the daemon's job/queue seam); foreground awaits result | Modify: internal/tools/builtin/delegate_workflow.go (continue); Test: e2e delegate-seam | `make e2e-fast-area AREA=delegate-seam` | background path returns no jobId | foreground returns the JSON result; background returns `{kind:"background", jobId, runId}` and the value arrives with the completion notice | Given a workflow with `run_in_background:true`; When the tool runs; Then it returns a background job id immediately and no partial result; the value rides the job completion |

T3 (expand): implement `run_in_background` via the daemon's EXISTING
queue/job seam (internal/queue + scheduler deferred-turn mechanism; do not
build a second job registry). Foreground awaits the run's `result` and always
disposes (dispose waits for script + child cleanup). A job kill/cancel maps to
`run.cancel` which interrupts the workflow's children (via B3 cancelHook).
Background settlement maps stopReason onto the job outcome vocabulary
(`completed`->result, `cancelled`->killed, `error`->failed —
tool-workflow/index.ts:219-236).

### T4: Result rendering cap + stopReason → isError

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T4 | Cap the rendered result (default 50000 chars, truncated with notice); non-completed stop reasons become isError | Modify: internal/tools/builtin/delegate_workflow.go (continue); Test: e2e delegate-seam | `make e2e-fast-area AREA=delegate-seam` | over-cap result not truncated / cancelled not an error | a >50k JSON result truncates with a notice; a cancelled/errored run returns an isError naming the stop reason | Given a workflow whose return value exceeds the render cap; When it settles completed; Then the tool returns a truncated result with a truncation notice; a cancelled run returns an isError with the reason |

T4 (expand): port `stopReasonError` (tool-workflow/index.ts:197-210):
`completed`=>ok; `cancelled`=>"run was cancelled (...)" isError;
`error`=>"run failed (...)" isError. Port `renderResult` cap +
truncation notice (index.ts:239-246, maxResultChars default 50000).

### T5: RPC + TUI/GUI parity + docs

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T5 | User-facing surface for the workflow tool + docs | Modify: docs/workflows/delegation.md (workflow section); cmd/meept/ or internal/tui/ (workflow status/listing), or defer with note | `make graphs-check` + `make docs-generate` (if gendoc drifted) | the workflow tool has no UI-to-drive surface | a user can see workflow runs (foreground/background) and kill a background run via CLI/TUI; docs describe script + caps | Given a running background workflow; When the user lists runs; Then they see its job/run id and status and can stop it |

Root AGENTS.md wiring requirement: ship a UI/CLI/TUI path to see + kill
background workflows (e.g. `meept delegate workflow list|kill <job>`), OR
document the deferral. `docs/workflows/delegation.md` gains the workflow
section (script helper contract, caps, confinement, result shape). Defer a
rich GUI panel with an explicit note if time-bounded; the CLI + doc must land.

## Self-Verification Checklist

- [ ] Script/meta validated host-side (SCRIPT_PARSE/META_INVALID fatal) before any child
- [ ] Script runs in the codemode goja VM (no fs/net/timer/Node leakage); `require("fs")` isError; allocation accounting interrupts a runaway
- [ ] Helpers agent/pipeline/parallel/phase/log/args are ToolBridge calls over B1
- [ ] Caps FAIL-CLOSE (explicit over-ceiling = INVALID_ARGUMENT; trip = fatal), never narrower
- [ ] stopReason maps completed/cancelled/error; result render capped 50000
- [ ] background path returns jobId immediately; foreground awaits and disposes
- [ ] No scope creep: no goal-tools / no plan-mode (branch 02), only the delegation fan-out engine
- [ ] Length: this is the largest leaf — if e2e coverage grows, stop after the mandatory scenarios and REPORT, do not gold-plate

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** every created/modified path.
2. **Verify command + output:** `go build ./...`; `make e2e-fast-area AREA=delegate-seam`; last ~20 lines.
3. **Deviations:** none / list (esp. which codemode files were touched, whether a new engine seam vs inline bridges, how background maps onto the existing queue/job seam).

## Review Checklist (For Review Agent)

- [ ] Confinement invariant enforced: the workflow engine adds NO fs/net/timer/Node to the codemode VM (sandbox.go:3-14 unchanged)
- [ ] Caps FAIL-CLOSE everywhere; explicit over-ceiling is INVALID_ARGUMENT; misused hook ends the script fatally
- [ ] Helpers fan out via B1 Spawn + Join; a child failure resolves `null` (not a script abort); fatal workflow errors re-throw
- [ ] stopReason before tool return; result render cap; foreground always disposes; background uses the daemon job seam (no second job registry)
- [ ] No scope creep into goals/plan-mode; docs updated
- [ ] Conventions; no debug artifacts / line-number corruption

Output: APPROVED or specific gaps (file + line).

## Notes

- The single hardest invariant is CONFINEMENT: the workflow script must be
  unable to touch fs/net/timer even if the model writes a malicious body. The
  codemode sandbox already delivers this (sandbox.go:3-14); this leaf only
  ADDs the helpers as ToolBridge bridges, it never loosens the VM's globals.
- `02-goals` MAY reuse this fan-out engine for its autonomous round driver
  (master C4). Keep the engine entry points (start/join/cancel/result) in a
  form branch 02 can call directly, not only behind the tool's parameter
  schema — export the engine surface.
- dsh result vocabulary (`agentsStarted`, `runId`, stopReason) is the wire
  shape; keep it so branch 02's driver and any client tooling read the same
  fields.
