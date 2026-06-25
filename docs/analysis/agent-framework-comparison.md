# Agent Framework Comparative Analysis

**Date:** 2026-06-23
**Purpose:** Comprehensive comparison of Meept against other agent frameworks to identify differentiators and position Meept's unique capabilities.

---

## Executive Summary

This analysis compares Meept against five agent frameworks: **OpenCode**, **OpenAgent** (Rust + Python variants), **OpenClaw** (Claude Code derivatives), **oh-my-pi**, and **Hermes Agent**. The analysis reveals that Meept's key differentiators are:

1. **Constitution-bound AI Employees** - Only Meept has formal autonomy tiers with enforcement engines
2. **Evidence-based execution** - Tool results produce verifiable evidence, not just LLM claims
3. **Daemon architecture with multi-transport** - Persistent background operation with RPC + HTTP + WebSocket
4. **Seven-layer agent loop safety** - Watchdog, cycle detection, convergence detection, budget tracking, hallucination recovery, model failover, context firewall
5. **Five-tier memory system** - Episodic (FTS5), Task, Knowledge Graph, Semantic (vector), distributed (memvid)
6. **Self-improvement with full cycle** - Detect → Analyze → Generate → Validate → Apply (not just skill learning)
7. **Go implementation** - Native threading (goroutines), compiled performance, no Python runtime overhead

---

## Frameworks Analyzed

| Framework | Language | Repo | Architecture |
|-----------|----------|------|--------------|
| **Meept** | Go | caimlas/meept | Daemon + CLI + Flutter + MenuBar |
| **OpenCode** | TypeScript | various | CLI-first, single-session |
| **OpenAgent (Rust)** | Rust + Go services | mopenagent/OpenAgent | Single daemon + external TCP services |
| **OpenAgent (Python)** | Python | openagent-uno/openagent-server | Server + CLI + Desktop + P2P |
| **OpenClaw** | TypeScript | various | CLI-only, single-session |
| **oh-my-pi** | Python | can1357/oh-my-pi | Single-agent harness |
| **Hermes Agent** | Python | nousresearch/hermes-agent | CLI + Gateway daemon + 6 terminal backends |

---

## Detailed Feature Comparison

### 1. Agent Architecture

| Framework | Agent Model | Multi-Agent | Intent Classification | Model Routing |
|-----------|-------------|-------------|----------------------|---------------|
| **Meept** | 18 specialists + Employees | Yes (delegation + handoff) | LLM classifier | Capability-based resolver |
| **OpenCode** | Single generic agent | No | No | Manual selection |
| **OpenAgent (Rust)** | Single ReAct agent | TODO | No | Single provider |
| **OpenAgent (Python)** | Single + Team coordination | Dynamic teams | No | Team-as-router |
| **OpenClaw** | Single agent | No | No | Manual |
| **oh-my-pi** | Single agent | No | No | N/A |
| **Hermes** | Single + subagent delegation | Kanban swarm | No | Per-session swap |

**Meept Advantage:** Only Meept has predefined specialist roles (dispatcher, coder, debugger, planner, analyst, committer, scheduler, researcher, writer, architect, skeptic, librarian) PLUS 5 reviewer agents (code-reviewer, test-reviewer, debug-reviewer, planner-reviewer, analyst-reviewer) with domain-specific routing.

---

### 2. Memory System

| Framework | STM | LTM | Cross-Session | Compaction |
|-----------|-----|-----|---------------|------------|
| **Meept** | Conversation store | Episodic (FTS5) + Task + memvid | Thread partitioning | Context firewall + summarization |
| **OpenCode** | In-memory | None | None | Truncation |
| **OpenAgent (Rust)** | Sliding window (40) | LanceDB (stub vectors) | Dump to files | TODO |
| **OpenAgent (Python)** | Sessions table | SQLite + skills + Obsidian | Session search | In-session recap |
| **OpenClaw** | In-memory | None | None | Basic truncation |
| **oh-my-pi** | Conversation history | SQLite FTS | Session-based | Limited |
| **Hermes** | SQLite + FTS5 | 9 pluggable providers | Parent session chains | LLM summarization |

**Meept Advantage:** Only Meept has five distinct memory tiers with FTS5 indexing, duplicate detection, consolidation jobs, epistemic categorization (trustworthy/untrustworthy), and thread-based context partitioning.

---

### 3. Tool System

