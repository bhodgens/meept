# Multi-Agent Collaboration Design

**Date:** 2026-06-05
**Status:** Draft — awaiting user review

## 1. Problem Statement

Meept's current multi-agent architecture supports:
- Single-agent execution (dispatcher → specialist → result)
- Asynchronous actor-reviewer pairs (`PairManager`/`PairOrchestrator`)

Two advanced collaboration patterns are desired:

1. **Pair Programming (symmetric):** Two agents collaborate on the same task in real-time, with fluid role reversal, sharing a workspace and conversation context. Neither is permanently the actor or reviewer.
2. **Differential Development (A/B comparison):** Two independent models implement the same task in parallel worktrees, each validated by a reviewer. A third model (the differentiator) compares both plan-validated implementations and synthesizes a combined result.

Additionally, agents should be able to **initiate collaboration on their own** when encountering ambiguous or complex problems, without requiring the user to request it explicitly.

## 2. Goals

- Add collaboration as a **first-class, pluggable layer** without breaking existing actor-reviewer pairs.
- Enable **symmetric, multi-turn pair programming** with shared state and fluid role reversal.
- Enable **parallel A/B implementation + review + differentiation** with workspace isolation.
- Allow **agent-initiated collaboration** with budget, depth, and approval guardrails.
- Reuse existing components (`PairManager`, `WorkspaceManager`, `AgentRegistry`) where possible.

## 3. Non-Goals

- Replacing the existing `PairManager` or `PairOrchestrator`.
- Real-time concurrent editing (agents take turns, one at a time).
- Automatic detection of "when to use collaboration" (dispatcher heuristic is manual/explicit for now).
- Cross-session memory sharing (each collaboration session is isolated).

## 4. Architecture Overview

```
┌─────────────┐     ┌──────────────┐     ┌──────────────────────────────┐
│  Dispatcher │────▶│ Orchestrator │────▶│  CollaborationEngine         │
│ (intents)   │     │ (routes)     │     │  ├─ PairManager *            │
└─────────────┘     └──────────────┘     │  ├─ PairOrchestrator *       │
                                         │  ├─ PairProgrammingDriver    │
                                         │  ├─ DifferentialDriver       │
                                         │  ├─ TurnManager              │
                                         │  └─ SessionRegistry          │
                                         └──────────────────────────────┘
                                                       │
                        ┌──────────────────────────────┼──────────────────────────────┐
                        ▼                              ▼                              ▼
                   ┌─────────┐                  ┌─────────┐                  ┌─────────────────┐
                   │ Agent A │                  │ Agent B │                  │ Differentiator  │
                   │ (model) │                  │ (model) │                  │   (model C)     │
                   └────┬────┘                  └────┬────┘                  └────────┬────────┘
                        │                            │                                │
                        └────────┬───────────────────┘                                │
                                 ▼                                                    │
                        ┌─────────────────┐                                  ┌────────▼────────┐
                        │ Shared Workspace│                                  │ Combined WS     │
                        │ (git-tracked)   │                                  │ (git-tracked)   │
                        └─────────────────┘                                  └─────────────────┘
```

\* Existing, unchanged components

## 5. CollaborationEngine

### 5.1 Core Type

```go
type CollaborationEngine struct {
    modes      map[string]CollaborationMode   // registered modes
    sessions   map[string]*CollaborationSession // active sessions
    bus        *bus.MessageBus
    registry   *agent.AgentRegistry
    workspaces *agent.WorkspaceManager
    pairMgr    *agent.PairManager          // reused for differential review loops
    logger     *slog.Logger
    mu         sync.RWMutex
}
```

### 5.2 Mode Interface

```go
type CollaborationMode interface {
    Name() string
    Run(ctx context.Context, sess *CollaborationSession) (*CollaborationResult, error)
    CanInitiate(agentID string, reason string) bool // for agent-driven requests
}
```

### 5.3 Session Type

