# Cluster E: Multi-Agent Scaling — N-Agent Teams Beyond Pairs

## Goal
Clarify and extend Meept's multi-agent execution model from pairs to N-agent teams, while maintaining compatibility with the existing collaboration plan.

## Background
User Question #5: "Meept has the ability for the orchestrator to create n+ tasks based on a plan, and then those n+ can be assigned to many agents with whatever model concurrency is configured — is this not correct?"

Partially correct, but there's nuance:
- **Plans** create N tasks assigned to appropriate agents
- **Pairs** let 2 agents collaborate on a single task
- **Team Mode** (OMO) has 1 lead + up to 8 parallel members with real-time communication
- **Existing collaboration plan** implements `CollaborationEngine`, `TurnManager`, `PairProgrammingDriver`, `DifferentialDriver`

Meept supports both parallel task execution (via the queue) and team collaboration (via the team mode). N agents work independently on N plan tasks simultaneously via the job queue. Teams coordinate and communicate through the team tools and per-session bus topics.

## Feature Checklist

### 1. Clarification: Parallel Task Execution (Current)
- Plans create tasks → tasks go to bus → agents pull matching tasks from queue
- This is parallel execution, not team collaboration
- Each agent works independently; no inter-agent communication during execution

### 2. Team Mode (Implemented)
- OMO: Lead agent orchestrates a team of specialists
- Team communicates via `team_*` tools (create, send_message, assign, status, result)
- Built-in team roles: hyperplan (5 critics), security-research (hunters + PoC engineers)

### 3. Hybrid Approach: Collaboration Plan + Team Mode
- **Existing plan**: Pair-based collaboration (pair_programming, differential)
- **Team Mode**: N-agent parallel teams (hyperplan, security-research)
- Both coexist: `CollaborationEngine` manages modes, `Pair` is one mode, `Team` is another

## Implementation Plan

### Phase 1: Document Current Parallelism (Complete)
1. Verify: plan -> tasks -> bus -> agents pulling from queue
2. Agents coordinate during parallel execution via per-session bus topics
3. Each agent works independently within the fan-out phase, then the lead synthesizes

### Phase 2: Extend Collaboration Engine for Team Mode (Complete)
1. `team_parallel` collaboration mode registered alongside `pair_programming` and `differential`
2. Team configuration: lead agent + specialist roster
3. Team uses message bus for coordination:
   - `team.{sessionID}.status` — shared task board
   - `team.{sessionID}.message` — inter-agent communication
   - `team.{sessionID}.result` — partial results aggregation
4. Reuses existing `PairManager`, `WorkspaceManager` infrastructure

### Phase 3: Team Tools (Complete)
1. `platform_team_create` — Lead agent initiates a team (publishes to `team.start` bus topic)
2. `team_preset_create` — Create a team from a predefined preset
3. `team_assign` — Assign subtask to team member (publishes to per-session message topic)
4. `team_status` — Check progress of all team members (reads from `TeamOrchestrator` state)
5. `team_message` — Send broadcast or targeted message (publishes to per-session message topic)
6. `team_result` — Submit partial result (updates team state and publishes to result topic)

### Phase 4: Preset Teams (Complete)
1. `hyperplan` — 5 diverse critic agents review a plan simultaneously
2. `security_research` — 3 hunters + 2 PoC engineers audit code

## Compatibility with Existing Collaboration Plan
- The existing plan (docs/superpowers/plans/2026-06-05-multi-agent-collaboration.md) builds PairProgrammingDriver and DifferentialDriver
- Team Mode adds a new driver (`ParallelTeamDriver`) to CollaborationEngine
- TurnManager still manages editor tokens; Team Mode uses a simpler round-robin for lead review
- **No overlap** — pairs are for 2-agent focused work, teams are for N-agent parallel work
- Implemented after existing collaboration was merged

## Implemented Files

### Core Components
- `internal/agent/collaboration_engine.go` — CollaborationEngine with `team_parallel` mode registration
- `internal/agent/collaboration_team_driver.go` — `ParallelTeamDriver` (fan-out + synthesis pipeline)
- `internal/agent/collaboration.go` — Shared types: `CollaborationSession`, `CollaborationMode`, `SessionConfig`, bus topics
- `internal/agent/collaboration_errors.go` — Error codes and sentinel errors

### Team Components
- `internal/agent/team_orchestrator.go` — `TeamOrchestrator` (bus-driven lifecycle, state queries, messaging)
- `internal/agent/team_presets.go` — Built-in preset definitions (hyperplan, security_research)

### Tools
- `internal/tools/builtin/team.go` — 6 team tools with callback wiring
- `internal/tools/builtin/collaboration.go` — `initiate_collaboration` tool
- `internal/tools/builtin/workspace_yield.go` — `workspace_yield` tool

### Daemon Wiring
- `internal/daemon/components.go` — Full wiring: CollaborationEngine → TeamOrchestrator → tool callbacks → bus publish

## Success Criteria
- [x] Parallel team of 4 agents can review a plan simultaneously
- [x] Lead agent aggregates results and synthesizes final output
- [x] Team mode doesn't interfere with existing pair collaboration
- [x] Existing collaboration plan work remains compatible
