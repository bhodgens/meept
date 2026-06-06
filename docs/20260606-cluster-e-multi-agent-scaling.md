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

The question is: does Meept currently let N agents work independently on N plan tasks simultaneously? Yes (via the queue). Does it let them communicate and coordinate like a team? Not really.

## Feature Checklist

### 1. Clarification: Parallel Task Execution (Current)
- Plans create tasks → tasks go to bus → agents pull matching tasks from queue
- This is parallel execution, not team collaboration
- Each agent works independently; no inter-agent communication during execution

### 2. Team Mode (New Capability)
- OMO: Lead agent orchestrates a team of specialists
- Team communicates via `team_*` tools (create, send_message, task_create, status)
- Real-time tmux visualization (optional, not critical for us)
- Built-in team roles: hyperplan (5 critics), security-research (hunters + PoC engineers)

### 3. Hybrid Approach: Collaboration Plan + Team Mode
- **Existing plan**: Pair-based collaboration (pair_programming, differential)
- **Team Mode**: N-agent parallel teams (hyperplan, security-research)
- Both can coexist: `CollaborationEngine` manages modes, `Pair` is one mode, `Team` is another

## Implementation Plan

### Phase 1: Document Current Parallelism
1. Verify: plan -> tasks -> bus -> agents pulling from queue
2. Identify gaps: do agents coordinate during parallel execution?
3. Answer: Not currently. Each agent runs independently.

### Phase 2: Extend Collaboration Engine for Team Mode
1. Add `team_parallel` collaboration mode alongside existing modes
2. Team configuration: lead agent + specialist roster
3. Team uses message bus for coordination:
   - `team.{sessionID}.status` — shared task board
   - `team.{sessionID}.message` — inter-agent communication
   - `team.{sessionID}.result` — partial results aggregation
4. Reuse existing `PairManager`, `WorkspaceManager` infrastructure

### Phase 3: Team Tools
1. `platform_team_create` — Lead agent initiates a team
2. `team_assign` — Assign subtask to team member
3. `team_status` — Check progress of all team members
4. `team_message` — Send broadcast or targeted message
5. `team_result` — Submit partial result

### Phase 4: Preset Teams
1. `hyperplan` — 5 diverse critic agents review a plan simultaneously
2. `security_research` — 3 hunters + 2 PoC engineers audit code

## Compatibility with Existing Collaboration Plan
- The existing plan (docs/superpowers/plans/2026-06-05-multi-agent-collaboration.md) builds PairProgrammingDriver and DifferentialDriver
- Team Mode adds a new driver (`ParallelTeamDriver`) to CollaborationEngine
- TurnManager still manages editor tokens; Team Mode uses a simpler round-robin for lead review
- **No overlap** — pairs are for 2-agent focused work, teams are for N-agent parallel work
- Can be implemented after existing collaboration is merged

## Files to Modify / Create
- `internal/agent/collaboration_engine.go` — Add team mode registration
- `internal/agent/collaboration_team_driver.go` (new) — Parallel team driver
- `internal/agent/team_orchestrator.go` (new) — Lead agent orchestration
- `internal/tools/builtin/team_*.go` (new) — Team coordination tools

## Success Criteria
- [ ] Parallel team of 4 agents can review a plan simultaneously
- [ ] Lead agent aggregates results and synthesizes final output
- [ ] Team mode doesn't interfere with existing pair collaboration
- [ ] Existing collaboration plan work remains compatible
