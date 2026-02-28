# Meept vs Claude-Flow: Detailed Comparison

## Executive Summary

| Aspect | Meept | Claude-Flow |
|--------|-------|-------------|
| **Language** | Go | TypeScript |
| **Architecture** | Daemon + CLI | CLI + MCP Server |
| **Agents** | 8 specialist types | 60+ agent types |
| **Consensus** | None (single leader) | Raft/Byzantine/Gossip |
| **Memory** | SQLite FTS5 + Memvid | HNSW + Knowledge Graph |
| **Learning** | Shadow training | ReasoningBank + SONA |
| **LLM Providers** | Single configurable | 6 providers + failover |
| **Security** | Tirith + SecurityEngine | AIDefence + validation |

---

## Feature-by-Feature Comparison

### 1. Multi-Agent Coordination

**Meept**:
- 8 specialist agents: dispatcher, chat, coder, debugger, planner, analyst, committer, scheduler
- Single dispatcher routes to specialists
- Keyword-based intent classification
- No distributed coordination (single process)

**Claude-Flow**:
- 60+ agent types with customization
- Queen-led hierarchical swarms
- Topology options: hierarchical, mesh, ring, star
- Distributed consensus (Raft, Byzantine, Gossip)

**Winner**: Claude-Flow
**Why**: Distributed consensus and topology management enable true multi-agent coordination. Meept's single-dispatcher model is simpler but less flexible.

**What Meept Could Adopt**:
- Topology manager for agent organization
- Anti-drift patterns (hierarchical default, task-scoped agents)
- Queen-type coordinator for complex tasks

---

### 2. Memory Systems

**Meept**:
- SQLite with FTS5 full-text search
- Memvid client for external vector memory
- Three memory types: episodic, task, personality
- Zone mapping to memvid namespaces
- Basic relevance scoring

**Claude-Flow**:
- HNSW vector search (150x-12,500x faster)
- Knowledge graph with PageRank
- Community detection (Louvain/label propagation)
- Hyperbolic embeddings for hierarchical data
- 3-scope system (project/local/user)
- Auto-edge creation from similarity

**Winner**: Claude-Flow
**Why**: Knowledge graph + PageRank provides structural understanding. Hyperbolic embeddings better capture code relationships.

**What Meept Could Adopt**:
- Knowledge graph layer on top of existing memory
- PageRank for memory importance scoring
- 3-scope memory organization (already partially exists)
- Graph-aware result ranking

---

### 3. Self-Learning

**Meept**:
- Shadow training captures successful interactions
- Domain classification (code, debugging, planning, analysis)
- Few-shot example injection after system prompt
- Basic pattern storage

**Claude-Flow**:
- ReasoningBank: 4-step pipeline (RETRIEVE→JUDGE→DISTILL→CONSOLIDATE)
- Trajectory-based learning with quality evaluation
- Pattern contradiction detection
- SONA: Self-Optimizing Neural Architecture (<0.05ms)
- EWC++: Prevents catastrophic forgetting
- MicroLoRA: Lightweight adaptation

**Winner**: Claude-Flow
**Why**: The 4-step learning pipeline is more sophisticated. Contradiction detection and consolidation prevent pattern drift.

**What Meept Could Adopt**:
- JUDGE step: Evaluate trajectory quality before storing
- CONSOLIDATE step: Dedup and prune old patterns
- Contradiction detection in memory storage
- Pattern evolution tracking

---

### 4. Hooks / Event System

**Meept**:
- Message bus pub/sub
- Topic subscriptions (exact + wildcard)
- Request/response pattern with reply channels
- Tool action categorization (shell_execute, file_read, etc.)

**Claude-Flow**:
- 17 hook events covering full lifecycle
- 12 background workers with triggers
- Integration with Claude Code's official hooks API
- Pre/post hooks for tools, tasks, sessions
- Learning hooks (PatternLearned, PatternConsolidated)

