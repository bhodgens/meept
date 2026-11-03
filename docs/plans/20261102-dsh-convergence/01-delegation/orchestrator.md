# 01-delegation - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this branch node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../../master.md
- **Children:** 6 impl leaves + 1 test-gate leaf
  (00T-tests, 01-spawn-async, 02-fork-seed, 03-child-control,
  04-model-selection, 05-workflow-tool, 06-descriptor-resume)
- **Scope:** The full delegation seam modeled on dsh (deepseek-harness):
  async spawn + child handle, per-child model/route selection, steer /
  interrupt / list-children control, history-seeded fork, a durable child
  descriptor with cold resume, and a model-authored workflow orchestration
  tool running in meept's confined goja codemode VM. ACP is reused as a
  delegation provider.

## Goal

Meept's delegation today is exactly ONE synchronous in-process call
(`delegate_task` -> `registry.RunAgent` -> `loop.RunOnce`) that returns a
bare string, exposes no child id, cannot be steered or interrupted per child,
inherits no parent context, and offers no per-child model selection. dsh
models subagents as a single provider-seam whose start API is capability-
validated and whose whole lifecycle (start, delivery, steer, interrupt,
cold-resume, disposal) is owned by a continuation manager. This branch closes
meept's delegation gaps (audit GAPs 1a, 1b, 2, 4, and part of 3) by building
that seam on top of the mechanism meept already has — the synchronous
`RunAgent`/`RunOnce` child -- refactored into an async manager that returns
a child handle immediately, publishes settlement to the parent, and joins a
fan-out. Master contracts C1/C2/C3 are the frozen surface; this branch is
their owner.

## Architecture

```
delegate_task (model tool) --async--> DelegateManager.Spawn
   |-> mode spawn: run agent in goroutine (persist-credit: runChild),
        settlement published on bus agent.delegate.result
   |-> mode fork : run agent seeded with parent's completed-turn prefix
   |-> mode acp  : route through existing *acp.Manager (GetOrCreatePersona)
   +-> returns child_id + status immediately; child record in per-session
        registry (ChildID -> ChildRecord)
Child control tools (subagent_send_message / subagent_interrupt /
   subagent_list_children) keyed by child_id -> registry + queue/steer + ACP
   cancel, mirroring dsh tool-subagent-control.
workflow (orchestration) tool: model-authored JS script -> goja codemode VM
   (no fs/net/timer/Node) -> fans out via DelegateManager.Spawn -> joins via
   the wait/join primitive -> returns a JSON value (or background job id).
Durable child descriptor + cold-resume: per-child record persisted so an
   idle/restarted daemon resumes an unfinished delegation.
```

Leaves are ordered so the seam core lands first and consumers key off its
frozen types.

## Interface Contracts

Cross-leaf contracts live here once. Leaves consume the exact signatures
below; they never restate full definitions. Master C1/C2/C3 (restated here
for the executing agent) are the authoritative surface.

### M-C1: Delegation seam (from master; Owner: this branch)

```go
// internal/agent/delegate.go (NEW seam; keeps byte-compatible legacy delegate_task)
type DelegateRequest struct {
    AgentID          string            `json:"agent_id"`
    Message          string            `json:"message"`
    Context          string            `json:"context,omitempty"`
    OutputSchema     json.RawMessage    `json:"output_schema,omitempty"`
    Mode             string            `json:"mode,omitempty"`   // "spawn"|"fork"|"acp" (default spawn)
    Model            string            `json:"model,omitempty"`  // alias or model ref
    Provider         string            `json:"provider,omitempty"`
    ReasoningEffort  string            `json:"reasoning_effort,omitempty"`
    Depth            int               `json:"depth,omitempty"`  // max nesting, default 1
}
type DelegateResult struct {  // EXPANDS today's platform.go:309-314
    AgentID string `json:"agent_id"`
    Success bool   `json:"success"`
    Response string `json:"response,omitempty"`
    Error    string `json:"error,omitempty"`
    ChildID  string `json:"child_id,omitempty"`   // NEW: steer/interrupt key
    Status   string `json:"status,omitempty"`     // running|completed|interrupted
}
```

### B1: Child registry + async spawn core (Owner: 01-spawn-async.md)

