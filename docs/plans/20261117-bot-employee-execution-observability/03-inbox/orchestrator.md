# Inbox Addressability - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this branch.
> Dispatch implementation agents, review, track completion. Do NOT implement
> code yourself. Full detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 2 leaves (test gate, roster + drainer)
- **Scope:** Make bots valid `send_agent_message` targets and drain messages at turn start

## Goal

Extend the messaging roster to bots and make turn-start injection live for both
tiers. Today `targetExists`/`listTargets` in `employee_message_tools.go:111-133`
cover only the agent registry and employees; bots are excluded. Separately,
`AgentLoop.SetMessageDrainer` at `internal/agent/loop_messages.go:42` has no
production caller, so the injection at `internal/agent/loop.go:9376` never fires.
Fix both.

## Architecture

The employee `MessageStore` (table `agent_messages`) already persists queued
messages for any recipient id. The daemon adapter wires it into three surfaces:
`builtin.AgentMessager` (send_agent_message/inbox tools), `agent.MessageDrainer`
(turn-start injection), and a roster predicate. `employeeTurnClone` copies the
template drainer so per-turn employee clones drain under the stamped
`agentID`. A bot addressed by id therefore sees the message on its next goal
round. No new table, no new tool, no new topic.

## Interface Contracts

### C4: Roster + drainer

```
targetExists(id) == true when AgentRegistry.GetSpec(id) exists
                        OR EmployeeManager.GetEmployee(id) succeeds
                        OR BotManager.GetBot(id) succeeds
listTargets() = dedup of agent registry ids + employee ids + bot ids
func (l *AgentLoop) MessageDrainer() MessageDrainer
func (a *employeeMessageStoreAdapter) DrainInbox(to string, limit int) ([]agent.IncomingAgentMessage, error)
```
`employeeTurnClone` copies `template.MessageDrainer()` onto the clone; the
clone's `SetAgentID(employeeID)` already stamps the drain key.

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-inbox-tests.md | leaf (test) | none | 30K | A |
| 01 | 01-roster-and-drainer.md | leaf | 00T | 55K | A |

## Dispatch Protocol

### Phase 0: Test Gate

Dispatch `00T-inbox-tests.md`. Validate RED against Test Gate F5/F6. Commit;
set TESTS_VALIDATED.

### Phase 1: Implementation

Dispatch 01 with C4 in full. This leaf edits `internal/agent/loop_messages.go`,
`internal/daemon/employee_message_tools.go`, and `internal/daemon/voice_employee.go`.
No other branch edits these files.

### Phase 2-3: Review and Integration

Review in-session. Verify both roster tests and clone-drain test green. Set COMPLETE.

## Review Checklist

- [ ] Bot IDs accepted by send_agent_message and listed by platform_agents
- [ ] Turn-start injection fires on the per-turn clone
- [ ] Legacy raw-text bodies unchanged; structured envelope unchanged
- [ ] Nil store / nil manager safe; no typed-nil regression

## Coding Conventions

- Go 1.27; gofmt; nil guards on every Set*
- Guard typed-nil interface assignments; snapshot under lock, act outside lock
- Leaf agents do NOT commit and do NOT run git add

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R4 | send_agent_message accepts bot ID; next run sees it | 01-roster-and-drainer.md | F5/F6 gate + e2e-04 |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-inbox-tests | PENDING | 0 | |
| 01-roster-and-drainer | PENDING | 0 | |

## Integration Test Plan

`go test -p 2 ./internal/agent/... ./internal/daemon/... ./internal/employee/... -count=1`
plus `go test -tags e2e ./e2e/suites/execution-observability/... -run BotInbox`.

## Test Gate

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F5 | Bots in roster | 01-roster-and-drainer.md | TestRoster_IncludesBotID | execution-observability-04 | PENDING |
| F6 | Clone inherits drainer | 01-roster-and-drainer.md | TestClone_InheritsDrainer | execution-observability-04 | PENDING |

Delta log: none yet.

## Open Questions

- Resolved: operator confirmed both overarching decisions (keep both tiers;
  separate result topics). Neither changes this branch. Delivery timing stays
  as specified: a bot drains at its next goal round, matching the issue's
  "next run sees the message" acceptance. Immediate push wake-up via
  `SteerEmployee` (`internal/employee/manager.go:1796-1820`) is OUT OF SCOPE.

## Notes

The `#139` typed inbox body (`employee.AgentInboxBody`) is unchanged.
