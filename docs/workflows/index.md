# Workflows

Feature specifications describing how each Meept subsystem works, with configuration, examples, and edge cases.

## Core Features

| Feature | Description |
|---------|-------------|
| [Dynamic Tool Routing](tool-routing.md) | How tools are matched to agents and executed |
| [Multi-Agent Orchestration](agent-orchestration.md) | Task decomposition, delegation, and review |
| [Skill System](skills.md) | Skill discovery, loading, and execution |
| [Security Engine](security.md) | Input sanitization, taint tracking, shell scanning |
| [Adversarial Input Defense](adversarial-input-defense.md) | Defense-in-depth protection for web fetches, file reads, MCP tools |
| [Memory System](memory.md) | Storage, retrieval, consolidation, and search |
| [LLM Provider Management](llm-management.md) | Multi-provider support, failover, budget |
| [Models CLI](models-cli.md) | Interactive provider/model management |
| [Context Firewall](context-firewall.md) | Context pressure management and summarization |

## Advanced Features

| Feature | Description |
|---------|-------------|
| [Code Intelligence](code-intelligence.md) | AST parsing and LSP client tools |
| [Job Scheduling](job-scheduling.md) | Cron jobs, reminders, and task scheduling |
| [External Integrations](external-integrations.md) | Telegram, web API, and Google Calendar |
| [Collaborative Planning](collaborative-planning.md) | Review/approval workflow for agent work |
| [Deterministic Execution](deterministic-execution.md) | Evidence-based validation, concurrency control, retry logic |
| [Taint Tracking](taint-tracking.md) | Lattice-based information flow security |
| [Shadow Training](shadow-training.md) | Parallel teacher execution and training data export |
| [Q Agent](q-agent.md) | Meta-agent for session analysis and optimization design |

## Self-Improvement

| Feature | Description |
|---------|-------------|
| [Self-Improvement](self-improvement.md) | Automated issue detection and code fixing |