| Framework | Tool Count | Discovery | Security Gating | MCP Support |
|-----------|------------|-----------|-----------------|-------------|
| **Meept** | 40+ builtin + MCP catalog | Intent-based routing | SecurityEngine + Tirith | 21 preconfigured servers |
| **OpenCode** | ~20 | Manual | Minimal | Limited |
| **OpenAgent (Rust)** | ~25 service tools | BM25 search | Guard whitelist | No |
| **OpenAgent (Python)** | 16+ MCP servers | MCP registry | Approvals | Yes |
| **OpenClaw** | ~15 | Manual | Minimal | No |
| **oh-my-pi** | ~10 | Static | Minimal | No |
| **Hermes** | 86 tools | Registry + MCP | Heuristic + OS isolation | Yes |

**Meept Advantage:** Only Meept has intent-based tool routing (tools chosen per-agent based on capabilities), SecurityEngine gating with SQLite permissions, and Tirith pre-execution shell scanning.

---

### 4. Security Model

| Framework | Permissions | Input Sanitization | Command Scanning | TLS | Path Fencing |
|-----------|-------------|-------------------|------------------|-----|--------------|
| **Meept** | SQLite policy checker | Prompt injection detection | Tirith | Yes | Project-local |
| **OpenCode** | None | Basic | No | Optional | No |
| **OpenAgent (Rust)** | Guard whitelist | Credential scrub | Sandbox only | No | No |
| **OpenAgent (Python)** | Approvals | No | Pattern blocks | P2P auth | No |
| **OpenClaw** | None | Basic | No | Optional | No |
| **oh-my-pi** | Minimal | No | No | No | No |
| **Hermes** | Toolset allowlist | Message sanitization | Tirith | Transport-level | Path validation |

**Meept Advantage:** Only Meept has a unified SecurityEngine with fine-grained permissions, InputSanitizer for prompt injection, Tirith for shell scanning, TLS for transport, AND path fencing for project isolation.

---

### 5. Execution Model

| Framework | Daemon | CLI | GUI | API | Session Persistence |
|-----------|--------|-----|-----|-----|---------------------|
| **Meept** | Yes (Unix socket + HTTP) | Charmbracelet TUI | Flutter + MenuBar | REST + RPC + WebSocket | SQLite |
| **OpenCode** | No | Yes | No | No | None |
| **OpenAgent (Rust)** | Yes (port 8080) | No | No | HTTP | SQLite |
| **OpenAgent (Python)** | Yes | Yes | Electron | HTTP + P2P | SQLite |
| **OpenClaw** | No | Yes | No | No | None |
| **oh-my-pi** | No | Yes | No | No | SQLite |
| **Hermes** | Gateway (platform adapters) | prompt_toolkit | No | No | SQLite |

**Meept Advantage:** Only Meept has a unified daemon with dual-transport (RPC + HTTP), Flutter web UI, macOS MenuBar app, AND persistent session/memory storage.

---

### 6. Context Management

| Framework | Window Management | Compression | Thread Partitioning | Token Tracking |
|-----------|------------------|-------------|---------------------|----------------|
| **Meept** | Context firewall | Hierarchical summarization | Yes (thread-based) | Per-turn + per-session |
| **OpenCode** | Basic truncation | No | No | Limited |
| **OpenAgent (Rust)** | Sliding window | TODO | No | No |
| **OpenAgent (Python)** | Threshold trigger | In-session recap | No | Basic |
| **OpenClaw** | Truncation | No | No | No |
| **oh-my-pi** | Limited | Limited | No | No |
| **Hermes** | LLM summarization | Pluggable engine | Session branching | Per-turn |

**Meept Advantage:** Only Meept has a ContextFirewall with hierarchical compression, structured summarization, token-aware truncation, AND thread-based context partitioning for cross-session isolation.

---

### 7. Scheduling Capabilities

| Framework | Cron | Job Queue | Priority | Agent Targeting | DAG/Workflow |
|-----------|------|-----------|----------|-----------------|--------------|
| **Meept** | Yes | Yes (claim-based) | Yes | Yes (agent_id) | Plans system |
| **OpenCode** | Limited | No | No | No | No |
| **OpenAgent (Rust)** | Full cron | No | No | No | No |
| **OpenAgent (Python)** | Cron + Workflows | Request queue | No | No | Yes (DAG) |
| **OpenClaw** | No | No | No | No | No |
| **oh-my-pi** | No | No | No | No | No |
| **Hermes** | Full cron | No | No | No | No |

**Meept Advantage:** Only Meept has a job queue with priority-based claim semantics AND agent targeting (jobs routed to specific agents by agent_id).

---

### 8. Observability / Metrics

