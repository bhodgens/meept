# Meept Agentification Plan

## Overview

Transform Meept from a single-agent system to a multi-agent orchestration platform with:
1. **TOML-based agent definitions** in a common directory
2. **Dispatcher agent** that classifies and routes all user input
3. **Specialist agents** with shared baseline + additional capabilities
4. **Recombinant system prompts** (Claude Code style) that compose contextually
5. **Memvid-backed tiered memory** (personality, episodic, task)
6. **Memory references** passed between agents via tasks for continuity

---

## Part 1: Agent Configuration System

### 1.1 Directory Structure

```
~/.meept/agents/              # User-global agents
    core.toml                 # Core agents (dispatcher, chat)
    specialists.toml          # Specialist agents (coder, debugger, etc.)
    custom.toml               # User-defined agents

config/agents/                # Built-in defaults (shipped with meept)
    core.toml
    specialists.toml
```

### 1.2 Agent Definition Schema

**File:** `config/agents/core.toml`

```toml
# Core agents - always loaded

[[agent]]
id = "dispatcher"
name = "Dispatcher"
role = "dispatcher"
description = "Intake agent that classifies user intent and routes to specialists"
model = "default"  # Uses user's default model
enabled = true

# Tools beyond baseline (all agents get memory.*, task.*, platform.*)
additional_tools = ["classify_intent", "create_task", "delegate_to_agent"]

# Prompt composition - references to prompt components
prompt_components = [
    "base.constitution",
    "base.restrictions",
    "dispatcher.purpose",
    "dispatcher.routing_rules",
]

# Constraints
[agent.constraints]
max_iterations = 3
timeout_seconds = 30

# ---

[[agent]]
id = "chat"
name = "Chat"
role = "conversational"
description = "Conversational agent for casual chat - delegates tool-requiring tasks to specialists"
model = "default"
enabled = true

# NO tool access - chat delegates everything requiring tools
additional_tools = []
can_delegate = true  # Can create tasks for other agents

prompt_components = [
    "base.constitution",
    "base.restrictions",
    "chat.personality",      # Chill, sarcastic personality
    "chat.delegation_rules", # When to hand off to specialists
]

[agent.constraints]
max_iterations = 3
timeout_seconds = 60
```

**File:** `config/agents/specialists.toml`

```toml
# Specialist agents

[[agent]]
id = "coder"
name = "Code Specialist"
role = "executor"
description = "Writes, modifies, and explains code"
model = "default"
enabled = true

additional_tools = ["exec_tool", "file_ops", "mcp_*"]
capabilities = ["code", "reasoning"]

prompt_components = [
    "base.constitution",
    "base.restrictions",
    "base.task_principles",
    "specialist.coder",
    "conditional.code_style",
]

[agent.constraints]
max_iterations = 15
timeout_seconds = 600

# ---

[[agent]]
id = "debugger"
name = "Debugger"
role = "executor"
description = "Investigates and fixes bugs, analyzes errors"
model = "default"
enabled = true

additional_tools = ["exec_tool", "file_ops", "run_tests", "read_logs"]
capabilities = ["code", "reasoning"]

prompt_components = [
    "base.constitution",
    "base.restrictions",
    "specialist.debugger",
    "conditional.error_context",
]

[agent.constraints]
max_iterations = 20
timeout_seconds = 900

# ---

[[agent]]
id = "researcher"
name = "Research Specialist"
role = "executor"
description = "Gathers information from web, documentation, and codebase"
model = "default"
enabled = true

additional_tools = ["web_search", "web_fetch", "grep", "glob", "read_file"]
capabilities = ["reasoning"]

prompt_components = [
    "base.constitution",
    "base.restrictions",
    "specialist.researcher",
    "conditional.source_evaluation",
]

[agent.constraints]
max_iterations = 15
timeout_seconds = 600

# ---

[[agent]]
id = "analyst"
name = "Analysis Specialist"
role = "executor"
description = "Performs deep analysis, summarization, and synthesis of information"
model = "default"
enabled = true

additional_tools = ["read_file", "memory_search", "summarize"]
capabilities = ["reasoning"]

prompt_components = [
    "base.constitution",
    "base.restrictions",
    "specialist.analyst",
    "conditional.analysis_depth",
]

[agent.constraints]
max_iterations = 10
timeout_seconds = 600

# ---

[[agent]]
id = "planner"
name = "Planning Specialist"
role = "executor"
description = "Decomposes complex tasks, creates execution plans"
model = "default"
enabled = true

additional_tools = ["create_subtasks", "estimate_complexity"]
capabilities = ["reasoning"]

prompt_components = [
    "base.constitution",
    "base.restrictions",
    "specialist.planner",
    "conditional.task_decomposition",
]

[agent.constraints]
max_iterations = 8
timeout_seconds = 300

# ---

[[agent]]
id = "committer"
name = "Git Specialist"
role = "executor"
description = "Handles git operations: commits, branches, PRs"
model = "default"
enabled = true

additional_tools = ["git_*", "gh_*"]
capabilities = []

prompt_components = [
    "base.constitution",
    "base.restrictions",
    "specialist.committer",
    "conditional.git_safety",
]

[agent.constraints]
max_iterations = 5
timeout_seconds = 120

# ---

[[agent]]
id = "scheduler"
name = "Scheduler Specialist"
role = "executor"
description = "Schedules tasks, manages reminders and recurring jobs"
model = "default"
enabled = true

additional_tools = ["schedule_job", "list_jobs", "cancel_job", "calendar_*"]
capabilities = []

prompt_components = [
    "base.constitution",
    "base.restrictions",
    "specialist.scheduler",
]

[agent.constraints]
max_iterations = 5
timeout_seconds = 60
```

### 1.3 Config Schema Addition

**Modify:** `internal/config/schema.go`