**Winner**: Claude-Flow
**Why**: Deeper integration with Claude Code hooks API. Background workers enable autonomous operation.

**What Meept Could Adopt**:
- Pre/post hooks for tools (pre-execution validation)
- Background workers for autonomous tasks
- Learning event hooks
- Session lifecycle hooks

---

### 5. Security

**Meept**:
- SecurityEngine: SQLite-backed permission checks
- InputSanitizer: Prompt injection detection (3 strictness levels)
- OutputMonitor: Credential redaction
- Tirith: Shell command scanning
- Financial operation blocking
- Audit logging

**Claude-Flow**:
- AIDefence: Threat detection with learning
- Input validation
- Path traversal prevention
- Command injection blocking
- CVE remediation scanning

**Winner**: Meept (slightly)
**Why**: Meept has more layered security with audit trails. Financial blocking is unique. Claude-Flow's AIDefence learning is interesting but less mature.

**What Meept Could Adopt**:
- Threat learning (learn from detected attacks)
- CVE scanning integration

---

### 6. LLM Integration

**Meept**:
- Single provider configuration
- Capability-based model selection
- Budget enforcement (tokens, cost, rate limits)
- Model escalation for complex tasks

**Claude-Flow**:
- 6 providers with automatic failover
- Cost-based routing
- Agent Booster: WASM for simple transforms (skip LLM)
- 3-tier routing: WASM → cheap model → expensive model

**Winner**: Claude-Flow
**Why**: Multi-provider failover and WASM optimization significantly reduce costs. 3-tier routing is clever.

**What Meept Could Adopt**:
- Multi-provider support with failover
- WASM-based simple code transforms
- 3-tier complexity-based routing
- Cost optimization tracking

---

### 7. Task Orchestration

**Meept**:
- Planner decomposes tasks into steps
- CollaborativePlanner for approval workflow
- WorkspaceManager with git-backed tracking
- Task dependencies via blockedBy

**Claude-Flow**:
- Queen coordinator analyzes complexity
- Sub-task decomposition with dependency graphs
- Execution strategies: sequential, parallel, pipeline, fan-out-fan-in
- Claims system for work stealing and load balancing

**Winner**: Tie
**Why**: Both have sophisticated planning. Claude-Flow has more execution strategies; Meept has better workspace management.

**What Meept Could Adopt**:
- Execution strategy options (parallel, pipeline)
- Work stealing for load balancing
- Complexity analysis before planning

---

### 8. Communication

**Meept**:
- Unix socket JSON-RPC
- Message bus pub/sub
- Proxy handler for external agents
- Connection pooling with timeouts

**Claude-Flow**:
- MCP server integration
- Shared memory namespaces
- Cross-agent knowledge transfer
- Event-driven coordination

**Winner**: Tie
**Why**: Different approaches. Meept's RPC is more traditional; Claude-Flow leverages MCP protocol.

---

## Features Claude-Flow Has That Meept Lacks

### High Value for Meept

1. **Knowledge Graph + PageRank**
   - Memory relationships beyond flat storage
   - Importance scoring for context injection
   - Community detection for related concepts

2. **ReasoningBank Learning Pipeline**
   - JUDGE: Evaluate before storing patterns
   - CONSOLIDATE: Dedup, contradiction detection, pruning
   - Pattern evolution tracking

3. **Multi-Provider LLM with Failover**
   - Automatic switching on provider failure
   - Cost-based routing
   - Provider health monitoring

4. **WASM Code Transforms**
   - Skip LLM for simple transforms
   - 352x faster for var→const, add-types, etc.
   - $0 cost for handled operations

5. **Background Workers**
   - Autonomous task execution on triggers
   - Continuous optimization
   - Security audits

### Medium Value for Meept

6. **Distributed Consensus**
   - Raft for leader election
   - Byzantine for untrusted environments
   - Useful if multi-instance deployment needed

7. **Hyperbolic Embeddings**
   - Better hierarchical code relationships
   - Improved semantic similarity for nested structures