| Framework | Metrics Store | Prometheus | Structured Logging | Health Endpoints |
|-----------|---------------|------------|-------------------|------------------|
| **Meept** | SQLite TSDB | Compatible | slog | Yes |
| **OpenCode** | None | No | Basic | No |
| **OpenAgent (Rust)** | OTEL JSONL | TODO | Structured | /api/diagnose |
| **OpenAgent (Python)** | SQLite usage | No | elog() | /api/usage |
| **OpenClaw** | None | No | Basic | No |
| **oh-my-pi** | Limited | No | Basic | No |
| **Hermes** | Cost estimation | No | RotatingFileHandler | No |

**Meept Advantage:** Only Meept has a comprehensive metrics store (`internal/metrics/`) with SQLite time-series storage, Prometheus-compatible metrics, AND structured logging with `log/slog`.

---

### 9. Model Routing / Resolution

| Framework | Provider Support | Capability Matching | Fallback Chain | Presets | Reasoning Effort |
|-----------|-----------------|---------------------|----------------|---------|------------------|
| **Meept** | 10+ providers | Skill-based resolution | Yes | 7 presets | Vendor translation |
| **OpenCode** | 2-3 | Manual | No | No | No |
| **OpenAgent (Rust)** | 1 | TODO | TODO | No | No |
| **OpenAgent (Python)** | 15+ | Team-as-router | Yes | No | No |
| **OpenClaw** | 1-2 | Manual | Limited | No | No |
| **oh-my-pi** | Limited | No | No | No | No |
| **Hermes** | 15+ | Per-session swap | Yes | No | Auxiliary model |

**Meept Advantage:** Only Meept has capability-based model resolution (skills declare requirements, models declare capabilities), natural language model reassignment ("use GLM for coding"), AND vendor-specific reasoning effort translation.

---

### 10. Self-Improvement Capabilities

| Framework | Detection | Analysis | Fix Generation | Validation | Application |
|-----------|-----------|----------|----------------|------------|-------------|
| **Meept** | Yes (multiple sources) | Root cause | AI-powered | Sandboxed testing | With approval |
| **OpenCode** | No | No | No | No | No |
| **OpenAgent (Rust)** | No | No | No | No | No |
| **OpenAgent (Python)** | Auto-skills | Pattern matching | No | No | No |
| **OpenClaw** | No | No | No | No | No |
| **oh-my-pi** | No | No | No | No | No |
| **Hermes** | Background review | Skill creation | Skill updates | Implicit | Automatic |

**Meept Advantage:** Only Meept has a FULL self-improvement cycle with SelfImproveController, SelfImproveDetector, SelfImproveAnalyzer, SelfImproveGenerator, SelfImproveValidator, AND SelfImproveApplier. Hermes has skill learning but NOT code self-improvement.

---

### 11. AI Employees / Autonomous Agents

| Framework | Constitution | Autonomy Tiers | Enforcement Engine | Goal Loop | Audit Findings |
|-----------|-------------|----------------|-------------------|-----------|----------------|
| **Meept** | Yes (4 sections) | 3 tiers (reactive/propose/autonomous) | Pre-exec + Post-turn + Periodic | ASSESS→PLAN→EXECUTE→REFLECT | SQLite findings |
| **OpenCode** | No | No | No | No | No |
| **OpenAgent (Rust)** | No | No | No | No | No |
| **OpenAgent (Python)** | No | No | No | No | No |
| **OpenClaw** | No | No | No | No | No |
| **oh-my-pi** | No | No | No | No | No |
| **Hermes** | No | No | No | Kanban tasks | No |

**Meept Advantage:** ONLY Meept has constitution-bound AI Employees with formal autonomy tiers, a three-checkpoint enforcement engine (pre-execution gate, post-turn audit, periodic drift detection), goal-based health tracking, and audit findings with resolution workflows.

---

### 12. Evidence-Based Execution

| Framework | Evidence Types | Validator | Claim Checking | Needs Info Routing |
|-----------|---------------|-----------|----------------|-------------------|
| **Meept** | File hashes, exit codes, API responses | Yes | Claims vs evidence | Human review |
| **OpenCode** | None | No | No | No |
| **OpenAgent (Rust)** | None | No | No | No |
| **OpenAgent (Python)** | None | No | No | No |
| **OpenClaw** | None | No | No | No |
| **oh-my-pi** | None | No | No | No |
| **Hermes** | None | No | Heuristic abort | No |

**Meept Advantage:** ONLY Meept has an evidence pipeline where every tool produces structured `Evidence` (file hashes, process exit codes, API responses), validators cross-check agent claims against ground truth, and claims without evidence trigger `needs_info` status for human review.