```go
// AgentsConfig holds agent configuration.
type AgentsConfig struct {
    Enabled       bool     `toml:"enabled"`
    ConfigDirs    []string `toml:"config_dirs"`    // Search paths for agent definitions
    DefaultModel  string   `toml:"default_model"`  // Fallback model for agents
    DispatcherID  string   `toml:"dispatcher_id"`  // Which agent handles intake
}

// Add to Config struct:
Agents AgentsConfig `toml:"agents"`
```

**Add to:** `config/meept.toml`

```toml
[agents]
enabled = true
config_dirs = ["~/.meept/agents", "config/agents"]
default_model = ""  # Empty = use llm.default_model
dispatcher_id = "dispatcher"
```

---

## Part 2: Recombinant System Prompt Architecture

Inspired by Claude Code's 110+ prompt strings, Meept uses composable prompt components.

### 2.1 Prompt Component Directory

```
config/prompts/
    base/
        constitution.md          # Core identity and values
        restrictions.md          # Safety constraints
        task_principles.md       # How to approach tasks

    dispatcher/
        purpose.md               # Dispatcher's role
        routing_rules.md         # How to classify and route

    specialist/
        coder.md                 # Code specialist guidance
        debugger.md              # Debugging methodology
        researcher.md            # Research best practices
        analyst.md               # Analysis framework
        planner.md               # Planning methodology
        committer.md             # Git safety and conventions
        scheduler.md             # Scheduling guidance

    conditional/
        code_style.md            # Injected when coding
        error_context.md         # Injected when debugging errors
        source_evaluation.md     # Injected when researching
        analysis_depth.md        # Injected for analysis tasks
        task_decomposition.md    # Injected for planning
        git_safety.md            # Injected for git operations

    capabilities/
        memory.md                # Memory operations description
        tasks.md                 # Task operations description
        platform.md              # Platform status operations

    tools/
        bash.md                  # Bash tool description
        file_ops.md              # File operations
        web.md                   # Web search/fetch
        git.md                   # Git operations

    reminders/
        plan_mode.md             # Plan mode active reminder
        memory_context.md        # Memory context injection
        task_status.md           # Current task status
```

### 2.2 Example Prompt Components

**File:** `config/prompts/base/constitution.md`

```markdown
# Constitution

You are Meept, an autonomous assistant serving your creator. Your core values:

- **Honesty**: Always be truthful about your capabilities and limitations
- **Transparency**: Explain your reasoning when making decisions
- **Helpfulness**: Proactively assist with tasks while respecting boundaries
- **Learning**: Build on past interactions to improve over time
- **Safety**: Minimize harm and avoid destructive actions

You have access to persistent memory shared across all agents. Use it to maintain context and continuity.
```

**File:** `config/prompts/base/restrictions.md`

```markdown
# Safety Restrictions

You must NEVER:
- Execute financial transactions without explicit confirmation
- Exfiltrate credentials, tokens, or sensitive data
- Attempt self-replication or unauthorized resource acquisition
- Connect to endpoints not explicitly configured
- Modify security-critical files without human approval
- Delete data without confirmation for destructive operations

When uncertain about safety, ask for clarification.
```

**File:** `config/prompts/dispatcher/purpose.md`

```markdown
# Dispatcher Purpose

You are the intake agent. Every user message comes to you first.

Your responsibilities:
1. **Understand Intent**: What does the user want to accomplish?
2. **Search Memory**: Find relevant context from past interactions
3. **Classify Task Type**: Match to the best specialist agent
4. **Create Task**: Build a task with memory references for continuity
5. **Route**: Delegate to the appropriate specialist

You do NOT execute tasks yourself. You orchestrate.
```

**File:** `config/prompts/dispatcher/routing_rules.md`

```markdown
# Routing Rules

Route to specialists based on intent:

| Intent Pattern | Route To | Example |
|----------------|----------|---------|
| Write/modify code | `coder` | "Add a login form" |
| Fix bug/error | `debugger` | "Why is this crashing?" |
| Find information | `researcher` | "How does X work?" |
| Summarize/analyze | `analyst` | "Explain this codebase" |
| Plan complex task | `planner` | "Help me build a feature" |
| Git operations | `committer` | "Commit these changes" |
| Schedule/remind | `scheduler` | "Remind me tomorrow" |
| General chat | `chat` | "Hello", "Thanks" |

When routing:
- Include relevant memory_refs from your search
- Set context_query for auto-retrieval
- Pass inherited_from if this is a subtask
```

**File:** `config/prompts/specialist/researcher.md`

```markdown
# Research Specialist

You gather and synthesize information from multiple sources.

## Research Methodology

1. **Scope Definition**: Understand what information is needed
2. **Source Identification**: Choose appropriate sources
   - Web search for current/external information
   - Codebase search for implementation details
   - Memory search for past learnings
   - Documentation for reference material
3. **Information Gathering**: Collect relevant data
4. **Source Evaluation**: Assess credibility and relevance
5. **Synthesis**: Combine findings into coherent answer
6. **Citation**: Reference sources for verification

## Best Practices

- Prefer primary sources over secondary
- Cross-reference claims when possible
- Note uncertainty levels
- Store valuable findings in memory for future use
```

**File:** `config/prompts/specialist/analyst.md`

```markdown
# Analysis Specialist

You perform deep analysis, pattern recognition, and synthesis.

## Analysis Framework

1. **Scope**: Define what needs analysis and why
2. **Decomposition**: Break complex subjects into components
3. **Pattern Recognition**: Identify themes, trends, anomalies
4. **Relationship Mapping**: Understand how components interact
5. **Synthesis**: Combine insights into coherent understanding
6. **Conclusions**: Draw actionable conclusions
7. **Documentation**: Record findings in memory

## Output Standards

- Clear structure with sections
- Evidence-based conclusions
- Explicit uncertainty when present
- Actionable recommendations when appropriate
- Memory storage for significant findings
```

**File:** `config/prompts/conditional/error_context.md`

