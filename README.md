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

The client is currently available as a console TUI, a Flutter desktop/web client, Telegram bot, macOS MenuBar, and MCP server.

If you use it and find it useful, drop me a message. If you'd like to contribute, please do. 

### The Agent Loop -- Core Concept

The biggest difference between meept and other agentic platforms is a combination of the system architecture. Every message you send to meept gets classified by a small, fast local model - intent classification occurs, and it is routed to the correct agent or enqued for pickup by an agent to do work. The pub/sub messagebus allows for agents to have their own definitions of what they're designed to do. This classification is automatic, so short of defining your agents beyond the defaults, you don't need to manually switch about between models or agents: those are all defined already. 

Instead of massive context due to plan files and SKILLS.md, everything gets loaded dynamically. This includes the common memory system that all agents are able to share, enabling them to share and retrieve findings other agents have made contextual to the work they're doing. 

  
### What Makes Meept Different

One chart. Rows are capabilities. Columns are named products. No empty cells.

Figures are from public docs and repos as of August 2026.

| Capability | Meept | Hermes | OpenCode | Claude Code | OpenClaw | Cursor | OpenAgent | oh-my-pi |
|---|---|---|---|---|---|---|---|---|
| **Runtime** | Go daemon: Unix RPC + HTTP + WebSocket + MCP | Python CLI + gateway daemon + Desktop | TypeScript server (HTTP/WS); TUI + desktop clients | CLI + IDE plugins + web; no local owner daemon | TypeScript always-on multi-channel daemon | IDE process + optional cloud agents | **Rust:** binary on :8080 + TCP services. **Python:** network server + P2P workspace | TypeScript + Rust natives; in-process TUI |
| **Deploy** | Single compiled Go binary | Python (uv) + Node extras | TypeScript (Bun/Node) | Anthropic TypeScript distribution | Node.js (~430k lines) | Proprietary VS Code fork | **Rust:** one control-plane binary. **Python:** pip package, 3.10+ | Bun + ~80k-line Rust core; native Windows, no WSL |
| **Clients** | TUI, Flutter desktop/web, macOS MenuBar, Telegram, MCP server | CLI/TUI, Desktop, 20+ gateways (Telegram, Discord, Slack, WhatsApp, Signal, Home Assistant, …) | TUI, SolidJS desktop, web, Slack; client/server | Terminal, VS Code, JetBrains, claude.ai/code | WhatsApp, Telegram, Slack, Discord, Signal, Gmail, companion apps | VS Code-fork IDE | **Rust:** Telegram, Discord, Slack, WhatsApp, CLI, IRC, MQTT. **Python:** CLI, Electron, shared workspace URL | TUI + `/collab` browser relay; no desktop IDE |
| **Agents** | 28 specialists (22 executor + 6 reviewers); LLM intent classifier routes work | One agent + isolated subagents; Desktop Bot Mode (`@mention` roster, per-bot memory) | Primary agent + Task subagents; plugin specialist packs | Sub-agents and agent teams (Opus) | Single personal assistant | Single Agent/Composer plus BugBot reviewer | **Rust:** one in-process ReAct loop (max 40 steps). **Python:** WorkerAgent teams / event network | Main agent + schema-validated `task` subagents + advisor model |
| **Memory** | Five tiers: episodic FTS5, task, knowledge graph, semantic vector, memvid | USER.md + MEMORY.md + SOUL.md; FTS5 session search; Honcho; optional mem0 | Session store; extra memory only via plugins | CLAUDE.md project memory; ~1M context window | Workspace files / JSONL transcripts | `.cursorrules` plus checkpoints | **Rust:** 40-message STM + LanceDB LTM. **Python:** SQLite + skills + Obsidian | Four backends (off / local / hindsight / mnemopi SQLite); off by default |
| **Context** | ContextFirewall: hierarchical compression, structured summary, thread partition | LLM summarization, `/compress`, session branching | Harness compaction; optional working-memory plugins | Auto-compact inside a large window | Session files; truncation | Rules file plus chat truncation | **Rust:** sliding window (compaction still open). **Python:** in-session recap | snapcompact plus rewind/checkpoint tools |
| **Models** | Capability resolver + natural-language reassignment ("use GLM for coding") + failover | Per-session `/model`; 200+ providers; fallback | 75+ providers; Zen gateway; fallback | Claude family (third-party keys optional) | Model-agnostic: Claude, GPT, Gemini, Ollama, vLLM | Multi-vendor + Composer; Auto mode | **Rust:** OpenAI-compatible + fallback chain. **Python:** 15+ providers; team-as-router | 60+ providers; role pins (smol / slow / plan / advisor) |
| **Security** | SecurityEngine (SQLite perms) + InputSanitizer + Tirith + TLS + project path fence | Toolset allowlist, message sanitizer, Tirith, Docker/SSH/Modal isolation | Permission allow/deny/ask; local HTTP has been unauthenticated by default | Permission prompts + sandbox (bypassable); SOC 2 | Sandbox off by default; 90+ advisories; plaintext keys | Opt-in sandbox; yolo toggle; SOC 2 on Enterprise | **Rust:** contact Guard whitelist + sandbox VM. **Python:** approvals + P2P auth | Stream-rule abort, hash-anchored edits, isolated worktrees; no OS sandbox default |
| **Evidence** | Every tool emits file hashes, exit codes, API bodies; validators check claims; missing evidence → `needs_info` | Heuristic abort; no evidence structs | Tool results as text; no claim checker | Trusts the model plus the permission UI | Trusts the model; no claim checker | Trusts the model; BugBot is a separate review pass | No evidence pipeline in either variant | Schema-validated subagent yield; hashline rejects stale patches — not claim-vs-evidence |
| **Loop safety** | Seven layers: firewall, cycle, convergence, watchdog, budget, failover, hallucination recovery | Iteration limits, heuristic abort, container isolation | Compaction + permission rules; no cycle/convergence detectors | Rate limits, permission loop, sandbox | Operator-managed; no loop watchdog | Credit caps; no loop watchdog | **Rust:** max 40 iterations + concurrency semaphore. **Python:** request queue | Stream rules + advisor model; no watchdog/budget/hallucination stack |
| **Scheduling** | Cron + claim-based job queue with priority and `agent_id` targeting | Full cron; deliver to any gateway; per-bot routines | Session-scoped server; no local job queue | GitHub Actions / Slack→PR / scheduled jobs; not a local queue | Heartbeats on an always-on daemon; no job queue | Cloud background agents; no local cron | **Rust:** SQLite cron/at/every (shell or agent). **Python:** cron + DAG workflows | No cron; live session + collab only |
| **Self-improve** | Detect → analyze → generate → validate → apply (code) plus skill evolution (usage, promote, version) | Skill create/refine from experience; no code-fix cycle | None built in | None | Unreviewed skill marketplace | None | Skills are static files | Autolearn lessons → `learned.md`; plugin skills; no code-fix cycle |
| **Observability** | SQLite metrics store, slog, health endpoints | Cost/usage DB, rotating logs | Server logs; optional share | Vendor usage dashboard | Local JSONL transcripts | IDE UI; audit logs on Enterprise | **Rust:** OTEL JSONL + `/health`. **Python:** SQLite usage + `/api/usage` | Local `omp-stats` dashboard; per-subagent cost in Agent Hub |
| **Employees** | Constitution (4 sections), 3 autonomy tiers, 3-checkpoint enforcement, goal loop, SQLite audit findings | SOUL.md persona; no tiers or enforcement engine | AGENTS.md / custom agents; no constitution runtime | CLAUDE.md + permission tiers; no employee runtime | SOUL.md-style persona; no enforcement engine | Rules and modes; no employee runtime | None | Advisor role + config persona; no autonomy tiers |