```go
// internal/agent/delegate.go
type ChildState string // "pending"|"running"|"completed"|"interrupted"|"failed"
type ChildRecord struct {
    ChildID         string        `json:"child_id"`
    AgentID         string        `json:"agent_id"`
    ParentSessionID string        `json:"parent_session_id"`
    Mode            string        `json:"mode"`             // spawn|fork|acp
    State           ChildState    `json:"state"`
    Model           string        `json:"model,omitempty"`
    Provider        string        `json:"provider,omitempty"`
    ReasoningEffort string        `json:"reasoning_effort,omitempty"`
    CreatedAt       time.Time     `json:"created_at"`
    Result          string        `json:"result,omitempty"`  // set on completed
    Err             string        `json:"err,omitempty"`     // set on failed
    Descriptor      ChildDescriptor `json:"descriptor,omitempty"` // B6 durable record
}
type ChildOptions struct { Provider, Model, ReasoningEffort string }

type DelegateManager struct { /* reg *AgentRegistry */ }
func NewDelegateManager(reg *AgentRegistry, publisher func(topic string, payload any), cancelHook func(childID string) error) *DelegateManager
func (m *DelegateManager) Spawn(ctx context.Context, req DelegateRequest, parentSessionID string) (DelegateResult, error)
    // async: returns CHILDID + Status="running" immediately; a goroutine
    // runs registry.RunAgent (spawn/fork) or the ACP provider (Mode acp)
    // and on settlement publishes bus topic "agent.delegate.result"
    // payload {child_id, status, response, error} and updates the record.
    // routeResolver field (default identity) lets 04-model-selection pin
    // the per-child route before runChild; see B4.
func (m *DelegateManager) Children(parentSessionID string) []ChildRecord
func (m *DelegateManager) Lookup(childID string) (ChildRecord, bool)
func (m *DelegateManager) Join(ctx context.Context, parentSessionID, childID string, timeout time.Duration) (DelegateResult, error)
    // bounded wait for the child to settle; polls the record / wedges on the
    // settlement channel; returns completed/interrupted/failed result.
func (m *DelegateManager) RouteResolver func(route ChildOptions, req DelegateRequest) (ChildOptions, error) // 04 sets via SetRouteResolver
// Consumers: 02-fork-seed (fork seed hook), 03-child-control (tools),
//   04-model-selection (route resolver), 05-workflow-tool (fan-out), 06-descriptor-resume (persist).
```

### B2: History-seeded fork (Owner: 02-fork-seed.md)

```go
// internal/agent/delegate_fork.go
// completedTurnPrefix mirrors dsh subagent-fork-in-process/index.ts:46-53:
// the seed is the parent session's messages up to and including its LAST
// completed turn; the in-flight turn is excluded. Returns empty when no turn
// has completed (child then starts fresh). Capture ONCE at spawn and store
// in the child's transcript (the seed rides the record's Descriptor).
func completedTurnPrefix(conversation *Conversation) []llm.ChatMessage
// DelegateManager.Spawn routes Mode=="fork" through this hook (a package var
// `forkSeedFunc`, default nil -> fresh), seeding the child via the existing
// BuildSpawnContext(IsolationSharedTranscript, ...) path (isolation.go:65).
// Consumers: 01-spawn-async (routing), B6 (seed durability in descriptor).
```

### B3: Child control tools (Owner: 03-child-control.md)

```go
// Model-facing tools registered in internal/tools/builtin (snake_case):
//   subagent_send_message{child_id, message} -> controller.SendMessage:
//       running child  -> Dispatcher.SteerActiveAgent(child.ConversationID,
//                        msg, source="subagent_send_message") (dispatcher.go:4758)
//       idle/finished  -> new turn via DelegateManager.Join + seed resume (B6)
//       acp child      -> *acp.Session.Send (acp/session.go:262)
//   subagent_interrupt{child_id} -> controller.Interrupt: mark interrupted +
//       cancel the child's run ctx (registry/queue path) or acp.Session.Cancel (acp/session.go:303).
//   subagent_list_children{parent_session_id?} -> controller.ListChildren
//       derived from the C1 child registry (DelegateManager.Children).
// Mirrors dsh tool-subagent-control/index.ts:29-112.
// Consumers: 05-workflow-tool (join), 02-goals branch (round driver).
```

### B4: Per-child model / route selection (Owner: 04-model-selection.md)