8. **Topology Manager**
   - Dynamic agent organization
   - Anti-drift patterns

### Low Value for Meept

9. **60+ Agent Types**
   - Meept's 8 specialists cover most use cases
   - Too many types can increase complexity

10. **Claude Code Official Hooks**
    - Meept is standalone daemon, not Claude Code plugin
    - Different integration model

---

## Features Meept Has That Claude-Flow Lacks

1. **Native Go Performance**
   - Single binary deployment
   - Lower memory footprint
   - Faster startup

2. **Layered Security with Audit Trail**
   - Multiple security layers (sanitizer, engine, tirith)
   - SQLite-backed audit log
   - Financial operation blocking

3. **Git-Backed Workspaces**
   - Auto-commit at lifecycle stages
   - PLAN.md, REVIEW.md, LOG.md artifacts
   - Full audit trail in git

4. **Memvid Integration**
   - External vector service support
   - Graceful fallback to SQLite

5. **Telegram/Web Communication**
   - Built-in comm channels
   - Not dependent on Claude Code

---

## Recommendations for Meept

### Priority 1: Knowledge Graph Layer

Add PageRank and community detection to existing memory:
- Build on SQLite FTS5 foundation
- Add reference tracking between memories
- Compute importance scores for context injection

### Priority 2: Learning Pipeline Enhancement

Extend shadow training with:
- Quality evaluation before storing (JUDGE step)
- Deduplication and contradiction detection (CONSOLIDATE)
- Pattern evolution tracking

### Priority 3: Multi-Provider LLM

Add provider failover:
- Abstract LLM client interface
- Health monitoring per provider
- Cost-based routing when multiple available

### Priority 4: WASM Simple Transforms

For simple code edits:
- Detect simple transform patterns
- Skip LLM for var→const, add imports, etc.
- Significant cost savings

### Priority 5: Background Workers

Add autonomous task system:
- Trigger-based worker dispatch
- Continuous memory optimization
- Scheduled security scans

---

## Architectural Comparison

```
MEEPT:                              CLAUDE-FLOW:
┌─────────────────┐                 ┌─────────────────┐
│   CLI / TUI     │                 │   CLI / MCP     │
└────────┬────────┘                 └────────┬────────┘
         │                                   │
┌────────▼────────┐                 ┌────────▼────────┐
│  Unix RPC       │                 │  MCP Server     │
└────────┬────────┘                 └────────┬────────┘
         │                                   │
┌────────▼────────┐                 ┌────────▼────────┐
│  Message Bus    │                 │  Swarm Coord    │
└────────┬────────┘                 │  + Consensus    │
         │                          └────────┬────────┘
┌────────▼────────┐                          │
│  Agent Loop     │◄───────────────►┌────────▼────────┐
│  (8 specialists)│                 │  Queen Coord    │
└────────┬────────┘                 │  (60+ agents)   │
         │                          └────────┬────────┘
┌────────▼────────┐                          │
│  Tool Registry  │                 ┌────────▼────────┐
└────────┬────────┘                 │  ReasoningBank  │
         │                          │  + HNSW Memory  │
┌────────▼────────┐                 └────────┬────────┘
│  SQLite + Memvid│                          │
│  (FTS5 Memory)  │                 ┌────────▼────────┐
└─────────────────┘                 │  Multi-Provider │
                                    │  LLM + WASM     │
                                    └─────────────────┘
```

---

## Conclusion

Claude-flow brings sophisticated concepts that could enhance meept:

1. **Immediate value**: Knowledge graph, learning pipeline improvements
2. **Medium-term**: Multi-provider LLM, WASM transforms
3. **Long-term**: Distributed consensus (if multi-instance needed)

Meept's Go architecture provides performance and deployment advantages that shouldn't be abandoned. The recommendation is to selectively adopt concepts that fit meept's daemon model rather than wholesale architectural changes.
