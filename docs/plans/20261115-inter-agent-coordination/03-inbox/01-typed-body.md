# Typed Inbox Body - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. The gate tests already exist and are committed - make them pass; do NOT delete, skip, weaken, or rewrite any gate test. Do NOT commit. Do NOT git add. Do NOT use read_file on existing source files - explore with search_files or terminal cat. After writing a file, do NOT read it back - write once and stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Add AgentInboxBody + InboxStructured types to the employee message layer (C4); let send_agent_message carry an optional structured payload; keep legacy free-text bodies working.
- **Dependencies:** 00T-unit-tests (gate tests committed), 01-01 (contract — not strictly needed; C4 is self-contained)
- **Estimated Context:** 50K
- **Concurrency Group:** A
- **File ownership:** `internal/employee/messaging.go`, `internal/tools/builtin/agent_message.go`. Do NOT touch `internal/agent/` (03-02 owns the scan side there).

## Goal

Messages between agents can carry a structured envelope (tool targets, file paths, plan entries) alongside the human text, so downstream consumers (the taint scan, future UIs) can key on fields. Legacy messages (plain text) keep working unchanged.

## Context

- `internal/employee/messaging.go:35-43` — AgentMessage (Body string). Add the new types here; AgentMessage stays as-is (the body field holds the JSON when structured).
- `internal/employee/messaging.go:249` — Enqueue (stores Body verbatim; no change).
- `internal/tools/builtin/agent_message.go:57-84` — SendAgentMessageTool: Parameters() (add optional `structured` object); Execute() (build AgentInboxBody, marshal to body).
- `internal/tools/builtin/agent_message.go:130-165` — Execute (exact insertion point).
- The store interface (`agentMessageStore`, agent_message.go:44-49) is unchanged.

## Verified Anchors

- `internal/employee/messaging.go:35-43` — AgentMessage struct (add types BELOW it, do not modify it).
- `internal/tools/builtin/agent_message.go:86-128` — SendAgentMessageTool.Name/Description/Parameters (extend Parameters).
- `internal/tools/builtin/agent_message.go:130-165` — Execute (build + enqueue).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/employee/messaging.go
type AgentInboxBody struct {
    Body       string           `json:"body"`
    Structured *InboxStructured `json:"structured,omitempty"`
}
type InboxStructured struct {
    ToolTargets []string `json:"tool_targets,omitempty"`
    FilePaths   []string `json:"file_paths,omitempty"`
    PlanEntries []string `json:"plan_entries,omitempty"`
}

// internal/tools/builtin/agent_message.go
// send_agent_message Parameters() gains:
//   "structured": { type: object, properties: {
//       tool_targets: {type: array, items: {type: string}},
//       file_paths:   {type: array, items: {type: string}},
//       plan_entries: {type: array, items: {type: string}} } }
// Execute(): when structured present, body = json.Marshal(AgentInboxBody{Body: message, Structured: parsed})
//            when absent, body = message (legacy, byte-identical)
```

### What This Leaf Consumes

- C4 (this leaf defines the Go type; the JSON tags are the contract).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | AgentInboxBody + InboxStructured types | Modify: `internal/employee/messaging.go` | `go test ./internal/employee/ -run 'TestAgentInboxBody_Roundtrip' -v` | FAIL: undefined AgentInboxBody | PASS: marshal/unmarshal table green | Given a structured body; When round-trip; Then fields equal. Given raw text; When unmarshal; Then Body==raw, Structured==nil |
| T2 | send_agent_message structured param | Modify: `internal/tools/builtin/agent_message.go` | `go test ./internal/tools/builtin/ -run 'TestSendAgentMessage_Structured' -v` | FAIL: unknown param | PASS: structured stored as AgentInboxBody JSON | Given store+roster; When send with structured; Then stored body parses with Structured set |
| T3 | Legacy path byte-identical | Modify: `internal/tools/builtin/agent_message.go` | `go test ./internal/tools/builtin/ -run 'TestSendAgentMessage' -v` | (existing tests must stay green) | PASS: legacy send stores raw text | Given send without structured; When executed; Then stored body == message (no JSON envelope) |

**T1 expansion:** define both structs with the exact JSON tags from C4. Add a `ParseAgentInboxBody(raw string) AgentInboxBody` helper (unmarshal; on failure return {Body: raw}).

**T2 expansion:** in Parameters(), add the optional object property. In Execute(), after building `msg`, parse args["structured"] (map[string]any → InboxStructured via manual extraction or json round-trip); if non-nil, `body, _ := json.Marshal(AgentInboxBody{Body: body, Structured: &st})` and use that as the message body. The 32KB cap (MaxAgentMessageBody) applies to the marshaled body.

## Self-Verification Checklist

- [ ] `go build ./internal/employee/... ./internal/tools/builtin/...` clean
- [ ] `go test ./internal/employee/ -run 'AgentInboxBody' -v` PASS
- [ ] `go test ./internal/tools/builtin/ -run 'SendAgentMessage' -v` PASS (new + existing)
- [ ] Legacy send stores raw text (no envelope)
- [ ] Gate tests untouched; `go fmt`; only the 2 owned files modified

**DO NOT COMMIT.**

## Completion Report Format

1. **Files touched:** [paths]
2. **Verify command + output:** [last 20 lines]
3. **Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Types + JSON tags exact per C4
- [ ] structured optional; legacy byte-identical
- [ ] 32KB cap still enforced
- [ ] Gate tests untouched; no line-number corruption

Output: APPROVED or specific gaps.