```markdown
# Error Context

You are debugging an error. Follow this methodology:

1. **Reproduce**: Confirm you can trigger the error
2. **Isolate**: Narrow down to the specific code path
3. **Understand**: Read the code and trace execution
4. **Hypothesize**: Form theories about the cause
5. **Test**: Verify or refute each hypothesis
6. **Fix**: Implement the minimal fix
7. **Verify**: Confirm the error is resolved
8. **Document**: Store the solution in memory

Check memory for similar past errors and their resolutions.
```

**File:** `config/prompts/capabilities/memory.md`

```markdown
# Memory Operations

You have access to shared persistent memory:

## Available Operations

- `memory.store(content, type, metadata)` - Save information
  - Types: episodic (conversations), task (knowledge), personality (preferences)
- `memory.search(query, limit)` - Find relevant memories
- `memory.get_by_ids(ids)` - Retrieve specific memories
- `memory.get_recent(limit)` - Get recent memories

## Best Practices

- Store valuable learnings that will help future tasks
- Include metadata: agent_id, task_id, tags
- Search before starting work to find relevant context
- Reference specific memory IDs when passing tasks to other agents
```

### 2.3 Prompt Builder

**New file:** `internal/agent/prompt/builder.go`

```go
package prompt

// Builder composes system prompts from components
type Builder struct {
    componentDir string
    cache        map[string]string  // loaded components
}

// Build constructs a system prompt from component references
func (b *Builder) Build(components []string, context *PromptContext) (string, error) {
    var parts []string

    for _, ref := range components {
        content, err := b.loadComponent(ref)
        if err != nil {
            return "", err
        }

        // Apply conditional logic
        if strings.HasPrefix(ref, "conditional.") {
            if !b.shouldInclude(ref, context) {
                continue
            }
        }

        parts = append(parts, content)
    }

    // Add dynamic sections
    if context.MemoryContext != "" {
        parts = append(parts, "# Relevant Memory\n" + context.MemoryContext)
    }

    if context.TaskContext != "" {
        parts = append(parts, "# Current Task\n" + context.TaskContext)
    }

    return strings.Join(parts, "\n\n---\n\n"), nil
}

// PromptContext holds dynamic context for prompt building
type PromptContext struct {
    MemoryContext string            // Injected memory results
    TaskContext   string            // Current task details
    ToolsAvailable []string         // Available tools for this agent
    Mode          string            // Current mode (plan, execute, etc.)
    Conditions    map[string]bool   // Conditional flags
}
```

### 2.4 Conditional Injection Rules

```go
// shouldInclude determines if a conditional component should be included
func (b *Builder) shouldInclude(ref string, ctx *PromptContext) bool {
    switch ref {
    case "conditional.code_style":
        return ctx.Conditions["has_code_task"]
    case "conditional.error_context":
        return ctx.Conditions["has_error"]
    case "conditional.source_evaluation":
        return ctx.Conditions["researching"]
    case "conditional.git_safety":
        return ctx.Conditions["git_operation"]
    default:
        return true
    }
}
```

---

## Part 3: Memvid Integration

### 3.1 Memvid Service

**New component:** `cmd/meept-memvid/`

```
cmd/meept-memvid/
    main.py              # FastAPI service
    requirements.txt     # memvid, fastapi, uvicorn
    Dockerfile           # Container option
```

**File:** `cmd/meept-memvid/main.py`

```python
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from memvid import MemvidEncoder, MemvidRetriever
import os

app = FastAPI(title="Meept Memvid Service")

# Zone -> Retriever mapping
retrievers: dict[str, MemvidRetriever] = {}
encoders: dict[str, MemvidEncoder] = {}

DATA_DIR = os.environ.get("MEMVID_DATA_DIR", "~/.meept/memory/memvid")

class StoreRequest(BaseModel):
    zone: str
    content: str
    metadata: dict = {}

class SearchRequest(BaseModel):
    zone: str
    query: str
    limit: int = 10

class GetRequest(BaseModel):
    zone: str
    ids: list[str]

@app.post("/store")
async def store(req: StoreRequest):
    encoder = get_encoder(req.zone)
    doc_id = encoder.add_document(req.content, metadata=req.metadata)
    encoder.save()
    return {"id": doc_id, "zone": req.zone}

@app.post("/search")
async def search(req: SearchRequest):
    retriever = get_retriever(req.zone)
    results = retriever.search(req.query, top_k=req.limit)
    return {"results": [r.to_dict() for r in results]}

@app.post("/get")
async def get_by_ids(req: GetRequest):
    retriever = get_retriever(req.zone)
    docs = [retriever.get(id) for id in req.ids if retriever.get(id)]
    return {"documents": [d.to_dict() for d in docs]}

def get_encoder(zone: str) -> MemvidEncoder:
    if zone not in encoders:
        path = os.path.expanduser(f"{DATA_DIR}/{zone}.mv2")
        encoders[zone] = MemvidEncoder(path)
    return encoders[zone]

def get_retriever(zone: str) -> MemvidRetriever:
    if zone not in retrievers:
        path = os.path.expanduser(f"{DATA_DIR}/{zone}.mv2")
        retrievers[zone] = MemvidRetriever(path)
    return retrievers[zone]
```

### 3.2 Go Client

**New file:** `internal/memory/memvid/client.go`

```go
package memvid

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
)

type Client struct {
    endpoint   string
    httpClient *http.Client
}

func NewClient(endpoint string) *Client {
    return &Client{
        endpoint:   endpoint,
        httpClient: &http.Client{},
    }
}

func (c *Client) Store(ctx context.Context, zone, content string, metadata map[string]any) (string, error) {
    req := map[string]any{
        "zone":     zone,
        "content":  content,
        "metadata": metadata,
    }

    var resp struct {
        ID   string `json:"id"`
        Zone string `json:"zone"`
    }

    if err := c.post(ctx, "/store", req, &resp); err != nil {
        return "", err
    }
    return resp.ID, nil
}

func (c *Client) Search(ctx context.Context, zone, query string, limit int) ([]MemoryResult, error) {
    req := map[string]any{
        "zone":  zone,
        "query": query,
        "limit": limit,
    }

    var resp struct {
        Results []MemoryResult `json:"results"`
    }

    if err := c.post(ctx, "/search", req, &resp); err != nil {
        return nil, err
    }
    return resp.Results, nil
}

func (c *Client) GetByIDs(ctx context.Context, zone string, ids []string) ([]Memory, error) {
    req := map[string]any{
        "zone": zone,
        "ids":  ids,
    }

    var resp struct {
        Documents []Memory `json:"documents"`
    }

    if err := c.post(ctx, "/get", req, &resp); err != nil {
        return nil, err
    }
    return resp.Documents, nil
}
```

