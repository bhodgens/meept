# Roster + drainer implementation - Implementation Leaf

> **For the implementing agent:** Implement all tasks. Gate tests exist and are
> committed; make them pass. Do NOT edit gate tests. Do NOT commit. Do NOT use
> read_file; explore with search_files/terminal cat. Write once, then verify.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Wire bots into messaging roster and make turn-start drain live
- **Dependencies:** 00T-inbox-tests.md
- **Estimated Context:** 55K
- **Concurrency Group:** A

## Goal

Close both inbox gaps: bots as message targets, and a production drainer on
both the shared daemon loop and per-turn employee/bot clones.

## Context

- `internal/daemon/employee_message_tools.go:18-55,83-136` - adapter + wiring
- `internal/agent/loop_messages.go:31-85` - MessageDrainer interface + injection
- `internal/daemon/voice_employee.go:458-478` - employeeTurnClone
- `internal/daemon/components.go:5429,5497` - WireEmployeeMessagingForComponents calls
- `internal/daemon/components.go:2656` - shared AgentLoop construction

## Verified Anchors

- `internal/daemon/employee_message_tools.go:111` - `targetExists := func(id string) bool`
- `internal/daemon/employee_message_tools.go:123` - `listTargets := func() []string`
- `internal/agent/loop_messages.go:42` - `func (l *AgentLoop) SetMessageDrainer`
- `internal/daemon/voice_employee.go:462` - `clone := agent.NewAgentLoop(...)`

## Interface Contracts (From Parent)

### What This Leaf Exposes

- C4 getter `func (l *AgentLoop) MessageDrainer() MessageDrainer`
- adapter `DrainInbox(to string, limit int) ([]agent.IncomingAgentMessage, error)`
- roster additions for bots

### What This Leaf Consumes

- `employee.MessageStore.DrainInbox(to, limit)` - existing
- `bot.Manager.GetBot` / `ListBots` - existing

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Add drainer getter | Modify: `internal/agent/loop_messages.go` | `go test -p 2 ./internal/agent/ -count=1` | getter absent | compiles | Given a loop; When MessageDrainer(); Then returns the set drainer |
| T2 | Extend adapter + roster | Modify: `internal/daemon/employee_message_tools.go` | `go test -p 2 ./internal/daemon/ -run TestRoster_IncludesBotID -count=1` | bot not in roster | gate passes | Given BotManager; When targetExists/listTargets; Then bot IDs included |
| T3 | Wire shared loop + clone | Modify: `internal/daemon/voice_employee.go`, `internal/daemon/components.go` | `go test -p 2 ./internal/daemon/ -run TestClone_InheritsDrainer -count=1` | clone has no drainer | gate passes | Given adapter; When shared loop built and clone made; Then both drain |

T1: add a `MessageDrainer() MessageDrainer` getter that snapshots `l.messageDrainer`
under `l.mu` (matching `SetMessageDrainer`'s lock).

T2: in `WireEmployeeMessagingForComponents`, extend `targetExists` with
`c.BotManager.GetBot(ctx, id)` (nil-safe manager) and `listTargets` with
`c.BotManager.ListBots(ctx)`, deduping ids. Add an adapter method that returns
`[]agent.IncomingAgentMessage` by mapping the stored messages, so the adapter
satisfies both `builtin.AgentMessager` and `agent.MessageDrainer`.

T3: at the shared loop construction (`components.go:2656`), after the adapter
exists, call `c.AgentLoop.SetMessageDrainer(adapter)` (nil-safe). In
`employeeTurnClone`, after `SetAgentID`, copy the drainer from the template via
`template.MessageDrainer()` with a nil guard. This makes both employee and bot
goal rounds drain queued messages at turn start.

## Self-Verification Checklist

- [ ] Bot IDs in roster; shared loop and clone both wired
- [ ] Nil manager/store/drainer safe; no typed-nil regression
- [ ] `inbox` tool still works (drain marks read)
- [ ] Gate tests unmodified; `gofmt` clean

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Getter locks correctly; no I/O under mutex
- [ ] Dedupe in listTargets; no duplicate ids
- [ ] Drainer propagation covers bot clone path

Output: APPROVED or specific gaps.

## Notes

`agent.MessageDrainer` returns `[]agent.IncomingAgentMessage`; the adapter's
existing `builtin.AgentMessager` returns `[]*builtin.AgentMessage`. Both methods
can coexist on one struct with different names or a small wrapper type.