```go
type CollaborationSession struct {
    ID           string
    Mode         string        // "pair_programming" | "differential"
    TaskID       string        // parent task
    State        SessionState  // created → active → converged | exhausted | failed
    Workspace    string        // base workspace path
    Participants []string      // agent IDs involved
    TurnLog      []TurnEntry   // complete turn history
    ParentID     string        // for nested (agent-initiated) sessions
    TokenBudget  int64         // remaining tokens, inherited from parent
    TimeBudget   time.Duration // remaining time, inherited from parent
    CreatedAt    time.Time
}

type SessionState string
const (
    SessionCreated   SessionState = "created"
    SessionActive    SessionState = "active"
    SessionConverged SessionState = "converged"
    SessionExhausted SessionState = "exhausted"
    SessionFailed    SessionState = "failed"
)
```

### 5.4 Registration

The engine is wired into the daemon alongside existing orchestrator components:

```go
engine := NewCollaborationEngine(CollaborationEngineDeps{
    Bus:         messageBus,
    Registry:    agentRegistry,
    Workspaces:  workspaceManager,
    PairManager: pairManager,
    Logger:      logger,
})
engine.RegisterMode("pair_programming", NewPairProgrammingDriver(...))
engine.RegisterMode("differential", NewDifferentialDriver(...))
```

## 6. Mode I: Pair Programming (Symmetric)

### 6.1 Concept
Two peer agents collaborate on a single task. They share a workspace and a conversation. Either can hold the "editor token" (be the active driver). The other observes, reviews, and may request the token.

### 6.2 Key Components

**`PairProgrammingDriver`**
- Owns the session lifecycle and the turn loop.
- Manages a shared `ConversationStore` keyed by session ID.

**`TurnManager`**
- Tracks who holds the editor token.
- Supports:
  - `yield` — agent voluntarily passes the token
  - `request_turn` — observer asks for the token
  - `force_yield` — after max tokens or timeout, the manager switches
- Initial token assignment: round-robin (agent A starts).

**Shared Workspace**
- Both agents read/write the same git-tracked directory:
  `~/.meept/workspaces/collab-{sessionID}/`
- After each agent turn, a commit is made so the other agent sees the diff.

### 6.3 Turn Lifecycle

1. **Driver turn:** The agent holding the token receives:
   - The original task description
   - Full conversation history
   - Git diff of what changed since its last turn
   - A system prompt indicating it is the current driver

2. **Driver writes:** The agent may write files, run tests, or ask questions via tools. All file changes land in the shared workspace.

3. **Commit:** The driver turn ends (agent calls `workspace_yield`, hits max tokens, or times out). The workspace manager commits changes.

4. **Observer turn:** The other agent receives:
   - The same full context
   - The latest git diff
   - A system prompt indicating it is the observer

5. **Observer response:** The observer can:
   - Approve and yield back ("looks good, continue")
   - Request the token ("I see a bug; let me fix it")
   - Challenge without taking token ("this function has an off-by-one; please fix")

6. **Token transfer:** If the observer requests the token, roles swap. If it approves, the turn returns to the driver (or the next agent if round-robin).

### 6.4 Terminal Conditions

| Condition | Result |
|-----------|--------|
| Both agents `yield` with "approve" in the same round | `SessionConverged` |
| Max turns reached (default 10) | `SessionExhausted` — last driver output returned |
| Agent error (tool panic, LLM failure) | `SessionFailed` |
| User `cancel` event on bus | `SessionFailed` |

### 6.5 Tool: `workspace_yield`

A new built-in tool available only to agents in a pair programming session:

```json
{
  "name": "workspace_yield",
  "description": "End your turn as the active driver. Optionally approve the current state, request changes, or request the token.",
  "parameters": {
    "type": "object",
    "properties": {
      "action": {
        "type": "string",
        "enum": ["approve", "request_changes", "request_token"],
        "description": "approve = pass turn to other agent; request_changes = ask other to fix something; request_token = take over as driver"
      },
      "feedback": {
        "type": "string",
        "description": "Context for the other agent (e.g. 'the sort function looks correct but add a nil check')"
      }
    },
    "required": ["action"]
  }
}
```

### 6.6 Integration with Existing Pair System

The existing `PairManager` remains unchanged for actor-reviewer workflows. The `CollaborationEngine`'s `PairProgrammingDriver` implements the symmetric variant as a separate mode. Both can coexist:
- `IntentPair` (existing) → `PairManager` (asymmetric, actor-reviewer)
- `IntentCollaborate` (new) → `CollaborationEngine.PairProgrammingDriver` (symmetric, peer)