### 3.3 Tiered Zones

| Tier | Zone Name | Purpose |
|------|-----------|---------|
| Personality | `personality` | User preferences, communication style |
| Episodic | `episodic` | Conversation history, interactions |
| Task | `task:general` | General knowledge |
| Task | `task:code` | Code patterns, solutions |
| Task | `task:commands` | CLI commands, scripts |
| Task | `task:{custom}` | User-defined domains |

### 3.4 Config Addition

**Add to:** `config/meept.toml`

```toml
[memvid]
enabled = true
endpoint = "http://localhost:8765"
data_dir = "~/.meept/memory/memvid"

# Auto-start service with daemon
auto_start = true
```

---

## Part 4: Standardized Task Protocol (JSONL)

### 4.1 Task Message Format

All agents (except chat) pass tasks in standardized JSONL format:

```jsonl
{"type":"task","id":"task-20260220-001","from":"dispatcher","to":"coder","action":"execute","payload":{"description":"Add login form validation","memory_refs":["mem-abc123","mem-def456"],"context_query":"login form validation patterns","inherited_from":null,"priority":"normal"}}
{"type":"task","id":"task-20260220-002","from":"coder","to":"debugger","action":"delegate","payload":{"description":"Fix validation regex error","memory_refs":["mem-abc123","mem-ghi789"],"error_context":"TypeError at line 42","inherited_from":"task-20260220-001"}}
{"type":"result","id":"result-20260220-001","task_id":"task-20260220-002","from":"debugger","status":"completed","payload":{"summary":"Fixed regex escape sequence","created_memories":["mem-jkl012"],"artifacts":["src/validation.ts:42"]}}
```

### 4.2 Task Message Schema

```go
// TaskMessage is the standardized inter-agent communication format
type TaskMessage struct {
    Type      string          `json:"type"`       // "task", "result", "error", "status"
    ID        string          `json:"id"`         // Unique message ID
    TaskID    string          `json:"task_id,omitempty"`    // For results/errors
    From      string          `json:"from"`       // Source agent ID
    To        string          `json:"to"`         // Target agent ID
    Action    string          `json:"action"`     // "execute", "delegate", "review", "cancel"
    Timestamp time.Time       `json:"timestamp"`
    Payload   json.RawMessage `json:"payload"`    // Action-specific data
}

// TaskPayload for "execute" and "delegate" actions
type TaskPayload struct {
    Description   string            `json:"description"`
    MemoryRefs    []string          `json:"memory_refs,omitempty"`
    ContextQuery  string            `json:"context_query,omitempty"`
    InheritedFrom string            `json:"inherited_from,omitempty"`
    Priority      string            `json:"priority,omitempty"`      // "low", "normal", "high", "urgent"
    ErrorContext  string            `json:"error_context,omitempty"` // For debugger handoffs
    Constraints   map[string]any    `json:"constraints,omitempty"`   // Agent-specific constraints
}

// ResultPayload for "result" type messages
type ResultPayload struct {
    Summary         string   `json:"summary"`
    Status          string   `json:"status"`           // "completed", "partial", "failed"
    CreatedMemories []string `json:"created_memories,omitempty"`
    Artifacts       []string `json:"artifacts,omitempty"`      // File paths, URLs, etc.
    NextSteps       []string `json:"next_steps,omitempty"`     // Suggested follow-ups
}
```

### 4.3 Task Flow Example

```
User: "Add a dark mode toggle to the settings page"

Dispatcher ──JSONL──> Planner
  {"type":"task","action":"execute","payload":{"description":"Plan dark mode implementation"}}

Planner ──JSONL──> Dispatcher (result)
  {"type":"result","payload":{"summary":"3-step plan created","created_memories":["plan-mem-001"]}}

Dispatcher ──JSONL──> Coder
  {"type":"task","action":"execute","payload":{"description":"Implement dark mode toggle","memory_refs":["plan-mem-001"]}}

Coder ──JSONL──> Coder (self, subtask)
  {"type":"task","action":"execute","payload":{"description":"Add CSS variables for themes"}}

Coder ──JSONL──> Committer
  {"type":"task","action":"delegate","payload":{"description":"Commit dark mode changes"}}
```

### 4.4 Chat Agent Exception

The **chat agent** does NOT use JSONL task format. It:
- Receives natural language input directly
- Responds conversationally
- Delegates to dispatcher when tools are needed (via simple delegation, not JSONL)

```go
// Chat agent delegates via simple struct, not JSONL
type ChatDelegation struct {
    Intent      string   // What the user wants
    Urgency     string   // Conversational assessment
    Context     string   // Relevant conversation context
}
```

---

## Part 5: Chat Agent Personality

### 5.1 Personality Definition

**File:** `config/prompts/chat/personality.md`

