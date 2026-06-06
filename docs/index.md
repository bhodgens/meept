# Meept

**Self-executing autonomous agent daemon with multi-agent orchestration, hybrid memory, and skill-based task execution.**

Meept is a Go-based daemon that runs AI agents as background processes. It supports multi-agent collaboration, persistent memory, tool execution, and multiple frontends (TUI, Telegram, web). Agents can decompose complex tasks, route work to specialists, and maintain context across sessions.

## Why Meept?

| Feature | Description |
|---------|-------------|
| **Multi-Agent Orchestration** | 8 specialist agents + reviewers with automatic task routing |
| **Hybrid Memory** | Episodic, task, personality, knowledge graph, and vector memory |
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

Unlike single-agent CLI tools (Claude Code, OpenCode) or chat-only interfaces, Meept is a **persistent daemon** that runs specialist agents as background processes. It combines:

- **Daemon architecture** for always-on availability and job scheduling
- **Multi-agent routing** so specialists handle what they're best at
- **Persistent memory** that survives restarts and accumulates knowledge
- **Extensible tool system** with MCP protocol support
- **Self-improvement** through shadow training and automated bug fixing

## Project Status

### What Works

| Component | Status | Notes |
|-----------|--------|-------|
| **Daemon Core** | Stable | Full lifecycle, config, RPC server |
| **Agent Loop** | Stable | Tool use, reasoning, iteration limits |
| **Multi-Agent** | Stable | 8 specialist agents, 5 reviewers, routing |
| **CLI/TUI** | Stable | Interactive chat, vim mode, markdown rendering |
| **LLM Client** | Stable | Multi-provider, retry, budget tracking |
| **Tools** | Stable | File ops, shell, web, memory, tasks, scheduling |
| **Memory** | Stable | Episodic, task, personality, knowledge graph |
| **Job Queue** | Stable | SQLite-backed, agent routing, priorities |
| **Security** | Stable | Sanitization, taint tracking, shell scanning |
| **Code Intel** | Stable | AST parsing, LSP client tools |
| **Shadow Training** | Stable | Parallel teacher execution, export |

### In Progress

| Component | Status | Notes |
|-----------|--------|-------|
| **Skills Execution** | Partial | Discovery works; execution not fully wired |
| **MCP Tools** | Partial | Protocol implemented; runtime integration ongoing |
| **Self-Improve** | Partial | Detection works; full cycle not implemented |
| **Telegram** | Stub | Bot scaffolding only |
| **Web Server** | Stub | Basic structure; many endpoints TODO |
| **Calendar** | Stub | File exists; no integration |

## Navigation

- **[Getting Started](getting-started/index.md)** — Install, configure, and run your first agent
- **[Concepts](concepts/index.md)** — Architecture, agents, memory, and tools explained
- **[Configuration](configuration/index.md)** — Full configuration reference with examples
- **[Workflows](workflows/index.md)** — Feature specifications and usage guides
- **[Reference](reference/index.md)** — CLI commands, API reference, and observability
