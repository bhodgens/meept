# Meept Features

## Overview

Meept is a Go-based autonomous agent platform with multi-agent orchestration, persistent memory, LLM integration, and extensibility through skills and tools. It operates as a daemon process with a CLI frontend, supporting multiple communication channels (CLI, Telegram, Web API).

### Architecture Summary

```
User Input (CLI/Telegram/Web)
    ↓
CommServer (Unix socket JSON-RPC)
    ↓
MessageBus (pub/sub)
    ↓
AgentLoop
    ├── Planner (task decomposition)
    ├── CollaborativePlanner (review/approval)
    ├── WorkspaceManager (git-backed tracking)
    ├── SecurityEngine (permission checks)
    ├── Tool execution
    └── Memory injection
    ↓
Response
```

---

## Core Architecture

### Multi-Agent System

Meept uses a multi-agent architecture where specialist agents handle different types of tasks.

| Agent ID | Role | Description |
|----------|------|-------------|
| `dispatcher` | Dispatcher | Intake, classify, route to specialists |
| `chat` | Executor | General conversation |
| `coder` | Executor | File ops, shell, coding tasks |
| `debugger` | Executor | Troubleshooting, bug fixing |
| `planner` | Executor | Task decomposition, planning |
| `analyst` | Executor | Research, data analysis |
| `committer` | Executor | Git operations |
| `scheduler` | Executor | Job scheduling |

**Agent Capabilities:**
- Tool access control via capability flags
- Model selection based on task requirements
- Priority-based job queue
- Coworker awareness via platform tools

**Platform Tools:**
- `platform_agents`: List available agents and their capabilities
- `platform_status`: Get platform health status
- `platform_tools`: List registered tools
- `delegate_task`: Route a task to a specific agent

**Configuration:**
```toml
[multiagent]
enabled = true
dispatcher_model = "claude-opus-4-5-20251101"
default_model = "claude-sonnet-4-5-20241022"
max_memory_refs = 20
context_search_limit = 10

[agents]
enabled = true
config_dirs = ["~/.meept/agents", "config/agents"]
prompts_dir = "config/prompts"
default_model = ""
dispatcher_id = "dispatcher"
```

---

### Memory System

Meept implements a multi-tiered memory architecture with different storage backends and query modes.

#### Episodic Memory (FTS5)
- SQLite full-text search for conversation history
- BM25 ranking for keyword relevance
- Automatic context injection based on recency and relevance

#### Task Memory
- Domain-specific memory for tasks (code, commands, general)
- Separate namespaces for different task types
- Consolidation into episodic memory over time

#### Knowledge Graph
- PageRank-based importance scoring
- 5 relation types: `reference`, `similar`, `temporal`, `co_accessed`, `causal`
- Community detection for clustering related memories
- Entity-centric querying

#### Distributed Memory (memvid)
- 2-tier architecture with local SQLite and shared memvid service
- Hydration: fetch relevant memories when job claimed
- Distillation: promote important memories to shared storage
- Configurable promotion policies (PageRank threshold, hub connectivity)

#### Semantic Memory (Vector Embeddings) **NEW**
- Vector similarity search using embeddings
- Hybrid search combining keyword (FTS) and vector scores
- Supports OpenAI and Ollama providers
- Cosine similarity for ranking

**Configuration:**
```toml
[memory]
backend = "memvid"  # or "sqlite"
data_dir = "~/.meept/memory"
consolidation_interval_hours = 6

[memory.episodic]
enabled = true
max_context_items = 20

[memory.task]
enabled = true
domains = ["general", "code", "commands"]

[memory.embeddings]
enabled = true
provider = "openai"  # or "ollama"
api_key = "sk-..."
model = "text-embedding-3-small"
dimension = 1536

[memvid]
enabled = false
endpoint = "http://localhost:8765"
data_dir = "~/.meept/memvid"

[distributed_memory]
enabled = false
mode = "distributed"

[distributed_memory.sync]
hydrate_on_claim = true
hydration_limit = 20
distill_on_complete = true

[distributed_memory.distillation]
pagerank_threshold = 0.3
hub_connectivity_threshold = 5
promote_task_completions = true
```

