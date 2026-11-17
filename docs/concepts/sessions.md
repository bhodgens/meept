# Sessions

## Overview

A session is Meept's durable, client-visible unit of conversational state.
Clients (TUI, Flutter desktop/web, CLI, Telegram, voice) attach to a session.
The session survives client disconnects, daemon restarts, and model context
exhaustion. Work state lives behind the session, not inside the chat context
window.

## Problem

Single-session CLI agents conflate two different things:

- The ongoing conversation the user sees.
- The execution state of the work being done (plans, steps, delegated
  children, parked turns, budgets, evidence).

Mixing both into one context window forces a bad choice: either carry the
full work state into every prompt (token bloat, drift, lost structure), or
drop it and lose track of what the work actually did.

## Solution

Meept separates the two.

```
Session (session-abc)                  backend state (keyed by session id)
  ├─→ message tree (branchable)         plan store: phases / steps / tasks
  ├─→ threads (topic partitions)        goal store: mandates, gates
  ├─→ LeafMessageID (live path)         delegate registry: child agents
  ├─→ ProjectID / WorktreePath          park/queue: suspended turns
  ├─→ GoalID / EmployeeID               budget + evidence: per-turn accounting
  └─→ ConversationID                    LLM session affinity + teardown snapshot
```

The orchestration shape is a tree, but the tree nodes are plan steps and
delegated children, not chat messages. The session is the handle the tree
hangs off.

### The message tree

Messages in a session are rows with `parent_id`, `entry_type`, and
`branch_id` (`internal/session/store.go`). One session can hold several
alternate histories. `session.LeafMessageID` names the live path.
`GetMessagePath` walks from the leaf to the root. `AssembleBranch`
(`internal/session/tree.go`) flattens that path into LLM messages, injecting
`[Compacted Context]` entries for compacted spans and `[Branch Summary]`
entries for abandoned sibling branches.

Threads (`internal/session/thread.go`) are a coarser partition inside a
session: topic-labeled sub-conversations, each with its own conversation ID
and summary. Exactly one thread is active per session.

### The work tree

Work hangs off the session by ID, not by containment:

- Plans: the planner/orchestrator/executor walk a tree of phases, steps, and
  tasks. Step state lives in the plan subsystem.
- Delegated children: `DelegateManager.Spawn` (`internal/agent/delegate.go`)
  creates a child record with `ParentSessionID` and a fresh child
  `ConversationID`. Depth is tracked and capped. Children run async and
  settle via bus events.
- Goals and employees: a session can be bound to one goal and one employee
  directly (`Session.GoalID`, `Session.EmployeeID`). The goal tree itself
  lives in the goal/employee store.
- Parked turns: quota-blocked or gate-blocked turns park on the backend and
  resume later. They are not chat messages.

### What the model sees

The chat context window never carries the raw work tree. It carries a
rendered digest (`internal/agent/session_digest.go`) plus the assembled
message path. The dispatcher (`internal/agent/dispatcher.go`) classifies each
turn and routes it: chat, plan, delegate, tool, or park.

## Why this is better than the alternatives

| Alternative | What breaks | Meept's answer |
|---|---|---|
| Flat transcript in one context window | Context exhaustion loses the whole work state; old decisions fall out of the window | Compaction entries + branch summaries keep the live path bounded; work state is stored outside the window |
| Whole plan files injected into every prompt | Token cost grows with plan size; stale plan text pollutes reasoning | Dynamic loading; only a digest is injected |
| Subagent output as chat paste | Child context bleeds into parent; no depth control | Children get their own conversation ID; depth cap; async settlement |
| "Which plan is this window working on?" lost on resume | User must track plan-to-session mapping by hand | Session carries `GoalID`, `EmployeeID`, `ProjectID`, `WorktreePath` |
| Client disconnect kills the run | Work in progress is lost | Daemon owns the session; clients reattach |
| Branching via git-like conversation fork only | Heavyweight for simple topic switches | Threads for topic isolation; branches kept but disabled by default |

## Session identity

`session_id` and `conversation_id` are distinct. Downstream consumers
(sticky endpoint map, teardown snapshot, budget scopes) key on the
accounting session id. Never mint a second identity for lifecycle events.

Project binding is PER-SESSION. Working directory resolution precedence:
`WorktreePath` > `ProjectPath` > `DetectionContext.CWD` > configured daemon
default (`internal/session/working_dir.go`). There is no global
"active project" fallback.

## Lifecycle

The store fires `SessionLifecycleHook` on archive, delete, and idle events
(see `internal/session/AGENTS.md`). The daemon wires one hook: the teardown
coordinator. Teardown is best-effort and never fails the archive or delete
caller. The idle reaper releases LLM session-affinity resources after a
quiet period.

## Source

- `internal/session/session.go` - Session struct
- `internal/session/store.go` - message/branch/tree store interface
- `internal/session/tree.go` - AssembleBranch
- `internal/session/thread.go` - threads
- `internal/session/branch.go` - branch navigation (disabled by default)
- `internal/session/working_dir.go` - per-session workdir precedence
- `internal/session/AGENTS.md` - lifecycle hooks and idle reaper
- `internal/agent/delegate.go` - delegated children
- `internal/agent/dispatcher.go` - intent routing
