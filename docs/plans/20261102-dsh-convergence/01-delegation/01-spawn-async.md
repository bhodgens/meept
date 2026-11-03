# 01-spawn-async - Async Spawn + Child Handle + Join (Seam Core)

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests for this branch
> (00T-tests.md) ALREADY EXIST and are committed — your job is to make them
> pass; do NOT delete, skip, weaken, or rewrite any gate test (report
> suspected-wrong gate tests to the orchestrator instead). Do NOT commit —
> the orchestrator handles all git operations after review. Do NOT use
> read_file on existing source files — explore with search_files or terminal
> cat. After writing a file, do NOT read it back — write once and stop.
> Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** The delegation seam core: an async `DelegateManager` that spawns
  a child (spawn/fork/acp modes), returns a child handle + status immediately,
  registers the child in a per-session registry, publishes settlement to the
  parent, and exposes wait/join. Supersedes the synchronous `delegate_task`
  while keeping it byte-compatible for existing callers.
- **Dependencies:** 00T-tests (gate); B1 child-registry types
- **Estimated Context:** 85K
- **Concurrency Group:** B (dispatch alone first)
- **Audit references:** dsh-audit GAP 1a (spawn), 1c (ACP), 2 (child handle)

## Goal

Make meept's single synchronous in-process delegation into an async seam with
a durable child identity. `delegate_task` currently calls
`registry.RunAgent -> loop.RunOnce` (registry.go:940-947) synchronously and
returns a bare `DelegateResult{AgentID,Success,Response,Error}`
(platform.go:309-314). This leaf builds `internal/agent/delegate.go`: a
`DelegateManager` that spawns children in goroutines, returns `ChildID` +
status `running` immediately, records each child in a `sync.RWMutex`-guarded
per-session registry keyed by `childID`, and on settlement publishes a bus
event (`agent.delegate.result`) to the parent and updates the record. It also
adds a `Join` primitive (bounded wait) so a turn can fan out several
delegations then await them — meept's analogue of dsh `waitForChildren`
(manager.ts:252-290). Mode routing covers `spawn` (fresh in-process child),
`fork` (B2 seed — hook only in this leaf), and `acp` (route through the
existing `*acp.Manager`). The legacy `delegate_task` tool stays byte-compatible:
it may return `child_id`+`running` immediately (async) while keeping the
existing sync shape for callers that rely on it (review_tools.go:132 invokes
the same mechanism).

## Context

- `internal/tools/builtin/platform.go:271-425` — the existing `delegate_task`
  tool + `DelegateResult`. This leaf supersedes `Execute`'s synchronous path.