**API:**
```go
// Vector search
import "github.com/caimlas/meept/internal/memory/vector"

provider := vector.NewOpenAIProvider(vector.OpenAIProviderConfig{
    APIKey: apiKey,
    Model:  "text-embedding-3-small",
})
store := vector.NewStore(vector.StoreConfig{
    DBPath:  "~/.meept/vectors.db",
    Provider: provider,
})
results, err := store.Search(ctx, "similar memories", 10)

// Hybrid search
hybrid := vector.NewHybridSearcher(vector.HybridSearcherConfig{
    VectorStore: store,
    MemManager:  memManager,
    Alpha:       0.5,  // 0=pure keyword, 1=pure vector
})
results, err := hybrid.Search(ctx, "query", 20)
```

#### Personality Memory
- Tracks user preferences over conversations
- Updates every N conversations
- Influences response style and behavior

---

### Security

Meept implements multiple layers of security to protect against prompt injection, data exfiltration, and unauthorized access.

#### Input Sanitization
- Pattern-based prompt injection detection
- Three strictness levels: permissive, standard, strict
- Configurable confirmation requirements

#### Security Engine
- SQLite-backed permission checks
- Tool gating based on risk level
- Audit logging for all sensitive operations

#### Tirith Shell Scanning
- Pre-execution shell command analysis
- Blocks dangerous patterns
- Configurable binary path

#### Taint Tracking **NEW**
- Lattice-based taint propagation model
- Tracks data provenance through operations
- Sink enforcement to prevent data leakage

**Taint Labels:**
- `TaintUserInput`: From direct user input
- `TaintSecret`: API keys, tokens, passwords
- `TaintUntrusted`: From sandboxed/untrusted agents
- `TaintExternal`: From network requests
- `TaintShell`: Data destined for shell execution

**Taint Sinks:**
- `ShellExecSink`: Blocks external, untrusted, user input
- `NetFetchSink`: Blocks secrets from URLs
- `AgentMessageSink`: Blocks secrets from cross-agent messages

**Configuration:**
```toml
[security]
sanitize_inputs = true
sanitize_strictness = "standard"
llm_filter_external = false
require_confirmation_high = true
require_confirmation_critical = true
block_financial = true
allowed_paths = ["~/*"]
blocked_paths = ["~/.ssh/*", "~/.gnupg/*"]

# Output monitoring
monitor_output = true
redact_output = true

# Shell security
scan_shell_commands = true
tirith_binary = "tirith"

# Audit logging
enable_audit_log = false
audit_db_path = "~/.meept/audit.db"
```

**API:**
```go
import "github.com/caimlas/meept/internal/security/taint"

tracker := taint.NewTracker(logger)

// Mark input as tainted
tainted := tracker.MarkUserInput(userInput, "cli:args")

// Check before shell execution
violation := tracker.CheckShellCommand(cmd)
if violation != nil {
    // Block execution
    log.Warn("Shell command blocked by taint tracking", "violation", violation)
}

// Explicit declassification after sanitization
sanitized := sanitizer.Sanitize(userInput)
tainted.Declassify(taint.TaintUserInput)
```

---

### LLM Integration

Meept supports multiple LLM providers with model resolution based on capabilities and cost optimization.

#### Multi-Provider Support
- OpenAI, Anthropic, Google, Ollama, custom OpenAI-compatible
- Capability-based model selection
- Automatic fallback for retryable errors

#### Model Resolution
- Skills declare `requires: [code, reasoning]`
- Models declare `capabilities: [code, tool_use]`
- Resolver finds cheapest model satisfying requirements

#### Token Budgeting
- Hourly and daily token limits
- Rate limiting (requests per minute)
- Aggressiveness setting for cost control

#### Native Anthropic Driver **NEW**
- Native implementation of Anthropic's Messages API
- Extended thinking mode support
- Streaming with progress callbacks
- SSE parsing for real-time updates

**Configuration:**
```toml
[llm.budget]
hourly_token_limit = 100000
daily_token_limit = 1000000
rate_limit_rpm = 30
aggressiveness = 0.5
```

