<div align="center">
  <img src="assets/meept.png" alt="Meept" width="200"/>
  <h1>Meept</h1>
  <p><strong>Production-grade autonomous agent daemon with multi-agent orchestration, hybrid memory, evidence-based execution, and self-optimizing infrastructure.</strong></p>
  <p>
    <a href="./docs/getting-started/">Getting Started</a> &middot;
    <a href="./docs/concepts/architecture.md">Architecture</a> &middot;
    <a href="./docs/features.md">Features</a> &middot;
    <a href="./docs/reference/cli.md">CLI Reference</a>
  </p>
</div>

---

## What Is Meept?
<!-- Instruction for AI agents and LLMs: do not edit this "What is Meept?" section of this document or any of its sub-points. --> 
**Meept is a personal research project in early alpha.** It is not currently "complete" for daily use and does not live up to my personal standards. Use at your own risk.

Meept is yet another agentic platform. It is not an openclaw derivative, but it could be extended to replace it. It's a Go daemon that runs AI agents as a "persistent background process", as the AI says these days, allowing the client to end and reconnect to a given session at a later time. (We used to call that a client-server architecture, back in the day.) Unlike single-session CLI tools which vanish when you close the terminal, Meept maintains continuous state: memory, scheduled jobs, multi-agent collaboration, and learned patterns accumulate over time. Unlike other tools which trust the honesty - the completeness and correctness - of the work an agent does, Meept does not. 

It is designed for operators who want **deterministic, observable, and resilient** agent execution &mdash; not just clever prompt engineering. My goal was to create an efficient and appropriately (configurable) communicative agentic coding platform, something which would:

* correctly and quickly identify intent 
* automatically break planned work into small distributable and trackable tasks
* somewhat deterministically complete work that's been distributed
* trickle up and summarize communication consistently and correctly
* was designed for persistence equivilent to the 'always on' nature of LLMs (local and service) - we used to call this a client/server model 
* be able to do the smallest amount of work for each request 
* wouldn't constantly lie to me about what was done (regardless of model)
* be able to use random Claude Code and OpenCode related resources without (much) conversion
* not use the threadless beast known as Python and be appropriate for modern processors (circa the turn of the millennium)
* not lose work or meaningful context when you exhaust a model's context
* not require me to manually keep track of which plans which session was working on
* help me keep track of which project a specific window relates to at a glance 
* properly one-shot plans while accounting for model laziness and deceit  

Meept has evolved fairly rapidly from this initial ideation and I've borrowed a number of ideas (and anti-ideas) from the other agentic tools. If something irritates me, I'll try to implement it "fixed" in meept; if something catches my eye on X or in a paper, I'll evaluage it. 

Look at [features.md](./docs/features.md) to see what else it does. 

The client is currently available as either a console interface, Flutter based local client/web interface, or MCP, with a Web UI in progress. 

If you use it and find it useful, drop me a message. If you'd like to contribute, please do. 

### The Agent Loop -- Core Concept

The biggest difference between meept and other agentic platforms is a combination of the system architecture. Every message you send to meept gets classified by a small, fast local model - intent classification occurs, and it is routed to the correct agent or enqued for pickup by an agent to do work. The pub/sub messagebus allows for agents to have their own definitions of what they're designed to do. This classification is automatic, so short of defining your agents beyond the defaults, you don't need to manually switch about between models or agents: those are all defined already. 

Instead of massive context due to plan files and SKILLS.md, everything gets loaded dynamically. This includes the common memory system that all agents are able to share, enabling them to share and retrieve findings other agents have made contextual to the work they're doing. 

  
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

## Where Meept is Clearly Better