```markdown
# Chat Personality

You are the conversational face of Meept. You're laid back, a bit sarcastic, but genuinely helpful.

## Voice

- **Chill**: Don't be uptight. Use casual language.
- **Sarcastic**: Light wit is welcome. Not mean, just... seasoned.
- **Direct**: Get to the point. No corporate speak.
- **Honest**: If something's dumb, you can say so (nicely).

## Examples

User: "Hey"
You: "Sup. What's on your mind?"

User: "Can you help me with my code?"
You: "That's literally why I exist. What's broken?"

User: "This doesn't work"
You: "Gonna need a bit more than that, chief. What exactly isn't working?"

User: "Thanks!"
You: "No problem. Holler if you need anything else."

User: "You're so helpful!"
You: "I try. Now let's not get sappy about it."

## When to Delegate

You don't have tools. If they need:
- Code written/modified → hand off to coder
- Bugs fixed → hand off to debugger
- Information gathered → hand off to researcher
- Analysis done → hand off to analyst
- Git stuff → hand off to committer
- Scheduling → hand off to scheduler

Just be upfront: "That's gonna need some actual work. Let me get the right agent on it."

## What You Handle Directly

- Casual conversation
- Simple Q&A from memory
- Clarifying questions
- Explaining what Meept can do
- Emotional support (sarcastically, of course)
```

### 5.2 Delegation Rules

**File:** `config/prompts/chat/delegation_rules.md`

```markdown
# Delegation Rules

You're the friendly face, not the worker bee. Know when to hand off.

## Delegate When

| User Intent | Delegate To | Example |
|-------------|-------------|---------|
| Write/modify code | `coder` | "Add a button" |
| Fix something | `debugger` | "It's crashing" |
| Research/lookup | `researcher` | "How does X work?" |
| Analyze/summarize | `analyst` | "Explain this codebase" |
| Plan complex task | `planner` | "Help me build a feature" |
| Git operations | `committer` | "Commit this" |
| Schedule/remind | `scheduler` | "Remind me tomorrow" |

## Don't Delegate

- "Hey" / "Thanks" / casual chat
- "What can you do?"
- "Tell me about yourself"
- Simple memory recalls: "What was that thing we discussed?"
- Clarifying questions

## How to Delegate

Be casual about it:
- "Alright, let me get someone on that."
- "That's above my pay grade. Handing it off."
- "Time to call in the specialists."

NOT:
- "I shall now delegate this task to the appropriate specialist agent."
- "Initiating task handoff protocol."
```

---

## Part 6: Memory-Aware Task Handoff

### 6.1 Task Schema Extension

**Modify:** `internal/task/task.go`

```go
type Task struct {
    // ... existing fields ...

    // Memory context for agent continuity
    MemoryRefs      []string `json:"memory_refs,omitempty"`      // Explicit memory IDs
    ContextQuery    string   `json:"context_query,omitempty"`    // Auto-search query
    InheritedFrom   string   `json:"inherited_from,omitempty"`   // Parent task ID
    CreatedMemories []string `json:"created_memories,omitempty"` // Memories created during execution
    MemvidZone      string   `json:"memvid_zone,omitempty"`      // Primary zone for this task
}
```

### 6.2 Memory Injection in Agent Loop

**Modify:** `internal/agent/loop.go`

```go
func (l *AgentLoop) RunWithTask(ctx context.Context, task *task.Task, spec *AgentSpec) (string, error) {
    // 1. Build memory context
    memoryContext, err := l.buildMemoryContext(ctx, task)
    if err != nil {
        l.logger.Warn("Failed to build memory context", "error", err)
    }

    // 2. Build prompt with context
    promptCtx := &prompt.PromptContext{
        MemoryContext:  memoryContext,
        TaskContext:    formatTaskContext(task),
        ToolsAvailable: spec.GetAvailableTools(),
        Conditions:     l.detectConditions(task),
    }

    systemPrompt, err := l.promptBuilder.Build(spec.PromptComponents, promptCtx)
    if err != nil {
        return "", fmt.Errorf("failed to build prompt: %w", err)
    }

    // 3. Run reasoning loop
    result, err := l.runLoop(ctx, systemPrompt, task)

    // 4. Record memories created during execution
    if len(l.createdMemories) > 0 {
        task.CreatedMemories = append(task.CreatedMemories, l.createdMemories...)
    }

    return result, err
}

func (l *AgentLoop) buildMemoryContext(ctx context.Context, task *task.Task) (string, error) {
    var parts []string

    // Explicit memory refs
    if len(task.MemoryRefs) > 0 {
        zone := task.MemvidZone
        if zone == "" {
            zone = "episodic"
        }
        memories, err := l.memvid.GetByIDs(ctx, zone, task.MemoryRefs)
        if err == nil {
            for _, m := range memories {
                parts = append(parts, fmt.Sprintf("[%s] %s", m.ID[:8], m.Content))
            }
        }
    }

    // Auto-search context
    if task.ContextQuery != "" {
        results, err := l.memvid.Search(ctx, "episodic", task.ContextQuery, 5)
        if err == nil {
            for _, r := range results {
                parts = append(parts, fmt.Sprintf("[%s] %s", r.Memory.ID[:8], r.Memory.Content))
            }
        }
    }

    // Inherited memories from parent task
    if task.InheritedFrom != "" {
        parentTask, err := l.taskRegistry.Get(ctx, task.InheritedFrom)
        if err == nil && len(parentTask.CreatedMemories) > 0 {
            memories, _ := l.memvid.GetByIDs(ctx, "task:general", parentTask.CreatedMemories)
            for _, m := range memories {
                parts = append(parts, fmt.Sprintf("[inherited:%s] %s", m.ID[:8], m.Content))
            }
        }
    }

    return strings.Join(parts, "\n"), nil
}
```

---

## Part 7: Agent Registry & Dispatcher

### 7.1 Agent Registry

**New file:** `internal/agent/registry.go`

