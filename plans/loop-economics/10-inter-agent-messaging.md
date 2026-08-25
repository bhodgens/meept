# Inter-Agent Messaging with Receipts - Implementation Leaf

> Implement ALL tasks via TDD. Do NOT commit. Do NOT read files back.

## Meta
- **Parent:** ../master.md
- **Scope:** Employee/specialist-to-employee messaging: send tool, receipts, inbox injection, roster reachability.
- **Deps:** none | **Context:** 60K | **Group:** C

## Goal

Agents coordinate only through the orchestrator today. prime-agent semantics: direct agent-to-agent messages with delivery states (queued/delivered), persisted so a message to a busy/offline employee waits; recipient sees unread messages injected at next turn start. Extends the existing bus + employee manager — no new transport.

## Context

internal/employee/manager.go (roster), internal/bus (pub/sub), platform tools pattern in internal/tools/builtin (platform_agents exists). SQLite conventions from session store.

Key files: internal/employee/*.go, internal/tools/builtin/platform*.go, config schema if needed.

## Interface Contracts (From Parent)

```go
// internal/employee/messaging.go:
type AgentMessage struct {
    ID string; From, To, Body string
    State string // "queued"|"delivered"|"read"
    CreatedAt time.Time; DeliveredAt *time.Time
}
func NewMessageStore(dbPath string) (*MessageStore, error) // table agent_messages
func (m *MessageStore) Enqueue(msg *AgentMessage) error          // state=queued
func (m *MessageStore) MarkDelivered(id string) error
func (m *MessageStore) DrainInbox(to string, limit int) ([]AgentMessage, error) // queued->delivered, returns
func (m *MessageStore) MarkRead(ids []string) error
```

Tools:
- send_agent_message{to, message} -> validates target exists in roster -> Enqueue -> immediate delivery attempt: if recipient loop active (manager knows), publish bus topic agent.message{msg_id} which injects as steering-class input? DECISION: deliver-at-next-turn-start via DrainInbox hook in loop prep (simpler, consistent); receipt returned to sender = {id, state:"queued"} then bus event agent.message.delivered when drained.
- inbox{} -> own unread list + mark read.

Roster: platform_agents output gains "reachable": bool + last_seen for employees (manager tracks heartbeat already or adds timestamp on turn start).

Injection point: agent loop pre-turn hook where memory context is assembled (loop.go context injection site) — prepend unread messages as system-anchored block "[message from X]" w/ ids for reply targeting.

## Tasks
1. Failing tests store: enqueue/drain/mark transitions; drain limits ordering FIFO.
2. Failing tool tests: unknown recipient error listing valid targets; send enqueues + returns receipt; inbox drains + marks read.
3. Failing loop-injection test: pending message appears in built context exactly once; second turn no re-inject.
4. Roster field plumbing; docs section appended to docs/workflows/employees.md.

## Self-Verification Checklist
- [ ] -race green internal/employee internal/tools/builtin internal/agent
- [ ] No message loss on daemon restart (persisted pre-drain)
- [ ] Body size cap 32KB enforced at enqueue

## Review Checklist
- [ ] Injection uses anchor/system path not user-role spoofing
- [ ] IDs pkg/id.Generate
- [ ] Conventions per orchestrator

Output: APPROVED or gaps. Notes: cross-daemon messaging OUT of scope (cluster later).