- `internal/agent/registry.go:940-947` — `RunAgent(ctx, agentID, message, conversationID) (string, error)`; the sync child. Async = call it in a goroutine, capture the string, publish a result.
- `internal/agent/registry.go` — `AgentRegistry` (`GetSpec`, `RunAgent`, `GetActiveQueue`).
- `internal/agent/dispatcher.go:4758` — `SteerActiveAgent(ctx, conversationID, content, source)` (for B3; this leaf needs the conversation id on the record so B3 can steer).
- `internal/agent/queue.go:227` — `MessageQueue.Steer`.
- `internal/agent/isolation.go:65-106` — `BuildSpawnContext(iso, brief, artifacts, memoryIDs, parent} ChatMessage)`. `IsolationSharedTranscript` for fork; `IsolationArtifactOnly` for the default fresh spawn.
- `internal/acp/manager.go:273,281` — `GetOrCreate` / `GetOrCreatePersona`; `internal/acp/session.go:262,303` — `Send` / `Cancel`. ACP route reuses these; do NOT build a second ACP client.
- `internal/tools/registry.go:81` — tool `Register`.
- `pkg/id/id.go:18` — `id.Generate(prefix)` for predictable-safe child ids (never UnixNano).
- `internal/agent/events.go:35,148` — `AgentEventType`/`TurnEndData` (the parent turn's shape; settlement reuses the bus topic spelling).

## Verified Anchors (grep-confirmed at authoring time)

- internal/tools/builtin/platform.go:272-307 — `NewDelegateTaskTool` + `Parameters` (agent_id/message/context/output_schema; this leaf ADDS mode/model/provider/reasoning_effort/depth params)
- internal/tools/builtin/platform.go:309-315 — `DelegateResult` struct (LEGACY shape to keep)
- internal/tools/builtin/platform.go:317-425 — `Execute` sync flow, `conversationID := id.Generate("delegate-")` at :381, `registry.RunAgent` at :384
- internal/agent/registry.go:940-947 — `RunAgent` -> `loop.RunOnce`
- internal/agent/registry.go:831-872 — `filterTools`; `CanDelegate` strips `delegate_task` when false — new tools must be gated by the same policy
- internal/agent/dispatcher.go:4758-4767 — `SteerActiveAgent` (child record stores the conversation id so B3 can steer)
- internal/agent/isolation.go:24-46 — `ContextIsolation` enum; `IsolationArtifactOnly` default, `IsolationSharedTranscript`
- internal/acp/manager.go:272-300 — session construction paths
- pkg/id/id.go:18 — `id.Generate(prefix string) string`

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/agent/delegate.go  (NEW)
type ChildState string // "pending"|"running"|"completed"|"interrupted"|"failed"
type ChildRecord struct {
    ChildID, AgentID, ParentSessionID, Mode, Model, Provider, ReasoningEffort string
    State ChildState; CreatedAt time.Time; Result, Err string
    ConversationID string  // steered by B3 via SteerActiveAgent
    Descriptor ChildDescriptor `json:"descriptor,omitempty"` // B6 writes it
}
type ChildOptions struct{ Provider, Model, ReasoningEffort string }
type DelegateManager struct{ /* reg *AgentRegistry */ }
func NewDelegateManager(reg *AgentRegistry, publisher func(string, any), cancelHook func(string) error) *DelegateManager
func (m *DelegateManager) Spawn(ctx, req DelegateRequest, parentSessionID string) (DelegateResult, error)
func (m *DelegateManager) Children(parentSessionID string) []ChildRecord
func (m *DelegateManager) Lookup(childID string) (ChildRecord, bool)
func (m *DelegateManager) Join(ctx, parentSessionID, childID string, timeout time.Duration) (DelegateResult, error)
func (m *DelegateManager) SetRouteResolver(f func(ChildOptions, DelegateRequest) (ChildOptions, error))
func (m *DelegateManager) SetForkSeed(f func(*Conversation) []llm.ChatMessage)
func (m *DelegateManager) SetDescriptorFactory(f func(ChildRecord) ChildDescriptor)
func (m *DelegateManager) SetPersist(f func(ChildRecord) error)
// internal/tools/builtin/platform.go  (MODIFY)
// DelegateTaskTool.Parameters gains: mode, model, provider, reasoning_effort, depth
// DelegateTaskTool gains a *DelegateManager; Execute routes through Spawn (async)
//   and keeps the legacy sync shape usable for existing callers.
```

### What This Leaf Consumes

- M-C1 DelegateRequest / DelegateResult (master C1) — restated in orchestrator.md; supply these types in delegate.go.
- B1 (own type) — ChildRecord/ChildState/DelegateManager; this leaf defines them.
- B2 hook — `SetForkSeed` (default nil → fresh); leaf 02 populates the real slicer.
- B4 hook — `SetRouteResolver` (default identity); leaf 04 pins per-child routes against allowlist.
- B6 hook — `SetDescriptorFactory` + `SetPersist` (default no-op); leaf 06 persists the descriptor for cold resume.

## Tasks

### T1: Define the seam core types + registry in delegate.go

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T1 | Define `ChildState`, `ChildRecord`, `ChildOptions`, `DelegateRequest`, `DelegateResult`, and the `DelegateManager` registry | Create: internal/agent/delegate.go; Test: internal/agent/delegate_test.go (gate) | `go test -p 2 ./internal/agent/ -run TestChildRecord -count=1` | `ChildRecord`/`ChildState`/`DelegateManager` undefined | Types compile; registry Lookup/Children/Join methods exist | Given a manager with an empty registry; When `Spawn` is called; Then a ChildRecord is created (State running) that `Lookup` returns and `Children` filters by parent |

T1 (expand): the gate test `TestChildRecord` in `internal/agent/delegate_test.go`
names these exact types — see 00T-tests T2. Implement:

```go
type ChildState string
const (ChildPending ChildState = "pending"; ChildRunning = "running"; ChildCompleted = "completed"; ChildInterrupted = "interrupted"; ChildFailed = "failed")
type ChildRecord struct {
    ChildID, AgentID, ParentSessionID, Mode, Model, Provider, ReasoningEffort, ConversationID string
    State ChildState
    CreatedAt time.Time
    Result, Err string
    Descriptor ChildDescriptor
}
type ChildOptions struct{ Provider, Model, ReasoningEffort string }
// DelegateRequest/DelegateResult per M-C1 (orchestrator.md), with json tags.
type DelegateManager struct {
    reg *AgentRegistry
    mu  sync.RWMutex
    children map[string]*childEntry  // childID -> entry{rec ChildRecord, settle func() <-chan struct{}}
    publisher func(topic string, payload any)
    cancelHook func(childID string) error
    routeResolver func(ChildOptions, DelegateRequest) (ChildOptions, error)     // B4
    forkSeed      func(*Conversation) []llm.ChatMessage                          // B2
    descriptorFactory func(ChildRecord) ChildDescriptor                          // B6
    persist       func(ChildRecord) error                                        // B6
}
```
Methods: `NewDelegateManager(reg, publisher, cancelHook) *DelegateManager`;
`Spawn`, `Children`, `Lookup`, `Join`, plus the Set* seam hooks. Seed every
setter with a typed-nil guard (nil keeps the default — a nil function field
must not panic the manager). Child id via `id.Generate("delegate-")`
(pkg/id/id.go:18). Verify pass as described above.

For a `Conversation`-typed fork seed: import `internal/agent` package local
`Conversation` (conversation.go). If a compile cycle would prevent that
signature landing here, declare the hook as
`func([]llm.ChatMessage) []llm.ChatMessage` (seed already materialized) and
note the deviation — but prefer the concrete `*Conversation` seam-slice so
leaf 02 can call `GetMessages()`.

### T2: Async Spawn (run child in goroutine + settle)

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T2 | `Spawn` returns child_id + status immediately and a goroutine runs the child, publishing settlement | Create: internal/agent/delegate.go; Test: e2e/suites/delegate-seam/delegate_seam_test.go (gate) | `make e2e-fast-area AREA=delegate-seam` | no child_id/settlement (async not built) | Spawn returns `{ChildID, Status:"running"}`; settlement arrives on the bus with the final response; record State=completed | Given a `Spawn` call; When execution completes; Then the parent receives an `agent.delegate.result` event carrying child_id+status+response and the record is completed |

T2 (expand):
1. Confirm the gate e2e `TestDelegate_SpawnAsync` REDs.
2. Implement `Spawn`:
   - Resolve route: `route, err := m.routeResolver(req.route(req.Model...), req)` (identity default; B4 later) — build `ChildOptions{Provider,Model,ReasoningEffort}` from `req`; on error return a `DelegateResult{Success:false, Error}` (never a Go error panic).
   - Check the spec exists via `m.reg.GetSpec(req.AgentID)`; error envelope if absent.
   - Validate depth: `req.Depth` default 1, never below guardedInvoke's `MaximumDepth` (subagent_pool.go:37-50) — cap at the registry's configured max.
   - `childID := id.Generate("delegate-")`; build the record (`Mode` default "spawn", State ChildRunning), stamp route fields, look up the child's conversation id (`delegate-`+childID or reuse), stash the entry, `publish` note nothing yet.
   - Launch `go m.runChild(ctx, spec, req, childID, record)`:
     - fork mode: if `m.forkSeed != nil`, seed the child via `BuildSpawnContext(IsolationSharedTranscript, message, nil, nil, seed)`; else `BuildSpawnContext(IsolationArtifactOnly, ...)` (default fresh spawn).
     - acp mode: `m.reg` is skipped; use an injected `*acp.Manager` route — see T4.
     - call `m.reg.RunAgent(ctx, spec.ID, fullMessage, conversationID)` — the string is the final response.
     - on return: lock, set State=completed + Result=response, release settle channel, `publish("agent.delegate.result", map[string]any{"child_id":cid,"status":"completed","response":resp})`, then `m.persist(record)` (B6 no-op default).
3. Implement `Join(ctx,parentSessionID,childID,timeout)`: wait on the child's settle channel (or poll the record under a `select` with ctx/timeout) and return the settled `DelegateResult` (+Status). Error `ErrChildNotFound`/timeout when not found/expired.
4. Keep all bus publishes OUTSIDE the record lock (never I/O under the mutex).
5. Verify pass via `make e2e-fast-area AREA=delegate-seam`.

`./bin/meept chat` smoke (optional, after build): a delegation returns a
child handle then the parent's next turn sees the settlement.

### T3: Expire lock discipline + concurrency safety

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T3 | Child registry safe under concurrent Spawn + concurrent Completes; no I/O under lock | Create: internal/agent/delegate.go (continue); Test: internal/agent/delegate_test.go (gate) | `go test -p 2 -race ./internal/agent/ -run TestChildRecord -count=1` + `make analyzers` | races / assert failures from shared map | `-race` clean; deterministic states | Given N concurrent Spawn calls and as many settled children; When each completes; Then the registry entries are consistent and each child's final State is exact |

Use a single `sync.RWMutex` around the `children` map; complete/decorate the
record under the write lock; publish + persist AFTER releasing. Run
`go test -p 2 -race ./internal/agent/ -run 'TestChildRecord' -count=1` and
`make analyzers` (mutexio) — must be clean.

### T4: ACP delegation provider (Mode "acp")

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T4 | Route `Mode:"acp"` through the existing acp.Manager as a delegation provider (reuse, no second client) | Modify: internal/agent/delegate.go; Test: e2e/suites/acp-wire (existing) or delegate-seam | `make e2e-fast-area AREA=delegate-seam` | acp mode errors "unsupported provider" | a delegated child routes through GetOrCreatePersona, returns a response, and Cancel()/Send() work (B3) | Given a `Spawn` with Mode=acp; When routed through the manager; Then the child's turn runs on the external harness and its response settles; a subsequent `subagent_send_message` reuses that session |

Use `*acp.Manager.GetOrCreatePersona(ctx, agentID, persona, workdir, opts)`
(manager.go:281) — the existing, tested path. Store the returned
`*acp.Session` (or a `SessionToken`) on the child record so B3 can
`Send`/`Cancel`. Inject the manager via a constructor field
(`acp *acp.Manager`) set when the daemon wires it; nil manager → `DelegateResult{Success:false, Error:"acp provider not wired"}`. No ambient secrets
cross to the child; the executor already scrubs env (internal/agent/acp_executor.go
+ acp/session.go) — reuse that scrubbing, do not reinvent.

### T5: Supersede delegate_task with byte-compatible async Execute

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T5 | Wire DelegateManager into the delegate_task tool; Parameters gain mode/model/provider/reasoning_effort/depth; Execute routes async but keeps the legacy sync shape for existing callers | Modify: internal/tools/builtin/platform.go, internal/tools/builtin/register_* (if needed); Test: e2e delegate-seam + existing turquoise delegation e2e | `go build ./...` + `make e2e-fast-area AREA=delegate-seam` | existing delegation e2e breaks (not byte-compatible) | existing callers unchanged; new params accepted; async spawn returns child_id+running; sync-only callers still receive the final Response | Given a caller that invokes delegate_task with only agent_id+message (legacy); When it runs; Then the response shape is unchanged from HEAD for existing suites, and an optional `mode:"spawn"` still returns child_id+status |

T5 (expand): modify `NewDelegateTaskTool(registry)` to
`NewDelegateTaskTool(registry, mgr *DelegateManager, acp *acp.Manager)` with a
non-nil `mgr` (nil keeps legacy sync Execute). In `Execute`:
- Parse new params into a `DelegateRequest`.
- If `mgr == nil` OR the synchronous path is requested explicitly (see below), fall through to the EXISTING sync flow (byte-compatible for review_tools.go:132).
- Else `return mgr.Spawn(ctx, req, parentSessionID)` where `parentSessionID` comes from the session/exec context (the calling agent's session is available through the tool's exec context — find the session-scoped seam and pass it; if not reachable, pass the tool's conversation id as the parent key and note it).
- Never drop `output_schema`: keep the JSON directive + `ValidateJSONSchema` for both sync and async paths (async: apply after the child settles).
- Preserve `CanDelegate` gating (registry.go:838) so the new params/tools don't bypass delegation policy.

Verify: `go build ./...`; `make e2e-fast-area AREA=delegate-seam`; existing
delegation suites still GREEN.

### T6: RPC + TUI/GUI parity + docs

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T6 | User-facing surface for the seam + docs/workflows/delegation.md, and a `delegate.list`/`delegate.status` CLI or /delegate TUI surface (root AGENTS.md wiring requirement) | Create: docs/workflows/delegation.md; Modify: cmd/meept/ (delegate list/status) OR internal/tui/ (delegate panel) | `make graphs-check` + `make docs-generate` (if gendoc drifted) | docs surface missing | a user can list/parse child status via CLI/TUI; docs describe spawn/fork/acp + settlement | Given a running daemon with children; When the user runs the delegate status surface; Then it reports child_id/status/parent for each active child |

The root AGENTS.md wiring requirement: every capability ships a user-facing
surface + agent wiring, or documents the deferral. At minimum ship:
- `meept delegate list` and `meept delegate status <child_id>` reading the
  manager (or the RPC handler that exposes it), OR a TUI panel.
- `docs/workflows/delegation.md` documenting the seam, the three modes,
  settlement, and the child-registry keys.
Defer the rich TUI/GUI panel with an explicit note if time-bounded, but the
CLI surface + doc must land in this leaf.

## Self-Verification Checklist

- [ ] All tasks implemented, gate tests passing (F1/F2 unit pins RED→GREEN, e2e GREEN)
- [ ] B1/M-C1 contracts satisfied exactly (ChildRecord/ChildState/DelegateManager/Spawn/Children/Lookup/Join signatures)
- [ ] All files at exact paths (delegate.go + platform.go); no changes beyond those + T6
- [ ] No deviations (or documented); no scope creep (no workflow tool, no control tools here — that's leaf 03/05)
- [ ] `go build ./...` + `go test -p 2 ./internal/agent/...` + `make analyzers` clean
- [ ] No `time.Now().UnixNano()`/`math/rand` ids; typed-nil guards on every Set* seam; no I/O under the record mutex
- [ ] Legacy delegate_task byte-compatible (existing delegation suites GREEN)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** every created/modified path.
2. **Verify command + output:** `go build ./...`; `go test -p 2 ./internal/agent/ -run 'TestChildRecord' -count=1`; `go test -p 2 -race ./internal/agent/ -run 'TestChildRecord' -count=1`; `make e2e-fast-area AREA=delegate-seam`; `make analyzers`; last ~20 lines each.
3. **Deviations:** none / list (esp. the per-child model pinning mechanics — see Orchestrator Open Question 1 — and the parentSessionID source).

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests untouched and GREEN
- [ ] B1/M-C1 signatures/types/paths match exactly
- [ ] Async spawn returns child_id+running immediately; settlement on bus; Join bounds correctly
- [ ] Legacy delegate_task byte-compatible; CanDelegate gating preserved
- [ ] Conventions: no ignored errors, no mutex-across-I/O, typed-nil guards, predictable-ID
- [ ] No scope creep; no debug artifacts / line-number corruption
- [ ] ACP route reuses *acp.Manager (no second client); no ambient secrets to the child

Output: APPROVED or specific gaps (file + line).

## Notes

- The concurrency contract is the hard part: `Spawn` returns before the
  child finishes; the child runs on a goroutine with its OWN context derived
  from a per-child cancel (B3 interrupts via `cancelHook`). Never hold the
  record mutex across `publish` or `persist`.
- `childID` vs `conversationID`: the child record keeps BOTH — `child_id` is
  the steer/interrupt key for B3; `ConversationID` is what `SteerActiveAgent`
  needs (dispatcher.go:4758).
- The `SetForkSeed`/`SetRouteResolver`/`SetDescriptorFactory`/`SetPersist`
  hooks are the seam for B2/B4/B6. They must be nil-safe and default-to-behavior
  — a leaf that never sets them must not change Spawn's byte behavior.
- dsh shape copied: the seam validates requested capabilities before any
  child exists and hands the whole lifecycle to a continuation manager. The
  analogous "validate before spawn" is the route resolver + spec-existence
  check in T2.