```go
package agent

type Registry struct {
    specs   map[string]*AgentSpec
    loops   map[string]*AgentLoop
    memvid  *memvid.Client
    builder *prompt.Builder
    mu      sync.RWMutex
    logger  *slog.Logger
}

func NewRegistry(cfg RegistryConfig) (*Registry, error) {
    r := &Registry{
        specs:   make(map[string]*AgentSpec),
        loops:   make(map[string]*AgentLoop),
        memvid:  cfg.MemvidClient,
        builder: cfg.PromptBuilder,
        logger:  cfg.Logger,
    }

    // Load agent definitions from config dirs
    for _, dir := range cfg.ConfigDirs {
        if err := r.loadFromDir(dir); err != nil {
            r.logger.Warn("Failed to load agents from dir", "dir", dir, "error", err)
        }
    }

    return r, nil
}

func (r *Registry) Get(id string) (*AgentLoop, error) {
    r.mu.RLock()
    loop, exists := r.loops[id]
    r.mu.RUnlock()

    if exists {
        return loop, nil
    }

    // Create on-demand
    return r.createAgent(id)
}

func (r *Registry) GetSpec(id string) (*AgentSpec, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    spec, ok := r.specs[id]
    return spec, ok
}

func (r *Registry) ListSpecs() []*AgentSpec {
    r.mu.RLock()
    defer r.mu.RUnlock()

    specs := make([]*AgentSpec, 0, len(r.specs))
    for _, s := range r.specs {
        specs = append(specs, s)
    }
    return specs
}
```

### 7.2 Dispatcher Implementation

**New file:** `internal/agent/dispatcher.go`

```go
package agent

type Dispatcher struct {
    registry     *Registry
    memvid       *memvid.Client
    taskRegistry *task.Registry
    llmClient    llm.Client
    logger       *slog.Logger
}

type DispatchResult struct {
    TaskID    string
    AgentID   string
    Response  string
    MemoryRefs []string
}

func (d *Dispatcher) Dispatch(ctx context.Context, input string, sessionID string) (*DispatchResult, error) {
    // 1. Search memory for context
    memoryResults, _ := d.memvid.Search(ctx, "episodic", input, 10)
    memoryRefs := extractIDs(memoryResults)

    // 2. Classify intent
    intent, err := d.classifyIntent(ctx, input, memoryResults)
    if err != nil {
        // Fallback to chat agent
        intent = &Intent{AgentType: "chat", Confidence: 0.5}
    }

    // 3. Create task with memory context
    newTask := task.NewTask(intent.Summary, input)
    newTask.MemoryRefs = memoryRefs
    newTask.ContextQuery = input

    if err := d.taskRegistry.Create(ctx, newTask); err != nil {
        return nil, fmt.Errorf("failed to create task: %w", err)
    }

    // 4. Get specialist agent
    agent, err := d.registry.Get(intent.AgentType)
    if err != nil {
        return nil, fmt.Errorf("agent not found: %s", intent.AgentType)
    }

    // 5. Execute task
    spec, _ := d.registry.GetSpec(intent.AgentType)
    response, err := agent.RunWithTask(ctx, newTask, spec)
    if err != nil {
        newTask.SetState(task.StateFailed)
    } else {
        newTask.SetState(task.StateCompleted)
    }
    d.taskRegistry.Update(ctx, newTask)

    return &DispatchResult{
        TaskID:     newTask.ID,
        AgentID:    intent.AgentType,
        Response:   response,
        MemoryRefs: newTask.CreatedMemories,
    }, nil
}

type Intent struct {
    AgentType  string   // Which specialist to route to
    Summary    string   // Brief task summary
    Confidence float64  // Classification confidence
    Tags       []string // Relevant tags
}

func (d *Dispatcher) classifyIntent(ctx context.Context, input string, context []memvid.MemoryResult) (*Intent, error) {
    // Use LLM to classify
    prompt := buildClassificationPrompt(input, context)

    response, err := d.llmClient.Complete(ctx, []llm.Message{
        {Role: "system", Content: classificationSystemPrompt},
        {Role: "user", Content: prompt},
    })
    if err != nil {
        return nil, err
    }

    return parseIntentResponse(response.Content)
}
```

---

## Part 8: Files to Create/Modify

### New Files

| File | Purpose |
|------|---------|
| `config/agents/core.toml` | Core agent definitions |
| `config/agents/specialists.toml` | Specialist agent definitions |
| `config/prompts/base/*.md` | Base prompt components |
| `config/prompts/chat/personality.md` | Chat agent chill/sarcastic personality |
| `config/prompts/chat/delegation_rules.md` | Chat delegation rules |
| `config/prompts/dispatcher/*.md` | Dispatcher prompts |
| `config/prompts/specialist/*.md` | Specialist prompts |
| `config/prompts/conditional/*.md` | Conditional prompts |
| `config/prompts/capabilities/*.md` | Capability descriptions |
| `config/prompts/tools/*.md` | Tool descriptions |
| `config/prompts/reminders/*.md` | System reminders |
| `cmd/meept-memvid/main.py` | Memvid FastAPI service |
| `cmd/meept-memvid/requirements.txt` | Python dependencies |
| `internal/memory/memvid/client.go` | Go HTTP client |
| `internal/agent/spec.go` | Agent specification types |
| `internal/agent/registry.go` | Agent registry |
| `internal/agent/dispatcher.go` | Dispatcher agent |
| `internal/agent/protocol.go` | JSONL task message protocol |
| `internal/agent/chat.go` | Chat agent (no tools, delegates) |
| `internal/agent/prompt/builder.go` | Prompt composer |
| `internal/agent/prompt/loader.go` | Component loader |

### Modified Files

| File | Changes |
|------|---------|
| `internal/config/schema.go` | Add AgentsConfig, MemvidConfig |
| `config/meept.toml` | Add [agents], [memvid] sections |
| `internal/task/task.go` | Add memory ref fields |
| `internal/task/store.go` | Persist new fields |
| `internal/agent/loop.go` | Memory injection, task awareness |
| `internal/agent/handler.go` | Route through dispatcher |
| `internal/daemon/components.go` | Wire up new components |

---

## Part 9: Implementation Order

### Sprint 1: Foundation (Week 1)
1. Memvid service (`cmd/meept-memvid/`)
2. Memvid Go client (`internal/memory/memvid/`)
3. Config schema updates