## 7. Mode II: Differential Development (A/B + Review + Differentiate)

### 7.1 Concept
Two independent models implement the same task in isolated worktrees. Each implementation is validated by a reviewer. A third model (differentiator) compares both validated implementations and synthesizes a combined result.

### 7.2 Workspace Layout

```
workspaces/diff-{sessionID}/
├── branch-a/          # Agent A implementation
│   ├── .git
│   └── ... (code)
├── branch-b/          # Agent B implementation
│   ├── .git
│   └── ... (code)
├── combined/          # Differentiator synthesis
│   ├── .git
│   └── ... (merged code)
└── meta/              # Comparison notes, diffs, evaluation scores
    ├── diff.patch     # diff between branch-a and branch-b
    ├── eval.json      # Differentiator's evaluation
    └── plan.md        # Original plan/spec
```

### 7.3 Four-Phase Pipeline

| Phase | Description | Parallel? | Gate |
|-------|-------------|-----------|------|
| **1. Fork** | Task plan copied to both branches | Yes | — |
| **2. Implement & Review** | Each branch runs its own actor-reviewer loop via the **existing `PairManager`** | Yes (A and B independently) | **Reviewer approval** required |
| **3. Validate Checkpoint** | Git tags `branch-a-approved` / `branch-b-approved`. If one fails review exhaustion, fall back to the other. | Sequential | Both approved (or fallback) |
| **4. Differentiate & Synthesize** | Differentiator reads both approved implementations, evaluates, and writes a combined version in `combined/` | Sequential | — |

### 7.4 Phase 2: Reusing PairManager per Branch

The `DifferentialDriver` spawns **two independent pair sessions**:

```go
sessionA := pairMgr.CreateSession(taskID, spec, "coder-A", "code-reviewer", maxRounds)
sessionB := pairMgr.CreateSession(taskID, spec, "coder-B", "code-reviewer", maxRounds)
```

Each uses its own workspace (`branch-a/` or `branch-b/`). The `PairManager` handles actor-reviewer rounds exactly as it does today. No new review logic is needed.

**Model overrides:** The same agent type (e.g., `coder`) may need to run with different models in branches A and B. The `DifferentialDriver` achieves this by creating lightweight cloned specs with model overrides:

```go
specA := registry.CloneSpecWithModel("coder", "model-a")
specB := registry.CloneSpecWithModel("coder", "model-b")
```

These clones are registered temporarily and removed when the differential session completes.

**Reviewer scope:** The reviewer's job is **plan completeness validation**, not style critique:
- Are all functions from the plan implemented?
- Do tests pass (if test commands are part of the plan)?
- Are error cases handled?
- Is the API surface from the plan present?

### 7.5 Phase 4: Differentiator

The differentiator receives:
1. The original task plan
2. Agent A's implementation (full source, directory tree, git log)
3. Agent B's implementation (same)
4. Each branch's review summary (approved or rejected, with issues)

**Evaluation criteria (prompted to differentiator):**
- Correctness: Does each implementation meet the spec?
- Completeness: Any missing components?
- Edge-case handling: Which handles errors/race conditions better?
- Idiomatic quality: Which is cleaner, more maintainable?
- Test coverage: Which has better coverage?

**Synthesis strategies (configurable per session):**
- `cherry_pick` (default): Extract best parts from both, write unified implementation.
- `winner_takes_all`: Pick the better implementation, apply the other's improvements as patches.
- `analysis_only`: Return a comparative report; do not write code.

### 7.6 Fallback Behavior

| Scenario | Action |
|----------|--------|
| Branch A converged, Branch B exhausted | Differentiator runs with A only + notice that B failed |
| Both branches exhausted | Session fails; dispatcher/user is notified with both raw outputs |
| Differentiator fails or produces garbage | User can manually inspect `branch-a/` and `branch-b/`; both are git-tagged |

### 7.7 Cost Considerations