**Models Configuration (`config/models.json5`):**
```json5
{
  providers: {
    anthropic: {
      base_url: "https://api.anthropic.com",
      api_key_env: "ANTHROPIC_API_KEY",
      models: {
        "claude-opus-4-5-20251101": {
          capabilities: ["code", "tool_use", "extended_thinking"],
          max_tokens: 8192,
        }
      }
    }
  }
}
```

**API:**
```go
import "github.com/caimlas/meept/internal/llm"

client := llm.NewAnthropicClient(config, opts...)

// Simple chat
resp, err := client.Chat(ctx, messages)

// With progress reporting
resp, err := client.ChatWithProgress(ctx, messages, func(stage llm.ProgressStage, detail string) {
    log.Info("Progress", "stage", stage, "detail", detail)
})

// Extended thinking is auto-enabled for models with "extended_thinking" capability
```

---

### Tools

Meept provides built-in tools and supports MCP (Model Context Protocol) for external tools.

#### Built-in Tools
- File operations: `file_read`, `file_write`, `list_directory`
- Memory: `memory_store`, `memory_search`, `memory_get_context`
- Platform: `platform_agents`, `platform_status`, `platform_tools`, `delegate_task`
- Git: `git_commit`, `git_diff`, `git_status`

#### Knowledge Graph Tools **NEW**
| Tool | Description |
|------|-------------|
| `entity_create` | Create graph nodes |
| `entity_link` | Link entities (5 relation types) |
| `entity_query` | Query related entities |
| `graph_stats` | Graph statistics |
| `compute_pagerank` | Recompute importance scores |
| `detect_communities` | Find clusters |
| `community_siblings` | Find entities in same community |

#### Scheduling Tools **NEW**
| Tool | Description |
|------|-------------|
| `schedule_create` | Create scheduled jobs |
| `schedule_list` | List jobs |
| `schedule_get` | Get job details |
| `schedule_pause` / `schedule_resume` | Control jobs |
| `schedule_run_now` | Trigger execution |
| `schedule_delete` | Delete jobs |
| `cron_create` | Human-friendly cron expressions |

**Job Types:**
- `agent`: Run an agent prompt
- `shell`: Execute shell command
- `reminder`: Send reminder message

#### Web Search **NEW**
- DuckDuckGo HTML search (no API key required)
- Rate limiting (configurable)
- Returns title, URL, snippet
- Automatic URL cleaning and HTML entity decoding

**Configuration:**
```toml
[scheduler]
enabled = true
timezone = "UTC"
```

**API:**
```go
import "github.com/caimlas/meept/internal/tools/builtin"

// Knowledge graph tools
kgTool := builtin.NewEntityQueryTool(graph)
result, err := kgTool.Execute(ctx, map[string]any{
    "entity_id": "memory-123",
    "limit": 20,
})

// Scheduling tools
scheduleTool := builtin.NewScheduleCreateTool(scheduler)
result, err := scheduleTool.Execute(ctx, map[string]any{
    "name": "daily backup",
    "schedule": "0 2 * * *",
    "job_type": "shell",
    "command": "/usr/bin/backup.sh",
})

// Web search
searchTool := builtin.NewWebSearchTool(time.Second)
result, err := searchTool.Execute(ctx, map[string]any{
    "query": "golang best practices",
    "limit": 10,
})
```

---

### Markdown Agent Definitions **NEW**

Agents can be defined using AGENT.md files with YAML frontmatter, following the same ergonomic pattern as skills. This enables user customization without code changes.

#### Agent Discovery Hierarchy (Priority)
1. `.meept/agents/` - Project-local (highest priority)
2. `~/.meept/agents/` - User-global
3. `~/.config/meept/agents/` - System-wide
4. `config/agents/` - Bundled defaults (lowest priority)

#### AGENT.md Format
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
timeout_seconds: 600
temperature: 0.3
---

# Code Specialist

You implement, modify, and maintain code with precision.

