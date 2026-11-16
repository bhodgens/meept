# Inbox Branch - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 3 (1 test gate + 2 implementation leaves)
- **Scope:** Type the employee inbox body (AgentInboxBody, C4), let send_agent_message carry a structured payload, and make the inter-agent taint scan key on structured fields when present (F6/F7/F8).

## Goal

The inbox is meept's real async inter-agent channel (employee/messaging.go + loop_messages.go + agent_message.go), but its body is free text. Free text forces the taint scan (collaboration_handoff.go) to regex over prose. This branch adds an optional structured envelope so the scan can inspect declared fields (tool targets, file paths, plan entries), while legacy free-text bodies keep today's scan byte-identically.

## Architecture

- 03-01 owns: `internal/employee/messaging.go` (AgentInboxBody + InboxStructured types; AgentMessage gains a typed accessor that decodes body JSON when present), `internal/tools/builtin/agent_message.go` (send_agent_message gains optional `structured` arg; inbox tool unchanged), `internal/agent/loop_messages.go` (IncomingAgentMessage unchanged; the injection block already carries body text — no change needed unless structured rendering is wanted, which it is not).
- 03-02 owns: `internal/agent/collaboration_handoff.go` (prepareHandoff gains a structured-scan branch: when the content parses as AgentInboxBody, scan Structured.ToolTargets/FilePaths/PlanEntries via the existing boundary patterns; free text keeps the current whole-blob scan).
- 03-02 depends on 03-01's AgentInboxBody type (import from internal/employee — but agent already imports employee? NO: employee imports agent? Check: agent does NOT import employee; employee imports bot; builtin imports employee via adapter. The AgentInboxBody type must live where both can import it: `internal/employee` is imported by `internal/tools/builtin` (adapter) and by `internal/daemon`. `internal/agent` must NOT import `internal/employee` (cycle risk: employee→bot→...→tools/builtin→agent). So: AgentInboxBody lives in `internal/employee/messaging.go`; 03-02 (internal/agent) CANNOT import it. Resolution: 03-02 defines its OWN minimal decode struct (same JSON tags) locally in collaboration_handoff.go, or the scan keys on the raw JSON fields via `json.RawMessage`. The contract (C4) pins the JSON tags so both sides agree without an import.)

## Interface Contracts

### C4: AgentInboxBody (JSON contract, pinned in both packages)

```go
// Owner of the Go type: 03-01 (internal/employee/messaging.go)
// 03-02 (internal/agent) defines a local mirror with the SAME JSON tags —
// it must NOT import internal/employee (cycle).

type AgentInboxBody struct {
    Body       string           `json:"body"`
    Structured *InboxStructured `json:"structured,omitempty"`
}
type InboxStructured struct {
    ToolTargets []string `json:"tool_targets,omitempty"`
    FilePaths   []string `json:"file_paths,omitempty"`
    PlanEntries []string `json:"plan_entries,omitempty"`
}
// Wire: send_agent_message{to, message, structured?{tool_targets, file_paths, plan_entries}}
// Storage: agent_messages.body holds the JSON of AgentInboxBody when structured is present;
// legacy messages hold the raw text (backward compatible — decode falls back to Body=raw).
```

### C7: structured scan seam (03-02)

```go
// Owner: 03-02 (collaboration_handoff.go)
// In prepareHandoff, before the existing whole-text scan:
//   var env struct {
//       Body       string `json:"body"`
//       Structured *struct {
//           ToolTargets []string `json:"tool_targets"`
//           FilePaths   []string `json:"file_paths"`
//           PlanEntries []string `json:"plan_entries"`
//       } `json:"structured"`
//   }
//   if json.Unmarshal([]byte(rawOutput), &env) == nil && env.Structured != nil {
//       // scan env.Structured.ToolTargets + FilePaths + PlanEntries via boundary.InspectHandoffContent
//       // (each field separately; a hit blocks with the field name in the reason)
//       // ALSO scan env.Body as before (the human text is still untrusted)
//   } else {
//       // existing whole-blob scan (unchanged)
//   }
// Consumers: 03-01 (produces), 03-02 (consumes)
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | [00T-unit-tests.md](00T-unit-tests.md) | leaf (test gate) | 01-01 | 30K | A |
| 01 | [01-typed-body.md](01-typed-body.md) | leaf | 00T, 01-01 | 50K | A |
| 02 | [02-structured-scan.md](02-structured-scan.md) | leaf | 01 (REVIEWED) | 45K | B |

02-02 is group B (needs 03-01's JSON contract committed; the local mirror struct is in the brief so it does not import employee).

## Dispatch Protocol

### Phase 0: Test Gate

Dispatch 00T-unit-tests.md. Validate; commit; TESTS_VALIDATED.

### Phase 1: Dispatch 03-01

Full brief + C4 contract + conventions + no-commit.

### Phase 2: Review 03-01 (in-session). Commit.

### Phase 3: Dispatch 03-02 (group B, after 03-01 committed).

### Phase 4: Review + integration; COMPLETE; report.

## Review Checklist

- [ ] AgentInboxBody round-trips; legacy free text still delivers
- [ ] send_agent_message accepts + stores structured; inbox returns it
- [ ] prepareHandoff scans structured fields when present; legacy scan unchanged
- [ ] No import cycle (agent does not import employee)
- [ ] Gate tests pass; gate-test diff empty

## Coding Conventions

- Go; snake_case JSON; stdlib testing; `go fmt`; orchestrator commits only

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-unit-tests | PENDING | 0 | |
| 01-typed-body | PENDING | 0 | |
| 02-structured-scan | PENDING | 0 | |

## Integration Test Plan

1. `go test ./internal/employee/ -run 'AgentInboxBody|Inbox' -v`
2. `go test ./internal/tools/builtin/ -run 'SendAgentMessage|Inbox' -v`
3. `go test ./internal/agent/ -run 'Handoff.*Structured' -v`
4. `go build ./...` — no import cycle

## Test Gate

Test leaves: 00T-unit-tests.md - dispatched Phase 0.

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F6 | AgentInboxBody round-trip + legacy compat | 03-01 T1-T2 | TestAgentInboxBody_Roundtrip | (root 00T) | PENDING |
| F7 | send_agent_message structured arg | 03-01 T3 | TestSendAgentMessage_Structured | (root 00T) | PENDING |
| F8 | Structured taint scan; legacy text scan unchanged | 03-02 | TestHandoff_StructuredScan | (root 00T) | PENDING |

Statuses: PENDING | DELTA | VALIDATED
Delta log: [date, feature ID, direction, one-line reason]

## Frozen Documents

Leaves freeze at dispatch; corrections ride dispatch context as dated addenda.

## Open Questions

- Q-03: Should the inbox tool render structured bodies differently (e.g. a summary line)? Rec: NO — the inbox returns the raw body; the reader (agent) decides. Impact: none.
- Q-04: 32KB cap applies to the marshaled AgentInboxBody (slightly larger than the text). Rec: keep the cap on the marshaled body. Impact: negligible.

## Notes

- The no-import constraint (agent cannot import employee) is load-bearing: 03-02 mirrors the JSON shape locally. C4 pins the tags so both sides agree.