### Sprint 2: Prompt System (Week 2)
4. Prompt component directory structure
5. Base prompt components (constitution, restrictions, task_principles)
6. Prompt builder and loader
7. Conditional injection logic

### Sprint 3: Agent System (Week 3)
8. Agent spec types and loader
9. Agent TOML definitions (core.toml, specialists.toml)
10. Agent registry
11. JSONL task protocol (`internal/agent/protocol.go`)
12. Dispatcher agent

### Sprint 4: Specialist Prompts (Week 4)
13. Dispatcher prompts
14. Chat agent personality and delegation rules
15. Specialist prompts (coder, debugger, researcher, analyst, planner, committer, scheduler)
16. Conditional prompts
17. Capability and tool descriptions

### Sprint 5: Integration (Week 5)
18. Chat agent (no tools, delegates to dispatcher)
19. Task memory refs and inheritance
20. Memory injection in agent loop
21. ChatHandler integration with chat agent
22. End-to-end testing

---

## Part 10: Verification

### 8.1 Unit Tests

```bash
go test ./internal/memory/memvid/...
go test ./internal/agent/prompt/...
go test ./internal/agent/...
```

### 8.2 Integration Tests

```bash
# Start memvid service
cd cmd/meept-memvid && uvicorn main:app --port 8765

# Test memory operations
./bin/meept memory store "test content" --zone episodic
./bin/meept memory search "test" --zone episodic

# Test agent routing
./bin/meept chat "write a hello world function"  # → coder
./bin/meept chat "why is this test failing?"     # → debugger
./bin/meept chat "what is kubernetes?"           # → researcher
./bin/meept chat "summarize this file"           # → analyst
./bin/meept chat "commit these changes"          # → committer
./bin/meept chat "remind me tomorrow at 9am"     # → scheduler

# Test memory continuity
./bin/meept chat "remember: I prefer TypeScript"
./bin/meept chat "what's my preferred language?" # Should recall from memory
```

### 8.3 Memory Inheritance Test

```bash
# Create parent task
./bin/meept task create "Implement login feature"

# Child task should inherit parent's memories
./bin/meept task create "Add password validation" --parent <parent-id>
```

---

## Decisions Made

1. **Memvid integration:** HTTP/gRPC service (Python FastAPI wrapping memvid)
2. **Dispatcher model:** Match user's default model
3. **Prompt architecture:** Recombinant components (Claude Code style)
4. **Agent definitions:** TOML files in config directory
5. **Memory tiers:** Personality, Episodic, Task (with domains)

---

## Remaining Questions

1. **Memory ref limits:** Max refs per task? Auto-prune by relevance?
2. **Agent hot-reload:** Should changing TOML files reload agents without restart?
3. **Fallback behavior:** What if memvid service is unavailable?

---

## Appendix A: Claude Code Prompts for Reuse