```go
// internal/agent/modelselect/modelselect.go
type ModelSelectionPolicy struct { Routes []AllowedModelRoute } // {Provider, Model string}
func ResolveChildRoute(parent ChildOptions, configured *ChildOptions, req DelegateRequest,
        policy *ModelSelectionPolicy, enabled bool) (*ChildOptions, error)
    // mirrors dsh model-selection.ts:99-128 requestedAgentOptions + :139-159
    // assertAllowedModelSelection. Provider+model must be supplied together
    // (delegation_model_request rule); empty string values rejected;
    // policy nil permits only the parent route; route changed + no effort ->
    // configured route-owned effort cleared. When no selection is requested,
    // returns configured (may be nil -> parent route).
func PreflightRoute(ctx context.Context, resolver *llm.Resolver, opts ChildOptions) error
    // dsh preflightChildLlmRoute (model-selection.ts:182-202): the route must
    // resolve through meept's resolver (HasAlias / provider reachable) BEFORE
    // the child spawns; errors visibly at the tool boundary.
// DelegateManager.SetRouteResolver wires this into Spawn (B1).
// Consumers: 01-spawn-async.
```

### B5: Workflow orchestration tool (Owner: 05-workflow-tool.md)

```json
// Model-facing "workflow" tool executing a confined program (master C3):
//   { engine: "dsh-archetype",
//     script: "<js>",  // top-level await; body hooks:
//       agent(prompt,{schema?,label?,phase?,provider?,model?}) -> child result|validate-object|null
//       pipeline(items, ...stages)  // per-item, no barrier
//       parallel(thunks)            // concurrency barrier
//       phase(title), log(message), args
//     max_concurrent_agents, max_total_agents, max_items_per_call,
//     sync_timeout_ms, run_in_background }
// Confinement: reuse meept's goja codemode VM (internal/codemode/sandbox.go:
//   sec note 3-14, New at :124) - NO fs/network/timer/Node APIs; "the agents
//   do the work". Caps FAIL-CLOSE (fatal, never narrower): mirror dsh
//   workflow-ptc/index.ts:106-112 defaults maxConcurrentAgents=0->
//   min(16,max(1,cores-2)), maxTotalAgents=1000, maxItemsPerCall=4096,
//   syncTimeoutMs=5000; an explicit value over the ceiling is INVALID_ARGUMENT.
// Fan-out via B1 DelegateManager.Spawn; join via B1 Join.
// Returns {kind:"foreground"|"background", runId, agentsStarted, result} or
//   {kind:"background", jobId, runId} respectively (tool-workflow/src/index.ts:378-408).
// stop reasons: completed | cancelled | error map to isError (index.ts:197-210).
// Result rendering capped (default 50000 chars, truncated with notice).
// Consumers: 02-goals branch MAY reuse the fan-out engine.
```

### B6: Durable child descriptor + cold resume (Owner: 06-descriptor-resume.md)