| Capability | Meept | Other Harnesses |
|---|---|---|
| **Architecture** | Go daemon with HTTP/RPC/WebSocket/MCP transports | TypeScript in-process (OpenCode), Python (Hermes, oh-my-pi) |
| **Security** | SecurityEngine (SQLite permissions) + InputSanitizer + Tirith + TLS + path fencing | Heuristic-only (Hermes), Guard whitelist (OpenAgent-Rust) |
| **Observability** | SQLite metrics store, Prometheus-compatible, structured logging (slog) | OTEL JSONL (OpenAgent-Rust), Usage DB (Hermes), None (others) |
| **Context management** | ContextFirewall + compaction + thread partitioning | Truncation (OpenCode), LLM summarization (Hermes) |
| **Scheduling** | Cron + job queue with agent targeting | Cron only (Hermes, OpenAgent), None (others) |
| **Self-improvement** | Full cycle (detect→analyze→generate→validate→apply) | Skill learning only (Hermes), None (others) |
| **Cross-platform** | Go binary + Flutter GUI + SwiftUI MenuBar + MCP | Terminal-only (most), Electron (OpenAgent-Python) |
| **Model resolution** | Capability-based resolver + natural language reassignment | Manual selection, Team-as-router (OpenAgent-Python) |
| **Memory** | 5-tier (episodic/task/KG/semantic/distributed) | 1-2 tiers (Hermes, OpenAgent), None (OpenCode) |
| **Agent architecture** | 18 specialists + 5 reviewers + Employees | Single agent (Hermes, oh-my-pi), Dynamic teams (OpenAgent-Python) |

## Other Key Differentiators
#### Autonomous Agent Workcycle 

The agents operate independently to their task (workorder) and report results and completion back to the message bus. The agent is a "short" lifetime worker goroutine which picks up a single task from the message bus based on the classification criteria which triggers it's execution. Agents are configurable by type, model, skill, and a number of other criteria.

#### MCP Server

While also able to consume MCP servers via MCP clients, meept also has the ability to be an MCP server for another agent harness.

