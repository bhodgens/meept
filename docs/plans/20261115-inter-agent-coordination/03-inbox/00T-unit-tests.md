# Inbox Unit Tests - Test Leaf

> **For the implementing agent:** Write tests ONLY. Do NOT write or modify implementation files. Tests must fail RED for missing-implementation reasons. Do NOT commit. Do NOT git add.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Unit tests for the typed inbox body (03-01) and structured taint scan (03-02). Gates both.
- **Dependencies:** 01-01 (contract committed)
- **Estimated Context:** 30K
- **Concurrency Group:** A

## Goal

Write the RED gate tests for F6, F7, F8.

## Context

- Test files: `internal/employee/messaging_inbox_test.go` (F6), `internal/tools/builtin/agent_message_test.go` (F7 — file exists, add cases), `internal/agent/collaboration_handoff_test.go` (F8 — file exists, add cases).
- stdlib testing; table-driven.
- The AgentInboxBody type and the structured arg do not exist yet; the scan branches on structured fields. Tests reference the target API and fail RED until implemented.

## Verified Anchors

- `internal/employee/messaging.go:35-43` — AgentMessage struct (no JSON tags; the body is a string).
- `internal/employee/messaging.go:249` — Enqueue.
- `internal/tools/builtin/agent_message.go:57-84` — SendAgentMessageTool (params: to, message).
- `internal/agent/collaboration_handoff.go:97-136` — prepareHandoff (whole-text scan).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// Test files only. Reference (C4):
//   employee.AgentInboxBody{Body string, Structured *employee.InboxStructured}
//   employee.InboxStructured{ToolTargets, FilePaths, PlanEntries []string}
//   send_agent_message gains optional "structured" param
//   prepareHandoff scans structured fields when body parses as AgentInboxBody
```

### What This Leaf Consumes

- C4 (03-01), C7 (03-02).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | AgentInboxBody round-trip + legacy compat | Create: `internal/employee/messaging_inbox_test.go` | `go test ./internal/employee/ -run 'TestAgentInboxBody_Roundtrip' -v` | FAIL: undefined AgentInboxBody | PASS: structured body marshals to {body, structured{...}}; a raw legacy string decodes as {Body: raw} | Given a body with structured; When marshal+unmarshal; Then fields match. Given raw "hello"; When unmarshal; Then Body=="hello", Structured==nil |
| T2 | send_agent_message structured arg | Modify: `internal/tools/builtin/agent_message_test.go` | `go test ./internal/tools/builtin/ -run 'TestSendAgentMessage_Structured' -v` | FAIL: unknown param "structured" | PASS: tool accepts structured, stores AgentInboxBody JSON, receipt includes id+state | Given a store + roster with "bob"; When send with structured{tool_targets:["shell"]}; Then stored body parses as AgentInboxBody with Structured.ToolTargets==["shell"] |
| T3 | Structured taint scan; legacy text scan unchanged | Modify: `internal/agent/collaboration_handoff_test.go` | `go test ./internal/agent/ -run 'TestHandoff_StructuredScan' -v` | FAIL: no structured branch | PASS: body with structured containing a deny-pattern file_path blocks in block mode; legacy free text still blocks on the same pattern; clean structured body passes | Given a HandoffBoundary in block mode + a deny pattern for "exfiltration"; When handoff with structured file_path matching; Then blocked with reason naming the field; When legacy text matching; Then blocked as today |

**T2 expansion:** the send_agent_message tool takes `to` + `message` args (agent_message.go:96-104); add the optional object property `structured` (llm.ParameterProperty supports nested Properties, models.go:492-501). Execute() builds AgentInboxBody{Body: message, Structured: parsed} and JSON-marshals it into the store. The inbox tool returns the raw body string as today (the reader decodes).

**T3 expansion:** the boundary's deny classes exist (agent_boundary.go:40-49: forged_role_marker, instruction_override, control_verb_forgery, exfiltration_directive). Fixture the deny content against the exfiltration class's compiled pattern (read the pattern list at agent_boundary.go:26-30 to build the fixture - do not invent a pattern string).

## Self-Verification Checklist

- [ ] All 3 test funcs written; RED until implemented
- [ ] Tests assert round-trip + scan behavior, not implementation
- [ ] No implementation files touched

**DO NOT COMMIT.**

## Completion Report Format

1. **Files touched:** [paths]
2. **Verify command + output:** [last 20 lines]
3. **RED confirmation:** [failures observed]
4. **Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Every F6-F8 row has a test
- [ ] Tests fail RED for missing-implementation reasons only
- [ ] No implementation code in test files

Output: APPROVED or gaps.