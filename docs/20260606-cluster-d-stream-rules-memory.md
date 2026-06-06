# Cluster D: Stream Rules, Deep Init, and Memory Evolution

## Goal
Integrate TTSR with streaming output, implement deep project initialization (`/init`), and evaluate OMO's hindsight memory pattern.

## Background
Meept's TTSR (`internal/agent/ttsr.go`) monitors LLM output for pattern violations but may not be deeply integrated with the streaming path. oh-my-pi has "time-traveling stream rules" that abort mid-token and inject rules. Meept also needs `/init` to generate hierarchical `AGENTS.md` files for token efficiency.

## Feature Checklist

### 1. TTSR Streaming Integration
- Current: `CheckDelta` is called on complete response
- Target: Integrate with streaming so rules trigger mid-token
- When a rule matches a streaming delta:
  - Abort the current generation
  - Inject rule content as a system reminder
  - Retry from the same point
- Rules survive context compaction (already partially supported)

### 2. Deep Project Initialization (`/init`)
- Walk project directory tree
- Generate hierarchical `AGENTS.md` files:
  - Root: project-wide conventions, architecture
  - Src dirs: domain-specific rules
  - Component dirs: specific component behaviors
- Use AST parsing for accurate symbol extraction
- Shorthand notation for token conservation
- Auto-load relevant `AGENTS.md` based on working context

### 3. Hindsight/Retain/Recall Memory Pattern
- OMO: `retain` queues facts into a "Hindsight bank", `recall` searches it
- Meept: Memory has episodic/task tiers but no explicit curation
- Evaluate if we need explicit agent-curated memory vs. automatic storage
- If valuable, add `retain`/`recall`/`reflect` tools

## Implementation Plan

### Phase 1: TTSR Streaming Hook
1. Modify `internal/llm/client.go` streaming to accept an `OnDelta` callback
2. TTSRManager.CheckDelta receives each streaming chunk
3. On match: cancel stream, return `ErrStreamAborted` with rule info
4. Orchestrator retries with rule content prepended to messages

### Phase 2: Deep Init Command
1. Create `internal/project/init_deep.go`
2. Tree-walk project, use `ast.ParseFile` for symbol extraction
3. Generate concise `AGENTS.md` for each significant subtree
4. Add CLI command: `meept init`
5. Load relevant `AGENTS.md` into prompts based on file context

### Phase 3: Memory Curation (evaluate)
1. Design: Would `retain`/`recall`/`reflect` tools improve memory quality?
2. If yes: add to tool registry, backed by existing memory store
3. If no: document why automatic storage is sufficient

## Files to Modify / Create
- `internal/llm/client.go` — Streaming hook for TTSR
- `internal/agent/ttsr.go` — Mid-stream abortion and retry
- `internal/project/init_deep.go` (new) — Deep init logic
- `cmd/meept/init.go` (new) — `meept init` CLI command
- `internal/memory/retain.go` (new, optional) — Hindsight-style tools

## Success Criteria
- [ ] TTSR rules trigger mid-stream and retry with injected content
- [ ] `meept init` generates correct hierarchical `AGENTS.md` in a test project
- [ ] Working on a file auto-loads relevant `AGENTS.md` into context
- [ ] Memory quality decision documented; implementation complete if approved