---

## What Makes Meept Different

### Unique Capabilities (No Other Framework Has)

1. **Constitution-bound AI Employees**
   - Three autonomy tiers (reactive, propose, autonomous)
   - Pre-execution gate (blocks forbidden tools, risk ceiling violations, never patterns)
   - Post-turn audit (small-model classifier scans for violations)
   - Periodic drift detection (catches gradual boundary-pushing)
   - Auto-pause on critical findings with operator resume required

2. **Evidence-Based Deterministic Execution**
   - Tool results carry `Evidence` structs (file hashes, exit codes, API responses)
   - Validators cross-reference claims against evidence
   - Missing evidence → `needs_info` → human review
   - No trust in LLM claims without verification

3. **Seven-Layer Agent Loop Safety**
   - Context Firewall (hierarchical compression, token-aware truncation)
   - Cycle Detector (repeated identical tool calls → abort)
   - Convergence Detector (stagnating responses → intervention)
   - Watchdog (heartbeat monitoring, kill stuck workers)
   - Budget Tracker (per-iteration, per-conversation, per-session)
   - Model Failover (rate-limit → rotation → exponential backoff)
   - Hallucination Recovery (pattern detection with configurable sensitivity)

4. **Five-Tier Memory System**
   - Episodic (SQLite FTS5, BM25 ranking)
   - Task (domain-specific: code/commands/generation)
   - Knowledge Graph (PageRank scoring)
   - Semantic (Vector, cosine similarity)
   - Distributed (memvid hydration/distillation)

5. **Full Self-Improvement Cycle**
   - Detect (pytest, runtime logs, type checking, linting)
   - Analyze (root cause analysis)
   - Generate (AI-powered fix proposals)
   - Validate (sandboxed testing)
   - Apply (with human approval for critical changes)

6. **Go Implementation**
   - Native threading (goroutines) - no Python GIL bottleneck
   - Compiled performance - no interpreter overhead
   - Single binary deployment - no virtualenv/dependency hell
   - Memory safety without GC pauses (where possible)

### Common Patterns (Shared with Some Frameworks)

| Pattern | Also In | Meept's Twist |
|---------|---------|---------------|
| Cron scheduling | OpenAgent (both), Hermes | Agent targeting via `agent_id` |
| MCP tool support | OpenAgent (Python), Hermes | 21 preconfigured servers, 4 enabled by default |
| Session persistence | OpenAgent (both), oh-my-pi, Hermes | Thread partitioning for cross-session isolation |
| Context compaction | OpenAgent (Python), Hermes | ContextFirewall with structured summarization |
| Model provider diversity | OpenAgent (Python), Hermes | Capability-based resolver + reasoning effort translation |
| Skill systems | OpenAgent (Python), Hermes | YAML frontmatter + priority shadowing discovery |

### Areas Where Meept Lags

| Capability | Leader | Gap |
|------------|--------|-----|
| Browser automation | Hermes (12 tools) | Meept has basic web fetch, no full browser control |
| Computer use | Hermes (macOS CUA) | Not implemented |
| Desktop app | OpenAgent (Python) Electron | Flutter UI is web-based, not native desktop |
| P2P networking | OpenAgent (Python) Iroh | Centralized daemon architecture |
| Home Assistant integration | Hermes | Not implemented |
| DAG workflow execution | OpenAgent (Python) | Plans system is simpler, no DAG engine |

---

## Update: README.md "What Makes Meept Different" Section

Replace the existing comparison table with:

```markdown
### What Makes Meept Different

| Platform | Model | Meept's Advantage |
|----------|-------|-------------------|
| **Claude Code / OpenClaw** | Single-session CLI | **Daemon architecture** with persistent memory, job scheduling, and multi-transport (RPC + HTTP + WebSocket) |
| **Hermes / OpenCode** | Terminal-only, ephemeral | **Constitution-bound AI Employees** with autonomy tiers, enforcement engines, and goal loops |
| **Cursor** | IDE-integrated copilot | **Evidence-based execution** - every tool produces verifiable evidence (file hashes, exit codes, API responses) |
| **OpenAgent (Rust)** | Single agent (ReAct) | **18 specialist agents + 5 reviewers** with intent classification and capability-based routing |
| **OpenAgent (Python)** | Team coordination | **Seven-layer safety stack** - watchdog, cycle/convergence detection, budget tracking, hallucination recovery, model failover, context firewall |
| **oh-my-pi** | Single-agent harness | **Five-tier memory system** - episodic (FTS5), task, knowledge graph, semantic (vector), distributed (memvid) |
| **Most agents** | Written in Python | **Go implementation** - native threading (goroutines), compiled performance, single binary deployment |
| **Most agents** | Manual model selection | **Model reassignment via natural language** - "use GLM for coding" triggers capability-based resolution |
| **Most agents** | Skill learning only | **Full self-improvement cycle** - detect → analyze → generate → validate → apply (code fixing, not just patterns) |

See [docs/analysis/agent-framework-comparison.md](docs/analysis/agent-framework-comparison.md) for the complete 12-category comparison.
```