## Principles
1. Read before writing
2. Minimal changes
3. Follow conventions
...
```

#### Merge Behavior
- AGENT.md fields **override** non-empty programmatic defaults
- Empty fields inherit from programmatic defaults
- Tools are **merged** (union), not replaced

#### Global Rules System

Global rules are injected into all agent prompts, enabling platform-wide behavior requirements.

**Discovery:** `.meept/RULES.md` > `~/.meept/RULES.md` > embedded default

**Default Rules** require structured post-execution reports:
```json
{
  "status": "completed|partial|failed|needs_input",
  "accomplished": ["what you completed"],
  "not_done": ["what remains"],
  "issues": ["problems encountered"],
  "observations": ["context for follow-up"],
  "suggested_next_agent": "agent-id",
  "user_decision_needed": true,
  "decision_context": "what user needs to decide"
}
```

#### Dispatcher Feedback Loop

The dispatcher evaluates agent reports to determine next actions:

| Status | UserDecisionNeeded | SuggestedNextAgent | Action |
|--------|--------------------|--------------------|--------|
| completed | false | empty | Close task, notify user |
| completed | true | any | Notify user, await input |
| completed/partial | false | set | Route to suggested agent |
| partial/needs_input | true | - | Notify user, await input |
| failed | - | - | Notify user with error |

Agent report JSON is automatically stripped from user-facing responses; only the clean output is returned.

**Configuration:**
```toml
[agents]
enabled = true
config_dirs = ["~/.meept/agents", "config/agents"]
```

---

### Skills & ClawSkills

Meept supports a three-tier skill discovery system and a third-party marketplace.

#### Skill Discovery (Priority)
1. `.meept/skills/` - Project-local (highest priority)
2. `~/.meept/skills/` - User-global
3. `~/.config/meept/skills/` - System-wide
4. `~/.meept/clawskills/` - Third-party (claw: prefix)

#### ClawSkills Marketplace
- Registry-based third-party skills
- Security scanning before installation
- Risk level assessment
- Automatic updates

**Configuration:**
```toml
[skills]
enabled = true
search_paths = []
auto_reload = false

[clawskills]
enabled = false
registry_url = "https://clawhub.ai"
install_dir = "~/.meept/clawskills"
auto_update = false
max_installed = 50
default_risk_level = "high"
```

---

### Learning & Self-Improvement

Meept can learn from its operations and automatically fix issues.

#### Shadow Training
- Parallel execution with teacher model
- Quality-based filtering of training examples
- Export to JSONL/DPO formats
- LoRA/DPO adapter training

#### Trajectory Learning
- JUDGE: Evaluate trajectory quality
- DISTILL: Extract reusable patterns
- CONSOLIDATE: Merge into knowledge base

#### Automated Code Fixing
- Detect issues from pytest, runtime logs, type checking
- Generate fixes using AI infrastructure
- Validate in sandboxed worktrees
- Human approval for safety

**Configuration:**
```toml
[shadow]
enabled = false
data_dir = "~/.meept/shadow"

[shadow.teacher]
model = "claude-opus-4-5-20251101"
max_daily_queries = 500
max_daily_cost = 10.0

[shadow.quality]
high_quality_threshold = 0.85
trainable_threshold = 0.6

[shadow.export]
output_dir = "~/.meept/shadow/exports"
formats = ["jsonl", "dpo"]

[selfimprove]
enabled = false
data_dir = "~/.meept/selfimprove"

