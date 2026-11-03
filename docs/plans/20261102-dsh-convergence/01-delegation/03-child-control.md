# 03-child-control - subagent_send_message / subagent_interrupt / subagent_list_children

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
- **Scope:** The child-control surface for the delegation seam: model-facing
  tools `subagent_send_message`, `subagent_interrupt`, `subagent_list_children`
  keyed by the C1 child registry (B1) + queue/steer + ACP cancel. Implements B3.
- **Dependencies:** 01-spawn-async (B1 registry/manager), 00T-tests (gate F3)
- **Estimated Context:** 65K
- **Concurrency Group:** C (parallel with 02, after 01 REVIEWED)
- **Audit references:** dsh-audit GAP 2 (child control surface), tool-subagent-control

## Goal

dsh exposes steer/interrupt/list via the model tools `send_message`,
`interrupt_agent`, and `list_agents`, all keyed to the continuation manager
(tool-subagent-control/index.ts:29-112). meept has steering
(`Dispatcher.SteerActiveAgent` dispatcher.go:4758 -> `MessageQueue.Steer`
queue.go:227) but it is keyed to a whole agent/conversation, NOT to a
delegated child, and `delegate_task` returns no child id so a parent cannot
steer the exact child it spawned. This leaf adds three model-facing control
tools that resolve `child_id` through the B1 child registry, then route to the
right primitive: a `running` child is steered via its recorded conversation
(`SteerActiveAgent`), an `acp` child via `*acp.Session.Send`, an idle/
interrupted child is continued via a new turn (resume seam, B6), and an
interrupt marks the record Interrupted + cancels via the manager's
`cancelHook` (or `acp.Session.Cancel`). Listing derives directly from
`DelegateManager.Children`.

## Context

- `internal/tools/builtin/` — where the new `delegate_control.go` tool lives; registration mirrors the existing `acp_agent` pattern (builtin/acp_register_tools.go) and the `delegate_task` tool (platform.go:271).
- `internal/agent/dispatcher.go:4758-4767` — `SteerActiveAgent(ctx, conversationID, content, source)`; `ErrQueueNotFound` when no active queue.
- `internal/agent/queue.go:227-262` — `MessageQueue.Steer(content, source)`; steering replaces the latest (single-slot), never appends.
- `internal/agent/delegate.go` (01) — `DelegateManager.Children/Lookup/Spawn`; `cancelHook`; child records carry `ConversationID` + `Mode`.
- `internal/acp/session.go:262,303` — `(*Session).Send(ctx,text)` / `(*Session).Cancel()` for acp-mode children.
- dsh reference: `/tmp/deepseek-harness/packages/subagent/tool-subagent-control/src/index.ts` (send_message/interrupt semantics) + `list-agents.ts`.

## Verified Anchors (grep-confirmed at authoring time)

