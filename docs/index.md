# Meept

**Self-executing autonomous agent daemon with multi-agent orchestration, hybrid memory, and skill-based task execution.**

Meept is a Go-based daemon that runs AI agents as background processes. It supports multi-agent collaboration, persistent memory, tool execution, and multiple frontends (TUI, Telegram, web). Agents can decompose complex tasks, route work to specialists, and maintain context across sessions.

## Why Meept?

| Feature | Description |
|---------|-------------|
| **Multi-Agent Orchestration** | 28 specialist agents + reviewers with automatic task routing |
| **Hybrid Memory** | Episodic FTS5, task, knowledge graph, semantic vector, memvid |
| **Skill System** | Three-tier skill discovery with capability-based model resolution |
| **Security Layers** | Input sanitization, taint tracking, shell scanning, audit logging |
| **Code Intelligence** | Tree-sitter AST parsing and LSP client tools |
| **Learning Pipeline** | Shadow training, trajectory learning, automated code fixing |
| **Native LLM Drivers** | OpenAI, Anthropic, Ollama with capability-based model resolution |

## Quick Start

```bash
git clone https://github.com/caimlas/meept.git
cd meept
make build
make setup
cp config/models.json5 ~/.meept/models.json5  # Add your API keys
./bin/meept-daemon -f  # Terminal 1
./bin/meept chat       # Terminal 2
```

See the [Getting Started](getting-started/index.md) guide for detailed installation instructions.

## What Makes Meept Different

Meept is a persistent Go daemon, not a single-session CLI and not an IDE copilot.

It combines:

- Daemon runtime with RPC, HTTP, WebSocket, and MCP
- 28 specialist agents plus reviewers, routed by an intent classifier
- Five-tier memory (episodic FTS5, task, knowledge graph, semantic, memvid)
- Evidence on every tool result (hashes, exit codes, API bodies)
- Constitution-bound AI employees with autonomy tiers

The full named-product chart is in the [root README](../README.md#what-makes-meept-different).

## Project Status

Status below matches the root README feature table.

### What Works

| Component | Status | Notes |
|-----------|--------|-------|
| **Daemon Core** | Stable | Lifecycle, config, RPC, HTTP REST |
| **Agent Loop** | Stable | Full safety stack (watchdog, cycle/convergence, budget, failover) |
| **Multi-Agent** | Stable | 28 agents (22 executor-role incl. dispatcher + chat, 6 reviewers) |
| **CLI/TUI** | Stable | Interactive chat, vim mode, markdown rendering |
| **LLM Client** | Stable | Multi-provider, retry, budget tracking, capability resolver |
| **Tools** | Stable | File ops, shell, web, memory, tasks, scheduling, MCP catalog |
| **Memory** | Stable | Five-tier: episodic, task, knowledge graph, semantic, memvid |
| **Job Queue** | Stable | SQLite-backed, agent routing, priorities |
| **Security** | Stable | Sanitization, Tirith, SecurityEngine, TLS, path fencing |
| **Code Intel** | Stable | AST parsing, LSP client tools |
| **Skills** | Stable | Three-tier discovery plus closed-loop evolution |
| **Self-Improve** | Stable | Detect → analyze → generate → validate → apply |
| **AI Employees** | Stable | Constitution, 3 autonomy tiers, enforcement, goal loop |
| **Clients** | Stable | TUI, Flutter GUI, macOS MenuBar, Telegram, MCP server |

### In Progress

| Component | Status | Notes |
|-----------|--------|-------|
| **Shadow Training** | Partial | Parallel teacher execution and export work; continuous learning is still open |

## Navigation

- **[Getting Started](getting-started/index.md)** — Install, configure, and run your first agent
- **[Concepts](concepts/index.md)** — Architecture, agents, memory, and tools explained
- **[Configuration](configuration/index.md)** — Full configuration reference with examples
- **[Workflows](workflows/index.md)** — Feature specifications and usage guides
- **[Reference](reference/index.md)** — CLI commands, API reference, and observability