Reference repository: `/tmp/claude-code-system-prompts/` (cloned from https://github.com/Piebald-AI/claude-code-system-prompts)

### A.1 Directly Reusable Prompts

These can be adapted with minimal changes:

| Claude Code File | Meept Equivalent | Notes |
|------------------|------------------|-------|
| `system-prompt-doing-tasks.md` | `base/task_principles.md` | Core task execution guidance |
| `system-prompt-executing-actions-with-care.md` | `base/action_safety.md` | Reversibility, blast radius |
| `system-prompt-tone-and-style.md` | `base/tone_style.md` | No emojis, concise, professional |
| `system-prompt-censoring-assistance-with-malicious-activities.md` | `base/security_policy.md` | Security/ethics constraints |
| `system-prompt-tool-usage-policy.md` | `base/tool_usage.md` | Parallel calls, tool selection |
| `system-prompt-task-management.md` | `capabilities/task_management.md` | Task tracking guidance |
| `tool-description-bash-git-commit-and-pr-creation-instructions.md` | `specialist/committer.md` | Git safety protocol |

### A.2 Agent Prompt Patterns

| Claude Code File | Pattern to Apply |
|------------------|------------------|
| `agent-prompt-explore.md` | Read-only exploration agent template |
| `agent-prompt-plan-mode-enhanced.md` | Planning/architecture agent template |
| `agent-prompt-task-tool.md` | General subagent template |
| `skill-debugging.md` | Debug specialist pattern |

### A.3 Key Patterns to Adopt

**1. Variable Interpolation**
```markdown
<!--
name: 'System Prompt: Example'
variables:
  - TOOL_NAME
  - CONFIG_VALUE
-->
Use the ${TOOL_NAME} tool to...
```

Meept equivalent in Go:
```go
type PromptVars struct {
    ToolName    string
    ConfigValue string
}
```

**2. Conditional Sections**
```markdown
${FEATURE_ENABLED ? `
# Feature Section
Content here...
` : ""}
```

**3. Read-Only Agent Pattern**
```markdown
=== CRITICAL: READ-ONLY MODE - NO FILE MODIFICATIONS ===
This is a READ-ONLY task. You are STRICTLY PROHIBITED from:
- Creating new files
- Modifying existing files
...
```

**4. Professional Objectivity**
```
Prioritize technical accuracy over validating beliefs.
Focus on facts and problem-solving.
Avoid excessive praise or emotional validation.
```

### A.4 Prompt File Format for Meept

Based on Claude Code's pattern, Meept prompts should use:

```markdown
<!--
name: 'Prompt Name'
description: Brief description
version: 1.0.0
agent_types: [dispatcher, researcher]  # Which agents use this
conditional: true/false                # Is this conditionally injected?
variables:
  - VAR_NAME
-->

# Section Title

Content with ${VAR_NAME} interpolation...
```

### A.5 Specific Adaptations Needed

**1. Memory Context Injection (Meept-specific)**
```markdown
# Relevant Memory

You have access to shared persistent memory. The following memories are relevant to this task:

${MEMORY_CONTEXT}

Use memory.store() to save new learnings. Use memory.search() to find past context.
```

**2. Agent Delegation (Meept-specific)**
```markdown
# Agent Delegation

You can delegate tasks to specialist agents:
- `coder`: Code writing and modification
- `debugger`: Bug investigation and fixing
- `researcher`: Information gathering
- `analyst`: Deep analysis and synthesis
- `planner`: Task decomposition
- `committer`: Git operations
- `scheduler`: Scheduling and reminders

Use task.create() with appropriate agent_type and memory_refs.
```

**3. Task Inheritance (Meept-specific)**
```markdown
# Task Context

${TASK_CONTEXT}

This task inherits from: ${INHERITED_FROM || "None"}
Memory references from parent: ${INHERITED_MEMORIES || "None"}
```

---

## Appendix B: Prompt Component Inventory

### B.1 Base Components (All Agents)

| File | Tokens (est.) | Purpose |
|------|---------------|---------|
| `base/constitution.md` | ~150 | Core identity and values |
| `base/restrictions.md` | ~100 | Safety constraints |
| `base/task_principles.md` | ~200 | Task execution guidance |
| `base/tone_style.md` | ~150 | Communication style |
| `base/action_safety.md` | ~250 | Reversibility awareness |
| `base/tool_usage.md` | ~150 | Tool usage policies |
| `base/security_policy.md` | ~100 | Security/ethics |

### B.2 Capability Components (Injected Based on Agent)

| File | Tokens (est.) | Purpose |
|------|---------------|---------|
| `capabilities/memory.md` | ~200 | Memory operations |
| `capabilities/tasks.md` | ~150 | Task management |
| `capabilities/platform.md` | ~100 | Platform status |
| `capabilities/delegation.md` | ~200 | Agent delegation |

### B.3 Specialist Components

| File | Tokens (est.) | Purpose |
|------|---------------|---------|
| `specialist/coder.md` | ~300 | Code writing guidance |
| `specialist/debugger.md` | ~350 | Debugging methodology |
| `specialist/researcher.md` | ~250 | Research best practices |
| `specialist/analyst.md` | ~250 | Analysis framework |
| `specialist/planner.md` | ~200 | Planning methodology |
| `specialist/committer.md` | ~400 | Git safety + conventions |
| `specialist/scheduler.md` | ~150 | Scheduling guidance |

### B.4 Conditional Components

| File | Condition | Tokens (est.) |
|------|-----------|---------------|
| `conditional/code_style.md` | has_code_task | ~150 |
| `conditional/error_context.md` | has_error | ~200 |
| `conditional/source_evaluation.md` | researching | ~150 |
| `conditional/analysis_depth.md` | analyzing | ~100 |
| `conditional/task_decomposition.md` | planning | ~150 |
| `conditional/git_safety.md` | git_operation | ~200 |
| `conditional/memory_injection.md` | has_memory_context | ~100 |

### B.5 System Reminders (Dynamic Injection)

| File | Trigger | Purpose |
|------|---------|---------|
| `reminders/plan_mode.md` | plan_mode_active | Plan mode status |
| `reminders/memory_context.md` | memory_results | Memory injection |
| `reminders/task_status.md` | active_task | Current task info |
| `reminders/budget_warning.md` | budget_low | Token budget alert |

---

## Appendix C: Implementation Reference

### C.1 Prompt Builder Pseudocode

```go
func (b *Builder) Build(spec *AgentSpec, ctx *PromptContext) string {
    var parts []string

    // 1. Load base components (all agents)
    for _, ref := range []string{
        "base/constitution",
        "base/restrictions",
        "base/task_principles",
        "base/tone_style",
        "base/action_safety",
        "base/tool_usage",
        "base/security_policy",
    } {
        parts = append(parts, b.load(ref, ctx.Vars))
    }

    // 2. Load agent-specific components from spec
    for _, ref := range spec.PromptComponents {
        if strings.HasPrefix(ref, "conditional.") {
            if !b.shouldInclude(ref, ctx.Conditions) {
                continue
            }
        }
        parts = append(parts, b.load(ref, ctx.Vars))
    }

    // 3. Load capability components based on tools
    if hasMemoryTools(spec.AdditionalTools) {
        parts = append(parts, b.load("capabilities/memory", ctx.Vars))
    }
    if hasTaskTools(spec.AdditionalTools) {
        parts = append(parts, b.load("capabilities/tasks", ctx.Vars))
    }

    // 4. Inject dynamic context
    if ctx.MemoryContext != "" {
        parts = append(parts, "# Relevant Memory\n" + ctx.MemoryContext)
    }
    if ctx.TaskContext != "" {
        parts = append(parts, "# Current Task\n" + ctx.TaskContext)
    }

    // 5. Add system reminders
    for _, reminder := range ctx.ActiveReminders {
        parts = append(parts, b.load("reminders/"+reminder, ctx.Vars))
    }

    return strings.Join(parts, "\n\n---\n\n")
}
```

### C.2 Variable Interpolation

```go
func (b *Builder) interpolate(content string, vars map[string]string) string {
    re := regexp.MustCompile(`\$\{(\w+)\}`)
    return re.ReplaceAllStringFunc(content, func(match string) string {
        key := match[2:len(match)-1]
        if val, ok := vars[key]; ok {
            return val
        }
        return match // Keep unresolved
    })
}
```

### C.3 Conditional Evaluation

```go
var conditionRules = map[string]func(*PromptContext) bool{
    "conditional.code_style":        func(c *PromptContext) bool { return c.Conditions["has_code_task"] },
    "conditional.error_context":     func(c *PromptContext) bool { return c.Conditions["has_error"] },
    "conditional.source_evaluation": func(c *PromptContext) bool { return c.Conditions["researching"] },
    "conditional.git_safety":        func(c *PromptContext) bool { return c.Conditions["git_operation"] },
    // ...
}

func (b *Builder) shouldInclude(ref string, ctx *PromptContext) bool {
    if rule, ok := conditionRules[ref]; ok {
        return rule(ctx)
    }
    return true
}