Differential development is expensive:
- 2× implementation agents (parallel)
- 2× reviewer agents (sequential to their respective actors)
- 1× differentiator agent (sequential to both branches)

This is expected for high-stakes tasks. The dispatcher should **suggest** differential mode for complex tasks and confirm with the user before proceeding.

## 8. Mode III (Hybrid): Agent-Initiated Collaboration

### 8.1 Concept
An executing agent can request a collaboration session mid-task. This is gated to prevent runaway token burn and cascading requests.

### 8.2 Tool: `initiate_collaboration`

Available to all agents in baseline tools:

```json
{
  "name": "initiate_collaboration",
  "description": "Request a collaborative session with another agent when facing an ambiguous or complex problem.",
  "parameters": {
    "type": "object",
    "properties": {
      "mode": {
        "type": "string",
        "enum": ["pair_programming", "differential"],
        "description": "Collaboration mode to use"
      },
      "task_description": {
        "type": "string",
        "description": "Description of what needs collaboration"
      },
      "reason": {
        "type": "string",
        "description": "Why collaboration is needed (e.g. 'uncertain about the best architecture')"
      },
      "preferred_agents": {
        "type": "array",
        "items": { "type": "string" },
        "description": "Optional agent IDs to involve"
      }
    },
    "required": ["mode", "task_description", "reason"]
  }
}
```

### 8.3 Guardrails

| Gate | Behavior |
|------|----------|
| **Budget check** | Parent session must have remaining token budget > estimated cost of sub-session |
| **Depth limit** | Max nesting depth = 1 (default). No collaboration-within-collaboration. Override via config. |
| **Dispatcher approval** (optional) | If `collaboration.require_dispatcher_approval: true`, publish `collaboration.requested` event; dispatcher has 10s to approve/override |
| **Timeout inheritance** | Sub-session gets `parent_timeout / 2` minimum |
| **Result injection** | Sub-session result is injected back into the parent agent's conversation as a tool result |

### 8.4 Example Flow

1. Coder agent implements a feature but encounters ambiguous architecture
2. Coder calls `initiate_collaboration` with mode=`pair_programming`, preferred_agent=`planner`
3. `CollaborationEngine` checks budget and depth, creates nested session
4. Coder and planner debate design in shared workspace (3 turns)
5. Planner proposes a design; coder approves
6. Session converges; result (design decision) returned to parent coder agent
7. Coder continues implementation with resolved design

## 9. Bus Topics & Event Flow

### 9.1 New Topics

| Topic | Direction | Payload |
|-------|-----------|---------|
| `collaboration.session_created` | Engine → bus | Session ID, mode, participants, task ID |
| `collaboration.turn_completed` | Engine → bus | Session ID, agent ID, turn number, action (yield/request_token) |
| `collaboration.phase_completed` | DifferentialDriver → bus | Session ID, phase number (1–4), branch |
| `collaboration.consensus_reached` | Engine → bus | Session ID, converged after N turns |
| `collaboration.divergence` | Engine → bus | Session ID, agents disagree, escalation needed |
| `collaboration.result` | Engine → bus | Session ID, final output, workspace path |
| `collaboration.error` | Engine → bus | Session ID, error message, phase |
| `collaboration.requested` | Agent → bus | Parent session ID, mode, reason, preferred agents |

### 9.2 Orchestrator Integration

The existing `Orchestrator` adds handlers for `collaboration.*` topics alongside existing `pair.*` handlers:

```go
topics := map[string]func(context.Context, *models.BusMessage){
    // existing topics...
    "collaboration.session_created": o.handleCollabSessionCreated,
    "collaboration.consensus_reached": o.handleCollabConsensus,
    "collaboration.error": o.handleCollabError,
    // etc.
}
```

The orchestrator never inspects mode internals — it just routes events to the `CollaborationEngine`.

## 10. Dispatcher Integration

### 10.1 Intent Classification

New intent type:

```go
const IntentCollaborate IntentType = "collaborate"
```

Keywords: `collaborate`, `pair program`, `debate`, `A/B test`, `differential`, `compare approaches`