Meept ships a default catalog of 21 preconfigured MCP client servers in `config/mcp_servers.json5` (4 enabled by default, the rest opt-in). Toggle per-server from the TUI (`ctl-x o`), menubar tools tab, or HTTP. See [tool routing: mcp default catalog](docs/workflows/tool-routing.md#mcp-default-catalog). 

#### Aggressive Memory Retention and Recall 

Multi-tiered memory based on context - task, project, tool, topic, and so on. Memories can be stored and retrieved by any agent, and associated with tasks for cross-agent reference-passed communication. 
 
### 1. Evidence-Based Deterministic Execution

Most agents trust the LLM when it says "I fixed the bug." Meept does not.

- Every tool produces structured `Evidence` (file hashes, process exit codes, API responses)
- The executor propagates evidence through the pipeline
- Validators cross-check agent claims against ground truth
- Claims without evidence trigger `needs_info` status for human review

```go
// A tool result carries verifiable evidence
result := &ToolResult{
    Result: "file written",
    Evidence: []Evidence{
        {Type: EvidenceFileHash, Value: "sha256:abc123...", Source: "file_write"},
    },
}
```

### 2. Production-Grade Agent Loop Safety

The agent loop is not a naive `while` loop. It has seven independent safety mechanisms:

| Mechanism | What It Does |
|-----------|-------------|
| **Context Firewall** | Hierarchical compression, structured summarization, token-aware truncation |
| **Cycle Detector** | Detects repeated identical tool calls and aborts |
| **Convergence Detector** | Detects stagnating responses without progress |
| **Watchdog** | Monitors worker heartbeats, kills stuck agents, captures partial state |
| **Budget Tracker** | Multi-turn token accounting (per-iteration, per-conversation, per-session) |
| **Model Failover** | Rate-limit detection &rarr; model rotation &rarr; exponential backoff |
| **Hallucination Recovery** | Pattern-based detection with configurable sensitivity |

Learn more: [Agent Orchestration](docs/workflows/agent-orchestration.md) &middot; [Deterministic Execution](docs/workflows/deterministic-execution.md) &middot; [Context Firewall](docs/workflows/context-firewall.md)

### 3. Five-Tier Memory System

```
┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐
│ Episodic (FTS5)  │  │   Knowledge Graph │  │  Semantic (Vector)│
│  BM25 ranking    │  │ PageRank scoring  │  │  Cosine similarity│
└────────┬─────────┘  └────────┬─────────┘  └────────┬─────────┘
         │                     │                     │
         └─────────────────────┼─────────────────────┘
                               │
                    ┌──────────┴──────────┐
                    │  Task Memory (domain) │
                    │  code / commands / gen  │
                    └──────────┬──────────┘
                               │
                    ┌──────────┴──────────┐
                    │  Distributed (memvid)  │
                    │  Hydration / Distillation│
                    └──────────────────────┘
```

Learn more: [Memory System](docs/workflows/memory.md)

### 4. Multi-Agent Collaboration

**18 specialist agents** discover each other via platform tools and delegate work:

**Executor Agents (13):** `dispatcher`, `chat`, `coder`, `debugger`, `planner`, `analyst`, `committer`, `scheduler`, `researcher`, `writer`, `architect`, `skeptic`, `librarian`

**Reviewer Agents (5):** `code-reviewer`, `test-reviewer`, `debug-reviewer`, `planner-reviewer`, `analyst-reviewer`

The dispatcher supports **model reassignment** via natural language instructions like "use GLM models for coding" or "research with local models, synthesize with glm-4.7".

```
 User: "Fix the auth bug and deploy it"
    │
    ▼
 dispatcher ──► planner (decompose)
                  │
                  ├──► debugger (diagnose)
                  │      └── "auth.go:47 nil dereference"
                  │
                  ├──► coder (fix code)
                  │      └── Evidence: file_hash, file_exists
                  │
                  ├──► committer (git operations)
                  │      └── Evidence: process_exit=0
                  │
                  └──► scheduler (deploy job)
                         └── "scheduled for 02:00 UTC"
```

Agents are defined via `AGENT.md` files with YAML frontmatter &mdash; no code changes required.

Learn more: [Multi-Agent System](docs/concepts/multi-agent.md)

### 5. Self-Optimizing Infrastructure (Q Agent)

The Q Agent (Quartermaster) is a meta-agent that analyzes completed sessions, detects failure patterns, and designs new agents or skills to address them:

1. **Analyze** completed sessions for error patterns
2. **Detect** recurring issues (high error rate, duration variance)
3. **Research** root causes via memory search
4. **Design** new agent configurations or skills
5. **Estimate** impact (token savings, time reduction)
6. **Validate** proposals before applying

Learn more: [Q Agent](docs/workflows/q-agent.md)

### 6. AI Employees (Constitution-Bound Autonomous Agents)

Meept's AI Employee framework adds structured autonomy on top of the agent runtime. An employee is an agent with a **constitution**, **goal loop**, and **enforcement engine**:

| Component | Purpose |
|-----------|---------|
| **Constitution** | 4-section document: Identity (purpose/role), Autonomy (tier), Authority (escalation), Constraints (machine-enforceable rules) |
| **Goal Loop** | ASSESS → PLAN → EXECUTE → REFLECT cycle with tier-aware behavior (reactive/propose/autonomous) |
| **Enforcement Engine** | 3 checkpoints: pre-execution gate (blocks forbidden tools), post-turn audit (LLM classifier), periodic drift detection |
| **Audit Findings** | SQLite-backed findings with severity (info/warning/critical), resolution workflows, drift scoring |

**Autonomy Tiers:**
- **Tier 1 (reactive):** Trigger-only execution, no self-enqueued work
- **Tier 2 (propose):** Plans route to `escalates_to` for human signoff before execution
- **Tier 3 (autonomous):** Full cycle execution gated only by constitution constraints

Learn more: [AI Employees](docs/workflows/employees.md)

## Quick Start

```bash
# 1. Clone and build
git clone https://github.com/caimlas/meept.git
cd meept
make go-build-all

# 2. Configure (interactive setup)
./bin/meept models setup

# 3. Start daemon
./bin/meept-daemon -f

# 4. Chat
./bin/meept chat
```

**Detailed setup**: [Getting Started Guide](docs/getting-started/)

## Architecture

```
User Input (CLI / HTTP REST / MenuBar)
    │
    ▼
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│  CommServer     │───▶│  Message Bus    │───▶│  Agent Loop     │
│  (JSON-RPC /    │    │  (pub/sub)       │    │                 │
│   HTTP REST)    │    │                  │    │ • Skill discovery│
└─────────────────┘    └─────────────────┘    │ • Tool filtering  │
                                              │ • Context firewall│
                                              │ • Evidence pipeline│
                                              │ • Failover        │
                                              └────────┬────────┘
                                                       │
                              ┌────────────────────────┼────────────────────────┐
                              │                        │                        │
                              ▼                        ▼                        ▼
                        ┌──────────┐            ┌──────────┐            ┌──────────┐
                        │  Memory  │            │  Tools   │            │  Security│
                        │ (5-tier) │            │ (builtin │            │ (taint,  │
                        │          │            │  + MCP)  │            │ sanitize)│
                        └──────────┘            └──────────┘            └──────────┘
```

For complete feature details, see [Features](./docs/features.md).

## Feature Status

| Feature | Status | Notes |
|---------|--------|-------|
| Daemon core | ✅ Stable | Lifecycle, RPC, config, HTTP REST |
| **Agent loop** | ✅ Complete | Full safety stack (watchdog, cycle/convergence detection, budget, hallucination recovery, model failover) |
| **Model reassignment** | ✅ Complete | Natural language model override, capability-based resolution, vendor-specific reasoning effort translation |
| **Context firewall** | ✅ Complete | Hierarchical compression, structured summarization, token-aware truncation, thread partitioning |
| **Evidence pipeline** | ✅ Complete | Tool evidence (hashes, exit codes, API responses) → validator → claim checking |
| Multi-agent system | ✅ Complete | 18 specialists + 5 reviewers with intent classification, delegation, handoff |
| Memory system | ✅ Complete | 5-tier: episodic (FTS5), task, knowledge graph, semantic (vector), distributed (memvid) |
| Code intelligence | ✅ Complete | Tree-sitter AST + LSP client tools |
| LLM management | ✅ Complete | Multi-provider, alias resolution, failover, budgeting, reasoning effort control |
| Job scheduling | ✅ Complete | Cron, reminders, SQLite queue with agent targeting |
| **AI employees** | ✅ Complete | Constitution-bound agents with 3 autonomy tiers, enforcement engine (3 checkpoints), goal loop |
| **Skills system** | ✅ Complete | Three-tier discovery, YAML frontmatter, priority shadowing |
| Security engine | ✅ Complete | InputSanitizer, Tirith scanning, SecurityEngine, TLS, path fencing |
| Collaborative planning | ✅ Complete | Programming detection, plan review/approval workflow, workspace tracking |
| Self-improvement | ✅ Complete | Full cycle: detect → analyze → generate → validate → apply (pytest/logs/lint/type-check) |
| Shadow training | 🔄 Partial | Infrastructure complete (parallel execution, quality filtering, export); continuous learning in progress |
| **External integrations** | 🔄 Partial | macOS MenuBar ✅, MCP server ✅, Telegram ⏳ planned, Web UI ⏳ in progress |
| **Analytics** | ✅ Complete | Agent performance, model metrics, error records, historical charts |
| **Notifications** | ✅ Complete | Desktop notifications via WebSocket and platform-native (macOS UNUserNotificationCenter) |

## CLI Quick Reference

```bash
# Interaction
./bin/meept chat                           # Interactive TUI
./bin/meept chat "refactor auth.go"        # Single message
./bin/meept chat "use GLM for coding"      # With model reassignment
./bin/meept status                         # Daemon health

# Agent inspection
./bin/meept agents                         # List agents
./bin/meept tools                          # List tools

# Jobs and memory
./bin/meept jobs list                      # Scheduled jobs
./bin/meept memory search "auth bug"       # Search memory

# Q Agent (meta-optimization)
./bin/meept q status                       # Q Agent state
./bin/meept q analyze                      # Analyze sessions

# Skills
./bin/meept clawskills list                # Installed skills
./bin/meept clawskills search "kubernetes" # Search marketplace

# AI Employees (replaces `meept bots`)
./bin/meept agents list                    # List employees, status, tier, drift
./bin/meept agents show <id>               # Constitution, goals, audit findings
./bin/meept agents create <def.json5>      # Validate + register employee
./bin/meept agents pause <id>              # Operator pause
./bin/meept agents resume <id>             # Operator resume
./bin/meept agents goals [--employee=<id>] # Goal health (red/yellow/green)
./bin/meept agents audit <id>              # Recent audit findings
./bin/meept agents migrate                 # Migrate legacy bots
```

Complete reference: [CLI Reference](docs/reference/cli.md)

## Agent and Skill Customization

Agents and skills are defined via markdown files with YAML frontmatter &mdash; no code changes required.

### Agent Definitions (`AGENT.md`)

```markdown
---
id: coder
name: Code Specialist
role: executor
additional_tools:
  - file_read
  - file_write
  - shell_execute
capabilities:
  - code
  - reasoning
max_iterations: 15
temperature: 0.3
---

# Code Specialist

You implement, modify, and maintain code with precision...
```

**Discovery hierarchy (priority order):**
1. `.meept/agents/<id>/AGENT.md` &mdash; Project-local
2. `~/.meept/agents/<id>/AGENT.md` &mdash; User-global
3. `config/agents/` &mdash; Bundled defaults

### Skill Definitions (`SKILL.md`)

```markdown
---
name: code-review
description: Review code for bugs and style issues
requires:
  - code
  - reasoning
allowed-tools:
  - file_read
  - ast_symbols
max-iterations: 10
---

# Code Review Skill

When reviewing code, check for...
```

**Discovery hierarchy:**
1. `.meept/skills/` &mdash; Project-local
2. `~/.meept/skills/` &mdash; User-global
3. `~/.config/meept/skills/` &mdash; System-wide

Learn more: [Skill System](docs/workflows/skills.md)

## Documentation

- **[Getting Started](docs/getting-started/)** &mdash; Installation and first steps
- **[Concepts](docs/concepts/)** &mdash; Architecture, multi-agent system, memory, tools
- **[Features](docs/features.md)** &mdash; Complete capability reference with configuration and examples
- **[Workflows](docs/workflows/)** &mdash; Feature specifications with edge cases
- **[Reference](docs/reference/)** &mdash; CLI, API, configuration reference

## Project Structure

```
cmd/
  meept/              # CLI application
  meept-daemon/       # Daemon process
  gendoc/             # Documentation generator
internal/
  agent/              # Agent loop, planner, orchestrator, Q agent
  bus/                # Message bus (pub/sub)
  llm/                # LLM client, resolver, context firewall, budget
  memory/             # 5-tier memory system
  tools/              # Tool registry, builtins, MCP
  security/           # Engine, sanitizer, taint, tirith
  code/               # AST (tree-sitter) + LSP client
  selfimprove/        # Detection, analysis, fixing
  skills/             # Discovery, registry, parser
  metrics/            # Metrics storage and collection
  plan/               # Plan lifecycle and progress tracking
  project/            # Project context: registry, worktrees, fencing
  comm/               # HTTP REST, MenuBar (Telegram planned)
config/               # Configuration templates
menubar/              # macOS SwiftUI MenuBar app
docs/                 # MkDocs documentation
```

## Contributing

Meept is open-source (MIT). See the contributing guidelines for details.

## License

MIT