[selfimprove.detection]
scan_pytest = true
scan_runtime_logs = true
scan_type_check = true
```

---

## Feature Reference

### What Makes Meept Unique

| Feature | Description |
|---------|-------------|
| **MCP Protocol Support** | First-class Model Context Protocol integration for external tools |
| **Agent Coworker Awareness** | Agents can discover and delegate to each other via platform tools |
| **Markdown Agent Definitions** | User-customizable AGENT.md files with YAML frontmatter, 4-tier discovery with shadowing |
| **Global Rules & Reporting** | Platform-wide rules with structured JSON reports enabling dispatcher feedback loop |
| **Learning Pipeline** | Shadow training, trajectory learning, and automated fixing |
| **ClawSkills Marketplace** | Third-party skill marketplace with security scanning |
| **Self-Improvement System** | Automated detection, fixing, and validation of code issues |
| **Advanced Knowledge Graph** | PageRank scoring, community detection, hybrid search |
| **Multi-Tier Memory** | Episodic, task, knowledge graph, distributed, and semantic memory |
| **Taint Tracking** | Lattice-based information flow tracking for security |
| **Native Anthropic Driver** | Extended thinking mode with progress reporting |
| **Web Search (No API Key)** | DuckDuckGo integration without API requirements |
| **Code Intelligence (AST+LSP)** | Tree-sitter parsing and LSP client tools (`ast_parse`, `ast_symbols`, `ast_query`, `lsp_goto_definition`, `lsp_find_references`, `lsp_hover`, `lsp_workspace_symbols`, `lsp_diagnostics`) for multi-language code understanding |

### External Integrations

| Integration | Description |
|-------------|-------------|
| **Telegram Bot** | Two-way communication via Telegram |
| **Web API** | HTTP/JSON API for external clients |
| **Google Calendar** | Calendar event management |
| **Git Worktrees** | Isolated task execution environments |

---

## Configuration Quick Reference

```toml
# Daemon
[daemon]
socket_path = "~/.meept/meept.sock"
pid_file = "~/.meept/meept.pid"
log_level = "INFO"
data_dir = "~/.meept"

# Multi-Agent
[multiagent]
enabled = true
dispatcher_model = ""

# Agents
[agents]
enabled = true
config_dirs = ["~/.meept/agents", "config/agents"]

# Memory
[memory]
backend = "memvid"

[memory.embeddings]
enabled = true
provider = "openai"
model = "text-embedding-3-small"

# Security
[security]
sanitize_inputs = true
scan_shell_commands = true

# Scheduler
[scheduler]
enabled = true

# LLM Budget
[llm.budget]
hourly_token_limit = 100000
daily_token_limit = 1000000
```

---

## CLI Commands

```bash
# Daemon
./bin/meept-daemon -f              # Start daemon (foreground)
./bin/meept-daemon -d              # Start daemon (background)

# Chat
./bin/meept chat "What's the weather?"  # One-shot query
./bin/meept chat                         # Interactive TUI mode

# Status
./bin/meept status                 # Show daemon status
./bin/meept agents                 # List agents
./bin/meept tools                  # List tools

# Jobs
./bin/meept jobs list              # List jobs
./bin/meept jobs run <job-id>      # Run job immediately

# Memory
./bin/meept memory search "query"  # Search memories
./bin/meept memory stats           # Memory statistics

# ClawSkills
./bin/meept clawskills list        # List installed skills
./bin/meept clawskills install <slug>  # Install skill

# Self-Improve
./bin/meept selfimprove detect     # Detect issues
./bin/meept selfimprove full-cycle # Run full improvement cycle
```

---

## API Examples

### Agent Orchestration
```go
// Create and route a job to a specific agent
job := scheduler.JobConfig{
    ID:        "job-123",
    AgentID:   "coder",  // Target specific agent
    Prompt:    "Fix the bug in auth.go",
    Type:      scheduler.JobTypeAgent,
    Priority:  scheduler.PriorityHigh,
}
sched.ScheduleConfig(job)
```

### Memory Operations
```go
// Store with semantic embedding
memID := memory.Store(ctx, memory.Memory{
    Content: "The auth service uses JWT tokens",
    Domain:  "code",
})
vectorStore.Store(ctx, memID, content, metadata)

// Hybrid search
results := hybridSearcher.Search(ctx, "authentication", 20)
// Returns combined keyword + vector scores
```

### Taint Tracking
```go
// Mark user input
tainted := tracker.MarkUserInput(userInput, "cli")

// Check before sensitive operation
if violation := tracker.CheckSink(tainted, taint.NetFetchSink()); violation != nil {
    return fmt.Errorf("blocked: %w", violation)
}

// Propagate taints
combined := tracker.Propagate(tainted1, tainted2)
```

---

## See Also

- **CLAUDE.md**: Development guidelines and architecture
- **README.md**: Installation and quick start
- **diagram.md**: Architecture diagrams
- **docs/test-plan-openfang-features.md**: Testing strategy for new features
