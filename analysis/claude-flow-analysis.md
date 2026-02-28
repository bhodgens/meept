# Claude-Flow (Ruflo V3) Analysis

## Overview

Claude-flow (rebranded as "Ruflo") is a TypeScript-based multi-agent orchestration framework designed to work with Claude Code. It provides swarm coordination, vector memory, self-learning capabilities, and multi-provider LLM support.

**Key Philosophy**: Claude-flow acts as an *orchestrator/ledger* (tracks state, stores memory, coordinates) while actual execution happens via external tools (Claude Code's Task tool, CLI commands). It doesn't execute code itself.

---

## Core Architecture Concepts

### 1. Swarm Coordination

**Topology Types**:
- `hierarchical`: Central queen/coordinator, prevents drift
- `mesh`: Peer-to-peer, all agents connected
- `ring`: Sequential message passing
- `star`: Hub-and-spoke

**Anti-Drift Pattern**:
- Hierarchical topology with 6-8 agents max
- Raft consensus for leader authority
- Short task cycles with verification gates
- Shared memory namespace

### 2. Consensus Algorithms

Three distributed consensus implementations:

| Algorithm | Use Case | Fault Tolerance |
|-----------|----------|-----------------|
| **Raft** | Leader election, log replication | Leader-based, strong consistency |
| **Byzantine (BFT)** | Untrusted agents, weighted voting | f < n/3 failures |
| **Gossip** | Eventual consistency, large swarms | High partition tolerance |

**Raft Implementation Highlights**:
- Standard leader/follower/candidate states
- Term-based voting with log replication
- Configurable election timeouts (150-300ms)
- Heartbeat intervals (50ms)

### 3. Queen Coordinator (Hive Mind)

Central orchestrator for 15-agent swarms:

**Task Analysis**:
- Complexity scoring (0-1)
- Duration estimation
- Required capabilities extraction
- Pattern matching from ReasoningBank
- Sub-task decomposition

**Delegation Planning**:
- Primary + backup agent assignments
- Execution strategies: sequential, parallel, pipeline, fan-out-fan-in
- Capability-based scoring (load, performance, health, availability)

**Queen Types**:
- Strategic (planning)
- Tactical (execution)
- Adaptive (optimization)

### 4. Memory System

**Three Layers**:

1. **HNSW Vector Search** (via AgentDB):
   - 150x-12,500x faster than brute-force
   - Local embeddings via ONNX Runtime (75x faster)
   - Hyperbolic (Poincaré ball) embeddings for hierarchical data

2. **Knowledge Graph**:
   - PageRank for importance scoring
   - Community detection (Louvain/label propagation)
   - Edge types: reference, similar, temporal, co-accessed, causal
   - Graph-aware result ranking

3. **3-Scope Agent Memory** (Claude Code compatible):
   - `project`: Shared, committed to git
   - `local`: Machine-specific, gitignored
   - `user`: Global per-user (~/.claude/agent-memory/)

### 5. Self-Learning (ReasoningBank)

4-step learning pipeline:

1. **RETRIEVE**: Top-k memory injection with MMR diversity
2. **JUDGE**: LLM-as-judge trajectory evaluation
3. **DISTILL**: Extract strategy memories from trajectories
4. **CONSOLIDATE**: Dedup, detect contradictions, prune old

**Performance Targets**:
- Retrieval: <10ms
- Learning step: <10ms
- Consolidation: <100ms

**Neural Components**:
- SONA: Self-Optimizing Neural Architecture (<0.05ms adaptation)
- EWC++: Elastic Weight Consolidation (prevents catastrophic forgetting)
- MicroLoRA: Lightweight fine-tuning (128x compression)
- 9 RL algorithms: Q-Learning, SARSA, A2C, PPO, DQN, etc.

### 6. Hooks System

**17 Hook Events**:
- PreToolUse, PostToolUse, SessionStart, SessionEnd
- PreEdit, PostEdit, PreRead, PostRead
- PreCommand, PostCommand, PreTask, PostTask
- AgentSpawn, AgentTerminate, PreRoute, PostRoute
- PatternLearned, PatternConsolidated

**12 Background Workers**:
Auto-dispatch on triggers (file changes, patterns, sessions)

**Official Claude Code Integration**:
- Maps to official hook events (PreToolUse, PostToolUse, etc.)
- Tool matchers for granular filtering
- Decision outputs (allow, deny, block, ask)

### 7. Agent Booster (WASM)

Skip LLM entirely for simple transforms (<1ms):

| Transform | Description |
|-----------|-------------|
| `var-to-const` | Convert var/let to const |
| `add-types` | Add TypeScript annotations |
| `add-error-handling` | Wrap in try/catch |
| `async-await` | Convert promises |
| `add-logging` | Add console.log |
| `remove-console` | Strip console.* |

**Cost Impact**: 352x faster, $0 per operation

### 8. Multi-Provider LLM

**6 Providers**:
- Claude (Anthropic)
- GPT (OpenAI)
- Gemini (Google)
- Cohere
- Ollama (local)
- Custom endpoints

**Routing Logic**:
- Automatic failover
- Cost-based routing (85% savings claimed)
- Quality requirements matching

### 9. Security (AIDefence)

- Prompt injection detection with threat learning
- Input validation at boundaries
- Path traversal prevention
- Command injection blocking
- CVE remediation scanning

---

## Package Structure

```
v3/@claude-flow/
├── cli/           # 26 commands, 140+ subcommands
├── swarm/         # Topology, consensus, queen coordinator
├── memory/        # AgentDB, HNSW, knowledge graph
├── embeddings/    # ONNX, hyperbolic embeddings
├── hooks/         # 17 hooks + 12 workers
├── neural/        # ReasoningBank, SONA, LoRA
├── claims/        # Work stealing, load balancing
├── guidance/      # Governance control plane
├── aidefence/     # Threat detection, learning
├── security/      # Input validation, CVE
└── shared/        # Core types, interfaces
```

---

## Implementation Quality Notes

**Strengths**:
- Well-structured Domain-Driven Design
- Comprehensive TypeScript types
- Performance targets documented
- Clean separation of concerns
- Good test coverage

**Concerns**:
- Heavy reliance on external CLI commands
- Some "vaporware" features (performance claims unverified)
- Complex dependency tree
- Marketing-heavy documentation

---

## Key Differentiators

1. **Distributed Consensus**: Full Raft/Byzantine/Gossip implementations
2. **Knowledge Graph**: PageRank + community detection on memory
3. **Self-Learning**: ReasoningBank with trajectory distillation
4. **WASM Optimization**: Skip LLM for simple code transforms
5. **Multi-Provider**: 6 LLM providers with failover
6. **Hyperbolic Embeddings**: Better hierarchical code relationships