```go
// internal/agent/delegate_descriptor.go
type ChildDescriptor struct {
    Version          int    `json:"version"`           // 1
    Mode             string `json:"mode"`
    Provider         string `json:"provider,omitempty"`
    Label            string `json:"label,omitempty"`
    AgentProvider    string `json:"agent_provider,omitempty"` // local|acp
    AgentModel       string `json:"agent_model,omitempty"`
    Reasoning        string `json:"reasoning,omitempty"`
    ParentSessionID  string `json:"parent_session_id"`
    SeedPayload      string `json:"seed_payload,omitempty"`   // fork seed (B2), durable
    CreatedAt        time.Time `json:"created_at"`
}
// The descriptor is stamped onto ChildRecord.Descriptor at spawn (B1 field),
// persisted with the child record so an idle/restarted daemon can cold-resume
// an unfinished delegation (mode/provider/model/seed from the descriptor, no
// live Agent load needed). Durable store: child records persisted under the
// parent session (project into session storage + a DelegateManager.Persist()
// commit on the store the daemon already owns). Cold-resume entry:
//   DelegateManager.Resume(ctx, childID) -> re-spawns the child from its
//   descriptor (re-running the seed/fresh spawn, re-pinning model/route).
// Mirrors dsh subagent/descriptor (descriptor.ts:48-86,230-269) + manager.coldResume
// (manager.ts:1349-1404).
// Consumers: 01-spawn-async (record/descriptor fields), B3 (resume on send_message to idle child).
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00 | 00T-tests.md | test-gate leaf | none (Phase 0) | 60K | A |
| 01 | 01-spawn-async.md | leaf | 00T (gate) | 85K | B |
| 02 | 02-fork-seed.md | leaf | 01 (B1 core types) | 55K | C |
| 03 | 03-child-control.md | leaf | 01 (B1) | 65K | C |
| 04 | 04-model-selection.md | leaf | 01 (B1), B2 (route) | 55K | D |
| 05 | 05-workflow-tool.md | leaf | 01 (B1), 03 (B3 join) | 90K | D |
| 06 | 06-descriptor-resume.md | leaf | 01 (B1), 02 (B2 seed) | 60K | E |

Concurrency: 00T dispatches first (Phase 0). 01 dispatches alone (B) — it
owns the seam core file `internal/agent/delegate.go`, which everything else
keys off. 02 and 03 dispatch together (C, parallel, independent files).
04 and 05 dispatch together (D). 06 dispatches last (E) after 01+02 land the
descriptor fields and seed hook.

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Test Gate (before ANY implementation dispatch)

Dispatch 00T-tests.md first via `delegate_task`: "Write tests ONLY; do NOT
write or modify implementation files. Tests must fail RED for
missing-implementation reasons. Do NOT commit." Brief = full leaf text +
B1..B6 contracts INLINED + repo e2e rules (below). On return, the control
agent (orchestrator, main model, IN-SESSION — never delegated) validates
against the Test Gate matrix; apply the delta protocol (LESSER artifact
updates to the GREATER; test deletion needs a written rationale). When every
matrix row is VALIDATED, commit the gate tests:
`git commit -m "test(delegate): gate tests for 01-delegation (F1-F3)"`
and set the branch to TESTS_VALIDATED. Implementation may then dispatch.

Repo e2e rules (binding): suites in `e2e/suites/<name>/` with the `e2e`
build tag; manifest + `path_map` for any new package in the same commit;
scratch MEEPT_HOME harness (`harness.WithPreBootHook`); assert through the
live RPC/WS surface. New feature tests go in the hermetic e2e tier — no new
unit test files for user-visible features. Pure internal helpers that the
master explicitly requires unit tests for (fork-seed prefix slicing,
model-selection merge/assert, descriptor serialization) are exempt and may
host in-package `_test.go` files — the branch authorizes these explicitly.

### Phase 1: Dispatch Concurrency Group B (01-spawn-async)

Dispatch 01-spawn-async alone: goal = "Implement all tasks from
01-spawn-async.md". context = full leaf text + B1/M-C1 contracts + Coding
Conventions + relevant source INLINED (platform.go:271-425, registry.go:940-947,
dispatcher.go:4758, queue.go:227, isolation.go:65-106, acp/session.go:262,303,
acp/manager.go:273,281, id.Generate). Include the no-commit, no-git-add,
no-read_file-on-source, write-once-and-stop rules.

### Phase 2: Dispatch Concurrency Group C (02, 03)

Dispatch 02-fork-seed + 03-child-control in parallel after 01 is REVIEWED.
02 context = B1/B2 inlined + conversation.go:518, isolation.go:65-106,
fork index.ts source. 03 context = B1/B3 + dispatcher.go:4758, queue.go:227,
acp/session.go:262,303, manager.go:273, tool-subagent-control source. Each
carries the no-commit / corruption / write-once rules.

### Phase 3: Dispatch Concurrency Group D (04, 05)

Dispatch 04-model-selection + 05-workflow-tool in parallel after 01 and 03
are REVIEWED. 04 = B4 + registry.go:484-504 (modelRefForAgent), llm resolver
surface, model-selection.ts source. 05 = B1/B3/B5 + codemode/sandbox.go:3-14,
124-160, workflow-ptc workflow-ptc/index.ts:106-112, tool-workflow source.

### Phase 4: Dispatch Concurrency Group E (06-descriptor-resume)

Dispatch 06 after 01, 02, 03 REVIEWED. = B1/B2/B6 + session store surface
(the daemon-owned store for durable child records).

### Phase 5: Review and Commit Each Child

Review each child in-session (the main model — never a delegated reviewer).
Check leaf spec, contracts, Review Checklist; run the verify commands; the
gate-test diff must be empty. Pass: commit
(`git commit -m "feat(delegate): implement <leaf>"`) + status REVIEWED.
Gaps: re-dispatch with findings, max 3 cycles, then escalate to master.

### Phase 6: Integration Review

After ALL children REVIEWED: `go build ./...`, verify B1..B6 types line up
across the seam (child_id -> registry -> control -> resume), `make test`
(short, -p 2), the delegate-seam e2e suite in isolation, `make graphs` +
`make graphs-check` (tool surface changed), `scripts/config-drift-check.py`.
Pass: set children COMPLETE, report to master.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (B1..B6 signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Gate tests pass GREEN; `git diff` shows no gate-test modification
- [ ] Tests written and passing (e2e for features; unit pins for bug fixes)
- [ ] Code follows project conventions (no ignored errors, no mutex across
      I/O, typed-nil guards on wiring seams, predictable-ID prevention)
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders)
- [ ] No line-number corruption (`     N|` prefixes) in source files
- [ ] Every model-facing tool registered + agent wiring; docs/workflows/
      updated in the same commit (root AGENTS.md feature-doc rule)
- [ ] ACP route: no ambient secrets cross to the child; env scrubbed + child
      env merged on top (audit 4g; acp executor already scrubs)

Output: APPROVED or a list of specific gaps with file+line.

## Coding Conventions

- **Language:** Go 1.22+ (repo pins GOTOOLCHAIN; x/tools analyzers need it). No TypeScript — port the dsh logic archetype, not its runtime.
- **Naming:** exported/unexported per repo; model tools snake_case (`subagent_send_message`); dsh identifiers map to Go (`isConcurrencySafe` -> `IsConcurrencySafe`, `waitForChildren` -> `Join`).
- **Imports:** stdlib first, then third-party; no unused (U1000-strict).
- **Error handling:** wrap with context; no ignored errors; no bare panic(err); return DelegateResult{ToolResult} envelopes per the existing platform.go pattern.
- **Security:** predictable-ID prevention (pkg/id.Generate, never UnixNano); typed-nil guards on every Set*/wiring seam; scrub secrets in child env; fork seed MUST terminate at a completed turn (never inherit an in-flight tool-call turn).
- **Concurrency:** no mutex across I/O; the child registry is a `sync.RWMutex`-guarded map; settlement is a bus publish + one record write, never both under the lock.
- **Testing:** new feature tests in the hermetic e2e tier (e2e/suites/delegate-seam + manifest path_map same commit); pure helper unit tests in-package only where the master explicitly requires them (B2/B4/B6).
- **Threading:** caps are FAIL-CLOSE (fatal, never narrower); a config value over the engine ceiling is INVALID_ARGUMENT, not a clamp.
- **Formatting:** gofmt before reporting; ASCII only in new .md.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-tests | PENDING | 0 | Phase 0 gate |
| 01-spawn-async | PENDING | 0 | seam core, dispatch B |
| 02-fork-seed | PENDING | 0 | dispatch C |
| 03-child-control | PENDING | 0 | dispatch C |
| 04-model-selection | PENDING | 0 | dispatch D |
| 05-workflow-tool | PENDING | 0 | dispatch D |
| 06-descriptor-resume | PENDING | 0 | dispatch E |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: implementation children stay PENDING until 00T-tests reaches
TESTS_VALIDATED.

## Integration Test Plan

- `go build ./...` — GREEN.
- `go test -p 2 ./internal/agent/... ./internal/tools/... ./internal/acp/... ./internal/codemode/...` — GREEN (unit pins).
- `make e2e-fast-area AREA=delegate-seam` — the branch e2e suite GREEN.
- Cross-boundary (02-goals round driver joins a 01 child): the master-level
  e2e asserting a spawned round resumes a running child via
  `subagent_send_message`. Run from master Phase 3.
- `make graphs` + `make graphs-check` (tool surface changed); `scripts/config-drift-check.py` clean.
- Manual seam proof: `meept chat` delegates (spawn), lists children, steers
  one child, interrupts a second, forks with seed, runs a `workflow` script.

## Test Gate

Test leaves: 00T-tests.md (dispatched Phase 0; covers F1-F3 unit + the
delegate-seam e2e suite). Validated by the control agent (orchestrator,
in-session, never delegated) against the matrix below; delta protocol:
LESSER artifact updates to GREATER.

| # | Feature (from plan) | Source (leaf/spec) | Unit tests | E2E tests | Status |
|---|---------------------|--------------------|-----------|-----------|--------|
| F1 | async spawn + child id + join | 01-spawn-async (B1) | ChildState/Lookup/Children pins | e2e_delegate_spawn_async:1 | PENDING |
| F2 | history-seeded fork | 02-fork-seed (B2) | CompletedTurnPrefix slices + drop in-flight turn | e2e_delegate_fork_seed:1 | PENDING |
| F3 | child control (send_message/interrupt/list) | 03-child-control (B3) | registry lookup pins | e2e_delegate_steer_interrupt:1 | PENDING |
| F4 | per-child model/route selection | 04-model-selection (B4) | ResolveChildRoute merge + assert + preflight | e2e_delegate_model_route:1 | PENDING |
| F5 | workflow orchestration tool + confinement | 05-workflow-tool (B5) | caps fail-close pins (engine ceiling) | e2e_delegate_workflow:1 | PENDING |
| F6 | durable descriptor + cold resume | 06-descriptor-resume (B6) | descriptor version round-trip | e2e_delegate_cold_resume:1 | PENDING |

Statuses: PENDING | DELTA | VALIDATED.

Delta log: [date, feature ID, direction, one-line reason]

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda (dated, appended to the dispatch brief). Never edit a
dispatched leaf document mid-execution.

## Open Questions

1. Per-child model pinning mechanics: `modelRefForAgent` binds a loop to its
   spec at construction (registry.go:484-504). A per-child route change for
   a spawn needs either a transient per-delegation AgentSpec (Model/Provider
   override) or a request-scoped `llm.WithModelOverride`-style override
   threaded through `RunAgent`. Leaf 04's `ResolveChildRoute` + `PreflightRoute`
   validate and resolve the route; leaf 01's `runChild` applies it via the
   route it resolves. The implementer should prefer the request-scoped
   override lane over mutating shared registry spec state (never mutate the
   shared spec — the registry is shared across sessions). Unresolved
   mechanics surface at leaf 01/04 review; escalate to master if the two
   contracts cannot meet cleanly.
2. Descriptor persistence store: meept's session store (internal/session)
   survives via sqlite. Child records may persist in a small side table the
   daemon-owned store already maps, or ride the session store's durable
   storage record-change events (branch 03). Prefer the latter to avoid a
   parallel persistence mechanism; defer any hard dependency on branch 03's
   exact SQL to leaf 06 review and reconcile then.
3. ACP provider AUTH: no ambient secrets cross to a child; the acp executor
   already scrubs env and merges explicit child env. Confirm the new ACP
   delegation route reuses `*acp.Manager.GetOrCreatePersona` so the model /
   permission relay is unchanged; do not build a second ACP client.

## Notes

- Delegation supersedes the synchronous delegate_task tool while keeping it
  byte-compatible for existing callers (master D1, audit 1a): the tool may
  still return the child's final response in a sync-only caller or, once
  async, immediately return child_id + status "running" with settlement
  delivered to the parent on the bus. Leaf 01 must keep the legacy shape for
  existing callers (review_tools.go:132 invokes the reviewer through the
  same mechanism).
- This whole branch SOURCES from the dsh audit (HEAD ce7073d, deliverables
  /tmp/dsh-audit/). The audit's file:line is the re-read point, not the final
  say — grep the actual dsh source before porting any threshold (leaf briefs
  do this; the orchestrator re-verifies constants at review).
- dsh defaults (verified from source): maxDepth=1, maxActiveSubagents=8
  (subagent/index.ts:196-197); workflow maxTotalAgents=1000,
  maxItemsPerCall=4096, syncTimeoutMs=5000, maxConcurrentAgents=0->
  min(16,max(1,cores-2)) (workflow-ptc/index.ts:106-112); workflow result
  render cap 50000 chars (tool-workflow/index.ts:61). Codex permission
  default 'never', Claude 'dontAsk' — out of scope (no codex/claude adapters,
  audit 1d/1e VALUE low).
- Depth cap: meept already enforces MaximumDepth via guardedInvoke
  (subagent_pool.go:37-50); DelegateRequest.Depth must not widen it, and
  must default to 1 per dsh.