- internal/agent/delegate.go (01) — `ChildRecord{ChildID, AgentID, ParentSessionID, Mode, State, ConversationID}`, `DelegateManager.Children/Lookup`
- internal/agent/dispatcher.go:4758-4767 — `SteerActiveAgent(ctx, conversationID, content, source) error`
- internal/agent/queue.go:227-262 — `MessageQueue.Steer(ctx, content, source)`
- internal/acp/session.go:262 — `(*Session).Send(ctx, text) (string, error)`; :303 — `(*Session).Cancel() error`
- internal/tools/builtin/acp_register_tools.go:25-86 — tool-provider registration pattern (permission-classed)
- internal/tools/builtin/platform.go:271-307 — delegate_task tool construction/params (parent tool reads same registry)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/tools/builtin/delegate_control.go  (NEW)
// Model-facing tools registered on the registry:
//   subagent_send_message{child_id, message}   -> Result{message_id} (delivery ack, not the answer)
//   subagent_interrupt{child_id}               -> Result{accepted:true} (fire-and-return)
//   subagent_list_children{parent_session_id?} -> Result{children:[{child_id, agent_id, mode, state, model}]}
// Controller:
type DelegateController struct{ mgr *DelegateManager; dsp *Dispatcher; acp *acp.Manager }
func NewDelegateController(mgr *DelegateManager, dsp *Dispatcher, acp *acp.Manager) *DelegateController
func (c *DelegateController) SendMessage(ctx, childID, message string) (any, error)
func (c *DelegateController) Interrupt(ctx, childID string) (any, error)
func (c *DelegateController) ListChildren(ctx, parentSessionID string) (any, error)
```

### What This Leaf Consumes

- B1 (01-spawn-async) — `DelegateManager.Children/Lookup`, `cancelHook`, child records
- M-C1 — child_id/status vocabulary
- B6 (06) — the resume seam for continuing an idle/interrupted child (this leaf calls `Resume`; declare it so only if 01/06 agree — otherwise SendMessage to an interrupted local child steers its existing conversation; reconcile at review)

## Tasks

### T1: SendMessage — steer running child / continue idle child

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T1 | `subagent_send_message` delivers to the right child; running -> steer, acp -> Session.Send, idle/interrupted -> resume | Create: internal/tools/builtin/delegate_control.go; Test: e2e delegate-seam (`TestDelegate_Steer`) | `make e2e-fast-area AREA=delegate-seam` | tool not registered / steer misses child | a running child receives the message at its next step and its next result reflects the steer; delivery returns `message_id` | Given a running delegated child; When the parent calls subagent_send_message{child_id, message}; Then the child's next step consumes the steer and the tool returns a delivery ack (not the answer) |

T1 (expand):
1. Confirm the gate e2e `TestDelegate_Steer` REDs.
2. Implement `NewDelegateController(mgr, dsp, acp)` and `SendMessage`:
   - `rec, ok := c.mgr.Lookup(childID)`; if `!ok` return a `tools.NewErrorResult("child not found ...")` envelope.
   - `state := rec.State`:
     - `running`: if mode is `acp` and `c.acp != nil`, call the recorded session `Send(ctx, message)`; else `dsp.SteerActiveAgent(ctx, rec.ConversationID, message, "subagent_send_message")` — returns `ErrQueueNotFound` if no active queue -> envelope error.
     - `interrupted` / `completed` / `idle` (State not set yet): continue via B6 `Resume(childID)` then steer (or, if Resume is not yet present, return a clear "child is not active; use a fresh spawn" message — do NOT silently drop). Mark delivery with a `message_id` (id.Generate("delegate-msg-")), not the child's answer.
   - Follow dsh semantics: "A working agent receives it at its next step; an idle agent starts a new turn with it. Returns delivery confirmation, not the agent's answer." (tool-subagent-control/src/index.ts:29-58).
3. Register the tool with `Name()=="subagent_send_message"`, category `delegation`, params `{child_id required string, message required string}`.
4. Verify `make e2e-fast-area AREA=delegate-seam`.

### T2: Interrupt — fire-and-return cancel

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T2 | `subagent_interrupt` cancels a descendant child; returns `accepted:true` without waiting | Create: internal/tools/builtin/delegate_control.go (continue); Test: e2e delegate-seam (`TestDelegate_Interrupt`) | `make e2e-fast-area AREA=delegate-seam` | interrupt not routed to the child | the child transitions to interrupted; the tool returned accepted:true immediately | Given a running delegated child; When the parent calls subagent_interrupt{child_id}; Then it returns `{accepted:true}` and the child's State becomes interrupted (settlement carries status interrupted) |

T2 (expand): implement `Interrupt`:
- `Lookup(childID)`; unknown -> error envelope.
- Set the record State -> ChildInterrupted under the manager lock.
- Route the cancel: acp mode -> `c.acp` session `Cancel()`; local mode -> the 01 `cancelHook(childID)` that cancels the child's run context (the manager wires it at construction — see 01 T4). If `dsp` has a direct cancel seam for the child's conversation (`FollowUpActiveAgent` is the reverse — do not use it; prefer cancelHook), use the hook.
- Return `{accepted:true}` immediately. Auth note (dsh manager.ts:183-228): only the direct parent (record.ParentSessionID == caller) or an ancestor may interrupt — enforce this in the tool by comparing the caller's session against the child's `ParentSessionID` lineage.
- Verify via the e2e interrupt scenario.

### T3: ListChildren — derive from the registry

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T3 | `subagent_list_children` enumerates a parent's children from the C1 registry | Create: internal/tools/builtin/delegate_control.go (continue); Test: e2e delegate-seam | `make e2e-fast-area AREA=delegate-seam` | monitor lists empty / tool unregistered | list returns child_id/agent_id/mode/state/model for every child of the caller | Given a parent with two delegated children; When the parent calls subagent_list_children; Then it returns both children with their current states |

T3 (expand): implement `ListChildren`:
- `parentSessionID` default = the caller's session (the binding registry key from 01); if absent in args, resolve from the exec context session.
- `children := c.mgr.Children(parentSessionID)`; return a sanitized projection (child_id, agent_id, mode, state, model) — never internal `Descriptor` dump, never secret material.

### T4: Registration + wiring + CanDelegate gating

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T4 | Register the three tools on the registry and gate them by CanDelegate policy like delegate_task | Modify: internal/tools/builtin/register_*.go (wherebuiltin tools register); Test: existing gate + `go build ./...` | `go build ./...` + `make e2e-fast-area AREA=delegate-seam` | tools not registered / CanDelegate strip bypassed | tools appear on the registry; a non-delegating agent does not get them | Given a delegating agent; When the tools are registered; Then subagent_* are available, and an agent with CanDelegate=false does not see them |

T4 (expand): wire `NewDelegateController` into the tool factory (the same
init-populated `ToolFactories`-style seam the github/acp tools use — see
acp_register_tools.go:25-86). Gate all three tools with the
`delegate_task`-equivalent `CanDelegate` policy (registry.go:838: when
`!spec.CanDelegate`, `delegate_task` is stripped). Keep each tool's
`Category()` (e.g. "delegation") and a truthful `Description()` that states
it steers a *delegated child*, not a whole agent.

### T5: RPC + TUI/GUI parity + docs

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T5 | Expose the control surface to a user (CLI/TUI/GUI or HTTP) and document it, per root AGENTS.md wiring | Create: docs/workflows/delegation.md (section); Modify: cmd/meept/ or internal/tui/ or internal/comm/http/ | `make graphs-check` + `make docs-generate` (if gendoc drifted) | control surface has no user-facing path | `meept delegate send/interrupt/list <child_id>` (or equivalent) drives the same controller; docs describe the three tools | Given a running daemon; When the user runs the delegate control surface; Then it steers/lists/interrupts the same children the model tools reach |

The root AGENTS.md wiring requirement: a capability ships a UI surface +
agent wiring, or documents the deferral. At minimum:
- `meept delegate list`, `meept delegate send <child> <msg>`, `meept delegate interrupt <child>` calling the controller through the existing RPC/CLI seam, OR an equivalent TUI/HTTP surface.
- `docs/workflows/delegation.md` — one section per control tool (semantics, auth, resume).
Defer a rich GUI panel with an explicit note if time-bounded; the CLI + doc must land here.

## Self-Verification Checklist

- [ ] All tasks implemented, gate tests GREEN (F3 e2e steer/interrupt/list)
- [ ] B3 contract satisfied: SendMessage (steer/resume/ack), Interrupt (fire-and-return + auth), ListChildren (registry projection)
- [ ] All files at exact paths (delegate_control.go + registration + T5 docs); no workflow tool or fork logic here
- [ ] Auth: only parent/ancestor interrupts a child; delivery returns ack not answer
- [ ] CanDelegate gating preserved; `go build ./...` clean; no ignored errors, no mutex-across-I/O
- [ ] No debug artifacts / line-number corruption

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** every created/modified path.
2. **Verify command + output:** `go build ./...`; `make e2e-fast-area AREA=delegate-seam`; last ~20 lines.
3. **Deviations:** none / list (esp. the resume seam call if 06's `Resume` is not yet present, and the user-facing surface choice).

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests untouched and GREEN
- [ ] subagent_send_message follows dsh semantics (delivery ack, running child steered, idle continued); subagent_interrupt is fire-and-return `accepted:true` + auth; subagent_list_children derives from the registry
- [ ] B3 signatures match; CanDelegate gating applied
- [ ] acp children routed through *acp.Manager (no second client); no ambient secrets
- [ ] Conventions; no debug artifacts / line-number corruption

Output: APPROVED or specific gaps (file + line).

## Notes

- The control tools are decoration over primitives that already exist
  (SteerActiveAgent, acp Send/Cancel, registry). Do not reimplement steering;
  reuse the exact primitives. The auth rule (parent/ancestor only) is the one
  security-sensitive behavior beyond the primitive — mirror dsh
  manager.ts:183-228.
- `FollowUpActiveAgent` (dispatcher.go:4772) is the *reverse* gesture (send
  when settled); do not confuse it with interrupt. Steering is
  single-slot-replace (queue.go:242); consecutive steers to one child replace
  the latest — that is an intentional primitive, not a bug.