**Distinction from `IntentPair`:** `IntentPair` routes to the existing `PairManager` for structured actor-reviewer loops (e.g., "review this code with a second set of eyes"). `IntentCollaborate` routes to the `CollaborationEngine` for symmetric peer collaboration or differential comparison. The dispatcher classifies based on keywords and context.

### 10.2 Routing

| Input | Dispatcher Action |
|-------|-------------------|
| "collaborate with coder and planner on refactoring X" | Classify `IntentCollaborate` → route to `CollaborationEngine` with mode=`pair_programming` and agent list |
| "implement X with A/B comparison" | Classify `IntentCollaborate` → route to `CollaborationEngine` with mode=`differential` |
| "implement X" (complex, high-confidence task) | Optionally suggest: "This looks complex. Run with A/B comparison for higher confidence? [y/n]" |
| Regular task (no collaboration keywords) | Existing routing unchanged |

### 10.3 Configurable Suggestion Heuristic

The dispatcher can suggest differential mode based on heuristics (future work):
- Task involves >5 files
- Task contains ambiguous requirements
- Model assignment includes multiple capable models

User confirmation is always required (opt-in, not automatic).

## 11. Error Handling & Edge Cases

| Edge Case | Handling |
|-----------|----------|
| Pair programming: agent never yields | `TurnManager` force-yields after `max_tokens_per_turn` or `turn_timeout` |
| Pair programming: endless back-and-forth | Max turns (default 10); SessionExhausted |
| Differential: one branch fails review | Phase 3 runs with available branch + notice; or escalate to user |
| Differential: both branches fail | SessionFailed; both workspaces preserved for manual inspection |
| Differential: differentiator produces invalid code | User can inspect `branch-a/` and `branch-b/`; both are git-tagged checkpoints |
| Nested collaboration exceeds budget | `ErrBudgetExceeded` returned to parent agent; parent continues solo or escalates |
| Nested collaboration exceeds depth | `ErrDepthExceeded` returned; agent must resolve alone |
| Workspace merge conflicts (pair) | Git conflict markers presented to next agent as part of its prompt |
| Agent crashes mid-turn | SessionFailed; partial workspace committed for forensic inspection |

## 12. Testing Strategy

### 12.1 Unit Tests

- `PairProgrammingDriver`: mock agents, in-memory workspace, verify turn sequence and token transfer
- `TurnManager`: edge cases (force-yield, request_turn approval/denial)
- `DifferentialDriver`: mock PairManager calls, verify four-phase ordering
- `CollaborationEngine`: mode registration, session lifecycle, nested session guardrails

### 12.2 Integration Tests

- Full pair programming session with real LLM (llama.cpp local): two agent IDs, shared workspace, verify files exist after convergence
- Full differential session: verify `branch-a/` and `branch-b/` are independently created and `combined/` exists after Phase 4
- Agent-initiated collaboration: verify parent agent receives sub-session result in conversation

### 12.3 Regression Tests

- All existing `PairManager` and `PairOrchestrator` tests must pass unchanged
- Existing actor-reviewer flows must not be affected by the new engine

## 13. Migration & Backward Compatibility

- The `CollaborationEngine` is an **optional** component. If not wired in the daemon, existing behavior is unchanged.
- Existing bus topics (`pair.*`) remain untouched.
- New bus topics (`collaboration.*`) are only published if the engine is active.
- No changes to `AgentSpec`, `AgentLoop`, or `AgentRegistry`.

## 14. Implementation Order

1. **CollaborationEngine scaffold** — types, registry, bus wiring
2. **DifferentialDriver** — four-phase pipeline, PairManager reuse (simpler; reuses existing actor-reviewer logic)
3. **PairProgrammingDriver** — symmetric turn loop, TurnManager, `workspace_yield` tool (new paradigm)
4. **Agent-initiated collaboration** — `initiate_collaboration` tool, guardrails
5. **Dispatcher integration** — `IntentCollaborate`, routing, suggestion heuristic
6. **Observability** — bus events, metrics, session logging

## 15. Open Questions

1. Should the `workspace_yield` tool support a `run_tests` flag so agents can validate before yielding?
2. Should differential mode support >2 branches (A/B/C) for higher-confidence comparison?
3. Should pair programming sessions be streamable to the user in real-time (TUI updates per turn)?