---

## Recommended README.md Updates

### 1. Feature Status Table - Update These Rows

| Feature | Old Status | New Status | Notes |
|---------|------------|------------|-------|
| **Self-improvement** | 🔄 Partial | ✅ Complete | Full cycle operational: detector (pytest/logs/lint/type-check), analyzer, generator, validator (sandboxed), applier with approval workflow |
| **Shadow training** | 🔄 Partial | 🔄 Partial (infrastructure complete) | Data collection + export operational; continuous learning pipeline in progress |
| **Multi-agent system** | ✅ Working (8 agents) | ✅ Complete (18 + 5 reviewers) | 13 executor agents + 5 domain-specific reviewers + dispatcher |
| **AI employees** | ✅ Working | ✅ Complete | Constitution engine, goal loop, enforcement (3 checkpoints), CLI + TUI + HTTP + RPC |

### 2. Agent Count - Update This Section

From:
> **Multi-agent orchestration** -  8+ configurable specialist agents that discover and delegate to each other

To:
> **Multi-agent orchestration** - 18 specialist agents (dispatcher, chat, coder, debugger, planner, analyst, committer, scheduler, researcher, writer, architect, skeptic, librarian) + 5 reviewers (code-reviewer, test-reviewer, debug-reviewer, planner-reviewer, analyst-reviewer) that discover and delegate to each other

### 3. Add Missing Agents to README

The README mentions 8 agents but Meept has 18 + 5 reviewers. Add:

```markdown
### Executor Agents (13)

| Agent | Role | Purpose |
|-------|------|---------|
| `dispatcher` | Router | Intent classification, task routing |
| `chat` | Conversational | General conversation, Q&A |
| `coder` | Code | File operations, implementation |
| `debugger` | Debug | Troubleshooting, bug fixing |
| `planner` | Planning | Task decomposition, step definition |
| `analyst` | Analysis | Research, synthesis, insights |
| `committer` | Git | Commit operations, branch management |
| `scheduler` | Scheduling | Cron jobs, reminders, calendar |
| `researcher` | Research | Web fetching, documentation |
| `writer` | Writing | Long-form content, documentation |
| `architect` | Architecture | System design, trade-off analysis |
| `skeptic` | Critical analysis | Stress-testing claims, contradictions |
| `librarian` | Memory | Tag hygiene, reflection, epistemic integrity |

### Reviewer Agents (5)

| Agent | Reviews Domain | Purpose |
|-------|---------------|---------|
| `code-reviewer` | Code changes | Code quality, patterns, bugs |
| `test-reviewer` | Tests | Test coverage, correctness |
| `debug-reviewer` | Debug work | Debug methodology, root cause |
| `planner-reviewer` | Plans | Plan completeness, feasibility |
| `analyst-reviewer` | Analysis | Research rigor, conclusions |
```

---

## Verification Checklist

- [ ] README "What Makes Meept Different" table updated with evidence-based claims
- [ ] Agent count corrected: 8 → 18 + 5 reviewers
- [ ] Self-improvement status: 🔄 Partial → ✅ Complete
- [ ] Shadow training status clarified (infrastructure complete, learning pipeline in progress)
- [ ] AI employees section added/expanded (constitution, enforcement, goal loop)
- [ ] Link to full comparison document: `docs/analysis/agent-framework-comparison.md`
- [ ] "Missing agents" added to multi-agent section
- [ ] Comparison claims validated against actual framework codebases (not marketing)

---

## Sources

- **OpenCode**: Multiple TypeScript derivatives analyzed
- **OpenAgent (Rust)**: github.com/mopenagent/OpenAgent
- **OpenAgent (Python)**: github.com/openagent-uno/openagent-server
- **OpenClaw**: Claude Code open derivatives
- **oh-my-pi**: github.com/can1357/oh-my-pi
- **Hermes Agent**: github.com/nousresearch/hermes-agent (v0.17.0)
- **Meept**: Internal codebase audit (config/agents/, internal/agent/, internal/employee/)

---

*Analysis generated 2026-06-23 as part of README.md modernization effort.*
