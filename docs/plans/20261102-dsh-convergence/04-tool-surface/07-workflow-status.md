# 07 Workflow Status - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below. Do NOT commit - the orchestrator handles all
> git operations after review. Scoped tests only (owned packages + the
> delegated verify gate).

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Expose background workflow job status to the MODEL via a
  `workflow_status` builtin tool, and expose delegated child records to the
  OPERATOR via the `delegate.status` RPC + `meept agents children` CLI
  (audit item 5 + DSH-9d).
- **Dependencies:** DSH-8 (completion delivery: `RunBackground` publishes
  `agent.delegate.result` kind:workflow and exposes `WaitCompletion`/`Completion`)
  and DSH-9d's intake_status_rpc.go precedent.
- **Concurrency Group:** A
- **Audit references:** /tmp/dsh-audit/runtime-safety.md (audit item 5:
  background wf-job ids were fire-and-forget from the model's perspective)

## Goal

A model that ran `workflow` with `run_in_background:true` receives
`{kind:"background", jobId, runId}` and then has NO way to ask "did it
finish?". This leaf closes that loop: a `workflow_status` tool takes a
wf-job id and returns `running` until the run settles, then the settled
`WorkflowRun` summary (`run_id`, `stop_reason`, `agents_started`, `result`,
`error`). Status is idempotent: repeated queries after settlement keep
reporting the settled outcome.

On the operator side, delegated children are invisible outside the model
surface. This leaf adds a read-only `delegate.status` RPC (listing the
children of a session or an agent id) and the `meept agents children
<agent-or-session>` CLI on top of it — report-only, no new write surface.

## Context

- `internal/tools/builtin/delegate_workflow.go` - `WorkflowEngine`
  (DSH-8): `RunBackground(script, args) string` returns a `wf-job-...` id,
  delivers the settled `WorkflowRun` on a single-consumption channel seam
  (`Completion()`/`WaitCompletion`), and publishes `agent.delegate.result`
  kind:workflow when a completion bus is wired. `WorkflowRun` carries
  `RunID/AgentsStarted/StopReason/StopMessage/Value`; stop reasons are
  `completed | cancelled | error`.
- `internal/tools/builtin/delegate_control.go` - `DelegateController`, the
  plain-data controller bound behind the delegate tool group (nil-safe
  primitives, fail-closed error envelopes).
- `internal/tools/builtin/delegate_register.go` - the `delegate` toolprov
  provider; `Discover()` constructs subagent_send_message / interrupt /
  list_children / workflow from the same bound controller. The new tool
  joins this list.
- `internal/rpc/voice_local.go` - the seam-based RPC handler precedent:
  func-field seams (no internal/rpc -> internal packages cycles), always
  registered so clients get a named refusal instead of "method not found".
- `internal/daemon/voice_local_rpc.go` / `intake_status_rpc.go` - the
  daemon wiring precedent: nil-safe `register*RPC(c *Components, server
  *rpc.Server, logger)` helpers called from the daemon's RPC registration
  section (`internal/daemon/daemon.go` ~:1546).
- `internal/agent/delegate.go` - `DelegateManager.Children(parentSessionID)`
  (snapshot by parent). To resolve an AGENT id as well as a session id the
  registry needs one trivially additive read-only accessor,
  `AllChildren()`.
- `cmd/meept/voice.go` - the CLI status precedent:
  `connectDaemon()` + `client.Call(method, req)` + a unit-tested
  tabwriter formatter.

## Verified Anchors

- internal/tools/builtin/delegate_workflow.go:391-414 - RunBackground (job id + delivery)
- internal/tools/builtin/delegate_workflow.go:436-467 - Completion / WaitCompletion (single-consumption channel)
- internal/tools/builtin/delegate_control.go:33-72 - DelegateController (+ SetCompletionBus)
- internal/tools/builtin/delegate_register.go:77-86 - delegateProvider.Discover
- internal/agent/delegate.go:452 - DelegateManager.Children
- internal/rpc/voice_local.go:126-146 - seam-based handler + registration
- internal/daemon/daemon.go:1540-1546 - RPC registration section

## Interface Contracts

### What This Leaf Exposes

```go
// internal/tools/builtin/delegate_workflow.go (+ controller state in
// delegate_control.go):
//  DelegateController:
//    registerWorkflowJob(jobID string, e *WorkflowEngine)   // bounded registry
//    workflowJobStatus(jobID string) (jobStatusView, bool)  // idempotent status
//  jobStatusView{JobID, RunID string; Status string; StopReason, Error,
//                Result string; AgentsStarted int64}
//  WorkflowStatusTool: Name "workflow_status", Category "delegation",
//    params {job_id:string, required}; read-only.
// internal/agent/delegate.go:
//    (m *DelegateManager) AllChildren() []ChildRecord  // snapshot, read-only
// internal/rpc/delegate_children.go:
//    DelegateChildRow{child_id, agent_id, parent_session_id, mode, state,
//                     model, created_at_rfc3339, has_result, error}
//    DelegateStatusResult{query, children, note}
//    delegate.status req {query} -> DelegateStatusResult
// internal/daemon/delegate_rpc.go:
//    registerDelegateStatusRPC(c *Components, server *rpc.Server, logger)
// cmd/meept/agents_children.go:
//    meept agents children <agent-or-session>
```

### What This Leaf Consumes

- DSH-8's completion seam (`WorkflowEngine.Completion`) — status reads it
  through a caching entry so the single-consumption channel is not
  observable at the tool surface.
- The intake_status_rpc.go / voice_local_rpc.go nil-safe registration
  pattern — the daemon registration block gains one call.

## Tasks

| Task ID | Objective | Files | Verify command | PASS state |
|---------|-----------|-------|----------------|------------|
| T1 | Bounded per-controller wf-job registry with idempotent cached status | Modify internal/tools/builtin/delegate_control.go (add registry + accessor), delegate_workflow.go (tool) | `go test -p 2 -count=1 ./internal/tools/builtin/ -run TestWorkflowStatus -v` | Given a background run; When queried before settlement; Then status is `running`; after settlement `completed` with the run summary; repeated queries stay `completed`; an unknown id is a clean error envelope; registering >cap jobs keeps the map bounded (oldest evicted) |
| T2 | Register `workflow_status` in the delegate provider | Modify internal/tools/builtin/delegate_register.go | `go test -p 2 -count=1 ./internal/tools/builtin/ -run TestWorkflow -v` | Discover() includes the new tool; existing registration test still passes |
| T3 | `AllChildren()` read-only accessor | Modify internal/agent/delegate.go (ONE added method) | `go build ./...` | Snapshot across all parents; no behavior change elsewhere |
| T4 | `delegate.status` RPC handler + daemon wiring | Create internal/rpc/delegate_children.go (+ test), internal/daemon/delegate_rpc.go; one registration call in internal/daemon/daemon.go | `go test -p 2 -count=1 ./internal/rpc/ ./internal/daemon/ -run TestDelegate -v` | Handler always registers; empty query -> named error; unwired seam -> empty list + note; daemon maps ChildRecords (session OR agent match) onto rows |
| T5 | `meept agents children` CLI | Create cmd/meept/agents_children.go (+ test); one AddCommand in cmd/meept/agents_cmd.go | `go build ./cmd/meept` and `go test -p 2 -count=1 ./cmd/meept/ -run TestAgentsChildren -v` | Exact-args command; renders the child table; empty list prints a clear line; formatter unit-tested without a daemon |

## Self-Verification Checklist

- [ ] `workflow_status` is read-only (IsReadOnly posture via ToolDefaults) and never blocks
- [ ] Job registry is bounded; unknown id -> clean error, never a panic or hang
- [ ] Status is idempotent after settlement (cached, not single-consumption)
- [ ] delegate.status is report-only: no steer/interrupt/cancel surface
- [ ] Child rows carry no seed/descriptor/secret material
- [ ] All verify commands GREEN

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Notes

- The channel seam (`Completion`) is single-consumption by design (see
  TestWorkflowBackgroundDeliversCompletion). The status surface must cache
  the first observed settlement per job so repeated `workflow_status` calls
  do not flip a completed job back to `running`.
- The e2e scenario for background-then-status was evaluated and skipped:
  the fake-model harness drives static post-tool text, so asserting
  running-vs-completed across an async settlement is timing-flaky. Unit
  coverage here is deterministic and covers the same contract.