## Other Key Differentiators
#### Autonomous Agent Workcycle 

The agents operate independently to their task (workorder) and report results and completion back to the message bus. The agent is a "short" lifetime worker goroutine which picks up a single task from the message bus based on the classification criteria which triggers it's execution. Agents are configurable by type, model, skill, and a number of other criteria.

#### MCP Server

While also able to consume MCP servers via MCP clients, meept also has the ability to be an MCP server for another agent harness.

Meept ships a default catalog of 20 preconfigured MCP client servers in `config/mcp_servers.json5` (6 enabled by default, the rest opt-in). Toggle per-server from the interactive config editor (`meept config`, "mcp servers" section), the menubar tools tab, or HTTP. See [tool routing: mcp default catalog](docs/workflows/tool-routing.md#mcp-default-catalog).

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

**28 agents** discover each other via platform tools and delegate work:

**Executor Agents (22):** `dispatcher`, `chat`, `coder`, `debugger`, `planner`, `analyst`, `committer`, `scheduler`, `researcher`, `writer`, `architect`, `skeptic`, `librarian`, `image-gen`, `video-gen`, `image-id`, `cost-auditor`, `doc-keeper`, `explore`, `flaky-test-hunter`, `onboarding-tour`, `release-manager`

**Reviewer Agents (6):** `code-reviewer`, `test-reviewer`, `debug-reviewer`, `planner-reviewer`, `analyst-reviewer`, `verifier`

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

### 6. Skill Evolution (Closed-Loop)

Skills are not static. Meept tracks how effective each skill is in practice and evolves them automatically:

- **Usage tracking:** Every skill injection and outcome is recorded in SQLite (`~/.meept/skills.db`)
- **Evidence-based refinement:** An LLM-driven evolver refines existing skills based on usage data (every 6h by default)
- **Pattern promotion:** High-performing learned patterns (UseCount >= 5, Confidence >= 0.7) get promoted to skill files
- **Pruning:** Skills with effectiveness < 0.2 after 10+ injections are archived
- **Verifier gate:** 4-dimension rubric (grounded/preserves/specific/safe) — no change goes live without passing
- **Versioning:** Every write is snapshotted; restore to any prior version

```bash
./bin/meept skills stats debug-systematically   # Check effectiveness
./bin/meept skills evolve                       # Trigger cycle manually
./bin/meept skills history code-review          # See version history
./bin/meept skills restore code-review --version=2  # Roll back
```

Learn more: [Skill System](docs/workflows/skills.md)

### 7. AI Employees (Constitution-Bound Autonomous Agents)

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
# 2. Configure (copy templates, then edit or use the interactive config TUI)
cp config/models.json5 ~/.meept/models.json5   # add your API keys
./bin/meept config                              # interactive config editor

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
| Multi-agent system | ✅ Complete | 28 agents (22 executor-role incl. dispatcher + chat, 6 reviewers) with intent classification, delegation, handoff |
| Memory system | ✅ Complete | 5-tier: episodic (FTS5), task, knowledge graph, semantic (vector), distributed (memvid) |
| Code intelligence | ✅ Complete | Tree-sitter AST + LSP client tools |
| LLM management | ✅ Complete | Multi-provider, alias resolution, failover, budgeting, reasoning effort control |
| Job scheduling | ✅ Complete | Cron, reminders, SQLite queue with agent targeting |
| **AI employees** | ✅ Complete | Constitution-bound agents with 3 autonomy tiers, enforcement engine (3 checkpoints), goal loop |
| **Skills system** | ✅ Complete | Three-tier discovery, YAML frontmatter, priority shadowing, **closed-loop evolution** (usage tracking, evidence-based refinement, pattern promotion, versioning) |
| Security engine | ✅ Complete | InputSanitizer, Tirith scanning, SecurityEngine, TLS, path fencing |
| Collaborative planning | ✅ Complete | Programming detection, plan review/approval workflow, workspace tracking |
| Self-improvement | ✅ Complete | Full cycle: detect → analyze → generate → validate → apply + skill evolution (usage tracking, LLM-driven refinement, pattern promotion, versioning) |
| Shadow training | 🔄 Partial | Infrastructure complete (parallel execution, quality filtering, export); continuous learning in progress |
| **External integrations** | ✅ Complete | macOS MenuBar ✅, MCP server ✅, Telegram bot ✅, Flutter GUI (desktop + web) ✅ |
| **Speech interfaces** | ✅ Complete | STT (native + Parakeet) and TTS (voice management, playback) wired into chat |
| **Unified theming** | ✅ Complete | Shared color tokens (`theme/tokens.json5`) drive TUI and GUI; cyberpunk/midnight/solarized variants via `rendering.ui_theme` — see [theming](docs/configuration/theming.md) |
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
./bin/meept agents                         # List employees, status, tier, drift

# Jobs and memory
./bin/meept jobs                           # Scheduled jobs
./bin/meept memory "auth bug"              # Search memory

# Q Agent (meta-optimization)
./bin/meept q status                       # Q Agent state
./bin/meept q analyze                      # Analyze sessions

# Skills
./bin/meept skills list                    # Available skills
./bin/meept skills stats [name]            # Usage/effectiveness per skill
./bin/meept skills archive <name>          # Archive a skill
./bin/meept skills restore <name>          # Restore archived skill
./bin/meept skills restore <name> --version=N  # Restore specific version
./bin/meept skills history <name>          # Version history
./bin/meept skills evolve                  # Trigger evolver cycle
./bin/meept skills gaps                    # Show skill coverage gaps
./bin/meept skills run <name> <input>      # Execute a skill directly

### Research references

The skill knowledge system implements mechanisms from two papers:

- **WikiSkill** (arXiv:2608.27454) — persistent wiki + immutable traces feeding
  skill evolution; meept adopts the raw/wiki/skill layering in the evolver.
- **SKILL.state** (arXiv:2608.26263) — bounded-prompt state-mode skill
  execution; meept adopts opt-in state mode via skill frontmatter `state: true`.

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
- **[Models Configuration](docs/reference/models.md)** &mdash; Providers, aliases, capabilities, reasoning, prompt cache
- **[Feature Comparison](docs/feature-comparison-matrix.md)** &mdash; Meept vs. frontier agents (FrontierAgent, duckagent, atomic-agent, prime-agent, Hermes, OpenCode, oh-my-pi, Claude Code)

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
  comm/               # HTTP REST, Telegram bot, MenuBar support
  employee/           # AI employees: constitution, goal loop, enforcement
  stt/ tts/           # Speech-to-text and text-to-speech
  tui/                # Terminal UI (bubbletea) with runtime theming
config/               # Configuration templates (JSON5)
theme/                # Shared color tokens (tokens.json5) for TUI + GUI
ui/flutter_ui/        # Flutter GUI client (desktop + web)
menubar/              # macOS SwiftUI MenuBar app
docs/                 # MkDocs documentation
```

## Contributing

Meept is open-source (MIT). See the contributing guidelines for details.

## License

MIT
