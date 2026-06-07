<div align="center">
  <img src="assets/meept.jpg" alt="Meept" width="200"/>
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

Look at [features.md](./features.md) to see what else it does. 

The client is currently available as either a console interface, Flutter based local client/web interface, or MCP, with a Web UI in progress. 

If you use it and find it useful, drop me a message. If you'd like to contribute, please do. 

### The Agent Loop -- Core Concept

The biggest difference between meept and other agentic platforms is a combination of the system architecture. Every message you send to meept gets classified by a small, fast local model - intent classification occurs, and it is routed to the correct agent or enqued for pickup by an agent to do work. The pub/sub messagebus allows for agents to have their own definitions of what they're designed to do. This classification is automatic, so short of defining your agents beyond the defaults, you don't need to manually switch about between models or agents: those are all defined already. 

Instead of massive context due to plan files and SKILLS.md, everything gets loaded dynamically. This includes the common memory system that all agents are able to share, enabling them to share and retrieve findings other agents have made contextual to the work they're doing. 

  
### What Makes Meept Different

| Platform | Model | Meept's Advantage |
|----------|-------|-------------------|
| **Hermes / OpenCode** | Terminal-only, ephemeral | **Daemon architecture** with persistent memory, job scheduling, and continuous learning |
| **Claude Code** | Single-session CLI | **Multi-agent orchestration** -  8+ configurable specialist agents that discover and delegate to each other |
| **Cursor** | IDE-integrated copilot | **Background operation** - runs independently, works across any editor or no editor at all |
| **General agents** | Trust LLM claims | **Evidence-based validation** - every claim is checked against verifiable tool output |
| **Most agents** | Token-heavy first response | **Classification routing** - utilizes a fast first-pass local classifer agent | 
| **Most agents** | written in Python | **Golang** - faster and smaller, with a proper thread (goroutine) model for modern CPUs |
| **Most agents** | Single agent or naive delegation | **Agentic pairs** - 4 collaboration modalities: spec-driven review, shared-context pair sessions, bus-channel debates, and inline review tools |

I've also borrowed  ideas implemented in agentic harnesses like the venerable [oh-my-pi](https://github.com/can1357/oh-my-pi) and [Hermes Agent](https://github.com/nousresearch/hermes-agent), as well as other projects, when I find a feature which I think would improve things. 

## Other Key Differentiators
#### Autonomous Agent Workcycle 

The agents operate independently to their task (workorder) and report results and completion back to the message bus. The agent is a "short" lifetime worker goroutine which picks up a single task from the message bus based on the classification criteria which triggers it's execution. Agents are configurable by type, model, skill, and a number of other criteria.

#### MCP Server

While also able to consume MCP servers via MCP clients, meept also has the ability to be an MCP server for another agent harness. 

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

Eight specialist agents (`dispatcher`, `chat`, `coder`, `debugger`, `planner`, `analyst`, `committer`, `scheduler`) discover each other via platform tools and delegate work. The dispatcher supports **model reassignment** via natural language instructions like "use GLM models for coding" or "research with local models, synthesize with glm-4.7".

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

For complete feature details, see [Features](docs/features.md).

## Feature Status

| Feature | Status | Notes |
|---------|--------|-------|
| Daemon core | ✅ Stable | Lifecycle, RPC, config, HTTP REST |
| **Agent loop** | ✅ Working | Full safety stack (watchdog, cycle/convergence detection, budget) |
| **Model reassignment** | ✅ Complete | Natural language model override ("use GLM for coding"), clarification dialogs, task/step-level overrides |
| **Context firewall** | ✅ Working | Hierarchical compression, structured summarization |
| **Evidence pipeline** | ✅ Working | Tool evidence &rarr; validator &rarr; claim checking |
| Multi-agent system | ✅ Working | 8 agents, routing, delegation via platform tools |
| Memory system | ✅ Working | 5-tier: episodic, task, knowledge graph, semantic, distributed |
| Code intelligence | ✅ Working | Tree-sitter AST + LSP client tools |
| LLM management | ✅ Working | Multi-provider, alias resolution, failover, budgeting |
| Job scheduling | ✅ Working | Cron, reminders, SQLite queue |
| **Persistent bots** | ✅ Working | Autonomous bots with cron/bus/webhook triggers, memory isolation, cost budgets |
| **Skills system** | ✅ Complete | Discovery, execution, CLI commands (`meept skills list/run/show`) |
| Security engine | ✅ Complete | Input sanitization, Tirith scanning, audit logging, security hooks for all tools |
| Collaborative planning | ✅ Complete | Programming task detection, plan review/approval workflow wired into chat handler |
| Self-improvement | 🔄 Partial | Detection works, full cycle in progress |
| Shadow training | 🔄 Partial | Infrastructure ready, data collection not active |
| **External integrations** | 🔄 Partial | macOS MenuBar working, Telegram planned, web UI in progress |

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

# Bots
./bin/meept bots list                      # List all bots
./bin/meept bots create bot-def.json        # Create a bot
./bin/meept bots pause <bot-id>            # Pause a bot
./bin/meept bots resume <bot-id>           # Resume a bot
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
  comm/               # HTTP REST, MenuBar (Telegram planned)
config/               # Configuration templates
menubar/              # macOS SwiftUI MenuBar app
docs/                 # MkDocs documentation
```

## Contributing

Meept is open-source (MIT). See the contributing guidelines for details.

## License

MIT
