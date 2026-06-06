# Meept Features

## Overview

Meept is a Go-based autonomous agent daemon with multi-agent orchestration, persistent hybrid memory, LLM integration with failover, production-grade execution controls, and extensibility through skills and tools. It operates as a background process with multiple frontends (CLI/TUI, Telegram, Web API, macOS MenuBar).

### Architecture Summary

```
User Input (CLI/Telegram/Web/MenuBar)
    ↓
CommServer (Unix socket JSON-RPC or HTTP REST)
    ↓
MessageBus (pub/sub)
    ↓
AgentLoop
    ├── Skill Discovery (capability-index matching)
    ├── ContextFirewall (compression, summarization, budget)
    ├── Planner (task decomposition)
    ├── CollaborativePlanner (review/approval)
    ├── WorkspaceManager (git-backed tracking)
    ├── SecurityEngine (taint, sanitize, audit)
    ├── Evidence Pipeline (tool evidence → validation)
    └── Memory injection (5-tier)
    ↓
Response
```

---

## Core Architecture

### Input Queuing & Steering System

When a user sends input while an agent is actively processing, Meept uses a dual-queue system to manage the incoming message based on urgency and intent.

#### Dual-Queue Architecture

| Queue | Capacity | Behavior | Use Case |
|-------|----------|----------|----------|
| **Steering Queue** | 1 (latest wins) | Interrupts active agent immediately | Urgent redirects, corrections |
| **Follow-up Queue** | 20 (FIFO) | Waits for natural stopping point | General chat, non-urgent input |

#### Activation Modes

| Mode | Trigger | Destination |
|------|---------|-------------|
| **Steer Mode** | ctrl+s in TUI | Steering Queue |
| **Normal** | Regular enter key | Follow-up Queue (if agent active) |
| **Idle** | No agent running | Direct processing |

#### Intent-Based Steering Classification

Even without ctrl+s, the dispatcher classifies input urgency based on intent type:

**High Urgency (Auto-Steer):**
- `IntentCode` - Redirecting coding approach
- `IntentDebug` - Bug spotted mid-execution
- `IntentSecurity` - Security concern
- `IntentToolUse` - Explicit tool redirection
- `IntentGit` - Git operations
- `IntentPlan` - Plan changes

**Low Urgency (Follow-up):**
- `IntentChat`, `IntentRecall`, `IntentResearch`
- `IntentReport`, `IntentPlatform`, `IntentStatus`

#### Message Flow

```
User presses ENTER
    │
    ▼
┌─────────────────────┐
│ Is agent active?    │
├─────────────────────┤
│ NO → Direct to RPC  │
│ YES → Queue mode    │
└─────────────────────┘
    │
    ▼
┌─────────────────────┐
│ Steer mode (ctrl+s)?│
├─────────────────────┤
│ YES → steeringQueue │ (replaces existing, max 1)
│ NO  → followUpQueue │ (FIFO, max 20)
└─────────────────────┘
    │
    ▼
┌─────────────────────┐
│ Dispatcher          │
│ shouldSteer()?      │
├─────────────────────┤
│ YES → Interrupt now │
│ NO  → Wait for stop │
└─────────────────────┘
```

#### TUI Commands

| Command | Description |
|---------|-------------|
| Normal enter | Send message (queues if agent active) |
| ctrl+s + enter | Force steering mode (urgent interrupt) |

#### Configuration

```json5
// Queue configuration (internal/agent/queue.go)
{
  max_steering: 1,      // Always 1 (latest wins)
  max_follow_up: 20,    // Configurable
  steering_drain: "one",
  follow_up_drain: "one", // or "all" to drain entire queue
}
```

**Key Files:**
- Queue implementation: `internal/agent/queue.go`
- Steering decision: `internal/agent/dispatcher.go:shouldSteer()`
- Steering heuristic table: `internal/agent/dispatcher.go:28-46`
- TUI input handling: `internal/tui/models/chat.go:doSendMessage()`

---

### Task Interrupt & Amendment System

Meept supports real-time task interruption and dynamic plan amendment during execution.

#### Interrupt System

| Component | Description |
|-----------|-------------|
| **InterruptToken** | Thread-safe trigger mechanism for task cancellation |
| **InterruptManager** | Manages lifecycle of interrupt tokens per task |
| **Job Queue Integration** | Skips jobs from cancelled tasks during Claim() |

**Interrupt-Aware Queue Claiming:**
```go
// Queue skips jobs from cancelled tasks
pendingJobs, err := q.store.ListByState(StatePending, 50)
for _, job := range pendingJobs {
    if job.TaskID != "" && q.isTaskCancelled(job.TaskID) {
        continue // Skip cancelled tasks
    }
    if job.CanBeClaimedBy(caps) {
        return q.store.ClaimNextByID(job.ID, workerID)
    }
}
```

#### Amendment System

| Amendment Type | Description |
|----------------|-------------|
| **inject_context** | Add new context/constraints to task |
| **skip_step** | Skip a specific step (removes dependencies) |
| **add_step** | Insert new step into task plan |
| **reprioritize** | Reorder step execution sequence |
| **change_agent** | Reassign step to different specialist agent |

**TUI Slash Commands:**
| Command | Description |
|---------|-------------|
| `/tasks [state]` | List tasks with optional state filter |
| `/cancel <task-id>` | Cancel task by ID |
| `/amend <type> <args>` | Submit amendment request |

**Architecture:**
```
User Input (TUI slash command)
    ↓
CommandHandler.executeCancel() / executeAmend()
    ↓
InterruptManager.Trigger() / AmendmentHandlers.handle*()
    ↓
Task Registry Update + Bus Event
    ↓
Queue Claim() skips cancelled jobs
```

**Configuration:**
```toml
[agent.interrupt]
enabled = true
propagate_to_subtasks = true
```

**API:**
```go
// Cancel a task
token, exists := registry.InterruptManager().Get(taskID)
if exists {
    token.Trigger() // Marks task as cancelled
}

// Set queue callback for interrupt-aware claiming
queue.SetTaskCancelledCallback(func(taskID string) bool {
    token, exists := interruptMgr.Get(taskID)
    return exists && token.IsTriggered()
})

// Submit amendment
amendment := &AmendmentRequest{
    Type:    AmendmentSkipStep,
    TaskID:  taskID,
    StepID:  stepID,
    Content: "skipped due to user request",
}
reply := handlers.handleSkipStep(ctx, amendment)
```

---

### Agent Loop & Execution Controls

The agent loop is the heart of Meept. Unlike simple `while (!done)` loops, it implements multiple independent safety and efficiency mechanisms:

#### Reasoning Cycle (`loop.go:reasoningCycle`)

Runs up to `MaxIterations` (default 25) with three termination cases:
1. **LLM returns text** — turn complete, return response
2. **LLM returns tool calls** — execute tools, feed results back, continue loop
3. **Budget/safety limit hit** — graceful wrap-up with partial results

#### Anchor Messages
System messages injected into conversation history to preserve context during summarization/compression. When running with a task, the loop injects `[step-context]` anchors that survive context pruning so task execution state is maintained.

#### Safety Mechanisms

| Mechanism | Description | Trigger |
|-----------|-------------|---------|
| **Cycle Detector** | SHA256 hashes of tool name + normalized args; detects exact repeats | Same tool+args seen twice |
| **Convergence Detector** | Content-hash history; detects stagnant non-tool responses | Last N responses identical (no tools) |
| **Watchdog** | Heartbeat monitoring per worker with stage tracking | Timeout, missed heartbeat, stuck state |
| **Token Budgets** | Three nested budgets: per-iteration, per-conversation, per-session | Budget exhausted |
| **Model Failover** | Alias resolution with rate-limit rotation | Rate limit or model failure |
| **Empty Response Nudge** | Injects system prompt when LLM returns empty content | Empty assistant response |

#### Token Budget Hierarchy

```
Session Budget: 100,000 tokens / 10 turns max
    │
    ├── Turn Budget: 30,000 tokens per iteration
    │       ├── Tool Definition Overhead: accurately counted
    │       └── Message Budget: residual after tool overhead
    │
    └── Conversation Budget: 50,000 tokens total
            ├── Warning at 80%: tools removed, wrap-up instruction injected
            └── Dynamic tool result compression scales with consumption
```

#### Evidence Requirements

Agents are instructed to substantiate claims with evidence. The loop injects validation anchor instructions into every new conversation.

**Configuration:**
```toml
[agent]
max_iterations = 25
timeout_seconds = 300
max_conversation_tokens = 50000

[agent.memory]
recall_mode = "auto"  # auto, on-query, hybrid, disabled
snapshot_caching_enabled = true
```

---

### Context Firewall

A transparent wrapper around the LLM client that manages context pressure before it causes failures.

#### Features

- **Hierarchical Compression**: Multi-stage summarization with structured extraction (DECISIONS, FILES, QUESTIONS, STATUS, FINDINGS)
- **Proactive Compression**: Multi-stage context compressor with LLM-summarized compression at stage 2 (keeps system + summary + last 4 messages), aggressive compression at stage 3 (keeps system + critical + last 4), and hard limit at stage 4 (keeps system + last 2)
- **LLM Summarization**: When a summarizer model is available, old conversation history is summarized via LLM rather than silently dropped, preserving key decisions, file paths, and task status
- **Token-Aware Truncation**: `TruncateByTokens()` considers tool definition overhead
- **Windowed Messages**: Preserves system prompt + original user message + recent context
- **Budget Ratios**: Configurable iteration (30%) and conversation (50%) budget allocation

**Three-Layer Context Management:**

| Layer | Component | Trigger | Behavior |
|-------|-----------|---------|----------|
| **Layer 1** | Context Compactor | `compaction_trigger_ratio` (e.g., 0.6) | LLM-based structured summarization replacing old messages with compaction entries |
| **Layer 2** | Context Compressor | Stage thresholds (70%, 85%, 95%) | Multi-stage compression: keeps system + summary + recent messages |
| **Layer 3** | Hard Limit | Token budget exhaustion | Keeps system + last 2 messages only |

**Structured Compaction:** The Context Compactor generates `[Compacted Context]` entries that replace ranges of old messages. Compaction entries store JSON with summary, compressed message IDs, tokens saved, and file operation tracking. The context assembler skips compressed messages and substitutes compaction summaries. Split-turn compaction handles cases where an assistant response spans a tool-call round trip by merging both halves into a single summary.

**Structured Summarization Output:**
```
[Conversation summary level 1]: status: debugging auth issue.
decisions: using jwt middleware; files: auth.go, middleware.go;
open questions: should refresh tokens be rotated?;
```

**Configuration:**
```toml
[agent]
proactive_compression = true
model_context_limit = 32768  # override model default

[agent.compaction]
enabled = true
trigger_ratio = 0.6          # compact when 60% of context used
summary_model = ""           # model for compaction summaries (empty = default)
```

---

### Session Persistence & Branching

Meept bridges its in-memory ConversationStore with SQLite-backed persistent storage, enabling session resumption across daemon restarts and tree-structured conversation branching.

#### Session Resumption

On daemon startup or when accessing a conversation not in the in-memory cache:
1. Query SQLite for the session's message path from root to the `leaf_message_id` pointer
2. Reconstruct the `[]llm.ChatMessage` slice including tool calls, compaction entries, and branch summaries
3. Populate the in-memory `Conversation` object for the agent loop
4. Apply message limits if configured

Incremental persistence tracks already-saved message count to avoid re-inserting all messages each turn. Only new messages are appended with proper `parent_id` chaining.

#### Tree-Structured Branching

Messages use a `parent_id` column enabling tree-structured conversations:

```
root ─── msg1 ─── msg2 ─── msg3 (leaf)
                     └── msg4 ─── msg5 (branch B)
```

**Branch Navigation:** Moving the leaf pointer to a prior message creates a fork. The abandoned branch can be summarized via LLM for context in sibling branches.

**Branch Summarization:** When navigating away from a branch with 5+ messages, the system generates a `[Branch Summary]` that is included in context for sibling branches.

**Session Forking:** Copy a conversation subtree to a new session for parallel exploration. The fork operation is transactional and preserves the full message tree.

**Context Assembly:** The `AssembleBranch` function walks the message path from root to leaf, skipping compacted messages (tracked via `compressed_ids` in compaction entries) and including compaction summaries and branch summaries as system messages.

**CLI Commands:**
```bash
./bin/meept branch list <session-id>       # List branches in a session
./bin/meept branch navigate <message-id>   # Move to a branch point
./bin/meept branch tree <session-id>       # Show tree structure
./bin/meept branch summary <session-id>    # Show branch summaries
```

**TUI:** `Ctrl+B` opens branch navigator; current branch shown in status bar.

**Configuration:**
```json5
{
  session: {
    persistence: true,              // Enable session resumption
    branching: true,                // Enable conversation branching
    max_branches: 20,               // Max branches per session (0 = unlimited)
    branch_summary_threshold: 5,    // Min messages before branch summarization
    restore_message_limit: 0,       // Max messages to restore (0 = all)
    compaction: true,               // Enable compaction entries
  }
}
```

---

### Multi-Agent System

Meept uses a specialist-agent architecture where different agents handle different task types.

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
- `delegate_task`: Route a task to a specific agent (synchronous, blocking)
- `request_handoff`: Dynamically inject a new step into the running task DAG and route it to another agent (async, non-blocking)

#### Dynamic Agent Handoff

Agents executing within the orchestrator pipeline can dynamically re-route to other agents mid-task using the `request_handoff` tool. This is distinct from `delegate_task` (synchronous blocking delegation) — handoff creates a real step in the task DAG with proper dependency wiring.

**Flow:**
1. Agent discovers mid-execution it needs different expertise (e.g., coder finds a runtime bug)
2. Agent calls `request_handoff` with target agent, description, and partial results
3. Tool publishes `orchestrator.handoff` bus event and returns immediately (agent continues)
4. TacticalScheduler processes the event: creates a new step, sets dependencies, rewires downstream
5. New step is scheduled via the existing amendment/step promotion system

**Key properties:**
- **Async**: Calling agent's step continues to completion normally
- **DAG integration**: Creates a real `TaskStep` with dependency wiring
- **Dependency rewiring**: Steps that depended on the originating step are rewired to depend on the injected step (maintains execution order)
- **Rate limiting**: `MaxHandoffSteps` (default 5 per task) prevents runaway chains
- **Amendment path**: When `HandoffUseAmendment` is enabled, handoffs route through the amendment system for review/approval before step creation
- **Context propagation**: The receiving agent gets the calling agent's partial results in `AccumulatedContext`

#### Steering and Follow-Up Queues

Real-time message injection into active agent conversations without restarting them.

**Steering** (urgent redirection): When the user sends a new message while an agent is executing, the dispatcher classifies the intent using a steering heuristic table. High-urgency intents (code, debug, git, plan, security, tool-use) immediately inject into the active agent's conversation via the message queue, causing the agent to pivot mid-execution.

**Follow-up** (queued context): Low-urgency intents (chat, memory, report, review, search, skill) are queued and injected after the current turn completes, providing context without interrupting flow.

**Key features:**
- Generation counters prevent stale queue operations from previous conversations
- SQLite persistence for follow-up messages survives daemon restarts
- Write-behind buffering with configurable flush delay
- Single-message drain for steering (processes one message at a time)
- Agent lifecycle events for queue registration/unregistration

**TUI:** `Ctrl+S` toggles steer mode; queue status shown in status bar.

**Configuration:**
```json5
{
  agent: {
    queues: {
      steering_drain: "one",    // "one" (single) or "all" (batch)
      followup_drain: "all",
      max_steering: 1,
      max_followup: 20,
      persist_followup: true,
      flush_delay_ms: 500,
    }
  }
}
```

#### Compound Task Acknowledgment

When the dispatcher detects a compound (multi-intent) request, it sends an enhanced async acknowledgment to the user before orchestration begins:

```
## starting task

**task:** build a feature with api, database, and tests
**id:** `task-xxx`
**plan:** `plan-xxx` | 4 subtasks | est. 12-17 min

**agents:** committer, coder, tester

**subtasks:**
- create database migrations (committer)
- implement api endpoints (coder)
- write integration tests (tester)
- deploy to staging (devops)

you will receive updates as subtasks complete.
```

**Features:**
- Subtask count and bulleted summary (max 5 displayed, with overflow indicator)
- Estimated duration from historical metrics (with 4 min/step fallback heuristic)
- Multi-agent detection showing which specialists are involved
- Rune-safe description truncation at 50 characters
- TUI renders markdown ACKs with full formatting; legacy JSON ACKs render as detached task cards

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

### Deterministic Execution Framework

A comprehensive framework ensuring reliable, verifiable task completion.

#### Evidence Pipeline

```
ToolResult.Evidence → ExecutionResult.Evidence → TaskStep.Evidence → Validator
```

**Evidence Types:**
| Type | Description | Produced By |
|------|-------------|-------------|
| `file_exists` | File exists at path with metadata | ReadFile, WriteFile, DeleteFile, ListDirectory |
| `file_hash` | SHA256 hash of file content | ReadFile, WriteFile |
| `process_exit` | Process exit code | Shell |
| `shell_output` | Command output (hashed) | Shell |
| `api_response` | HTTP status and response size | WebFetch, WebSearch |
| `db_row` | Database operation metadata | Memory operations |

**Claim-Evidence Matching:**
The validator detects mismatches between agent claims and evidence:

| Claim Pattern | Required Evidence |
|---------------|-------------------|
| "created", "wrote", "modified", "updated" | `file_exists` or `file_hash` |
| "executed", "ran", "command", "shell" | `process_exit` |
| "fetch", "api", "http", "web" | `api_response` |
| "memory", "stored", "retrieved", "context" | `db_row` |

#### Concurrency Control

- Global semaphore (default 10 concurrent jobs)
- Per-agent semaphore (default 3 per agent)
- Non-blocking acquisition with immediate fallback
- Blocked steps remain in "ready" state for next scheduling cycle

#### Retry Logic Hierarchy

| Level | Location | Behavior |
|-------|----------|----------|
| L1 | Per-tool | Tool-specific policies (0-2 retries), exponential backoff for network |
| L2 | Job-level | Rate limit retry with backoff (2s, 4s, 8s) |
| L3 | Agent loop | Model failover + exponential backoff (max 5 attempts) |

#### Validation Gates

- Configurable interval (default every 3 steps)
- Non-blocking: logs warnings without stopping execution
- Checks all completed steps have `Validated = true`
- **Validation retry loop**: On validation failure, steps are re-queued up to `MaxValidationLoops` (default 2) before escalating to human review

#### Checkpoints

Git-based checkpoints enable recovery:
- `CreateCheckpoint(taskID, label)` → git tag `checkpoint-{taskID}-{label}-{timestamp}`
- `RestoreCheckpoint(taskID, label)` → checkout most recent checkpoint tag
- `ListCheckpoints(taskID)` → all checkpoints for task

**Configuration:**
```toml
[execution]
max_concurrent_jobs = 10
max_concurrent_per_agent = 3
validation_gate_interval = 3

[retry]
max_retries = 3
retry_delay_base = "2s"
transient_error_patterns = ["timeout", "connection refused", "network"]

[validation]
require_evidence = true
fail_unknown_evidence_types = true
enable_checkpoints = true
```

**API:**
```go
// Evidence is attached automatically by built-in tools
result := &ToolResult{
    Result: "file written",
    Evidence: []models.Evidence{
        models.NewEvidence(models.EvidenceFileHash, "/tmp/test.txt", "sha256:abc...", "file_write"),
    },
}

// Validator checks claims against evidence
validationResult := validator.ValidateStep(ctx, step)
if !validationResult.Valid {
    log.Warn("validation failed", "errors", validationResult.Errors)
}
```

---

### Hallucination Detection

A configurable detector that analyzes LLM output for hallucination indicators.

**Detection Types:**
- **Confident Claims**: Unsubstantiated assertions without tool evidence
- **Fabricated References**: Mentions of files, functions, or URLs not in tool results
- **Contradictions**: Output contradicts previous conversation history
- **Impossible Responses**: Claims that violate known constraints

**Sensitivity Levels:**
- `low` (default): Conservative, minimal false positives
- `medium`: Balanced detection
- `high`: Aggressive, may flag legitimate creative responses

**Recovery:** When `MaxIndicators` threshold is exceeded, recovery is recommended.

**Configuration:**
```toml
[agent.hallucination]
enabled = true
sensitivity = "low"
max_indicators = 2
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

#### Semantic Memory (Vector Embeddings)
- Vector similarity search using embeddings
- Hybrid search combining keyword (FTS) and vector scores via `KeywordSearcher` interface
- Supports OpenAI and Ollama providers
- Cosine similarity for ranking

#### Memory Consolidation & Clustering
- **Semantic clustering**: Union-find algorithm groups memories by cosine similarity threshold
- **3-tier MergeRelated strategy**: (1) embedding clustering + LLM summarization, (2) LLM topic grouping, (3) date-based fallback
- **Robust JSON parsing**: Multi-strategy extraction from LLM responses (direct parse, markdown fence extraction, bracket matching with string/escape awareness)
- **ConsolidationBackend interface**: Pluggable storage backends for consolidation — `SQLiteConsolidationBackend` (default) and `MemvidConsolidationBackend` for distributed memory
- Configurable consolidation interval

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
    VectorStore:     store,
    KeywordSearcher: memManager, // Manager satisfies KeywordSearcher interface
    Alpha:           0.5,
})
results, err := hybrid.Search(ctx, "query", 20)
```

#### Personality Memory
- Tracks user preferences over conversations
- Updates every N conversations
- Influences response style and behavior

#### Context Propagation to Subtasks

Child steps inherit parent task context and accumulate knowledge from prior completed steps.

**Flow:** Parent task `MemoryRefs` → first step → subsequent steps. Each step's evidence/output is appended to an `AccumulatedContext` that becomes available to the next step.

- `TaskStep.MemoryRefs` — memory IDs inherited from parent task or accumulated from prior steps (deduplicated)
- `TaskStep.AccumulatedContext` — evidence/outputs from prior steps, appended with `---` separators
- `StrategicPlanner` copies parent task `MemoryRefs` to the first step during planning
- `TacticalScheduler.propagateContextToNextSteps()` copies completed step's result and memory refs to all ready next steps
- Agent prompts include a context section listing available memories and accumulated findings from prior steps

---

### Security

Meept implements multiple layers of security.

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

#### Taint Tracking
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

monitor_output = true
redact_output = true

scan_shell_commands = true
tirith_binary = "tirith"

enable_audit_log = false
audit_db_path = "~/.meept/audit.db"

strict_override_matching = false
```

**API:**
```go
tracker := taint.NewTracker(logger)

// Mark input as tainted
tainted := tracker.MarkUserInput(userInput, "cli:args")

// Check before shell execution
violation := tracker.CheckShellCommand(cmd)
if violation != nil {
    log.Warn("Shell command blocked", "violation", violation)
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

#### Token Cache Metrics
- L1 cache uses LRU eviction (based on `LastAccessedAt` timestamp) instead of FIFO
- L1/L2 cache eviction events recorded to metrics store (`cache.eviction` with reason tags: `lru`, `ttl_expired`, `file_invalidation`)
- Entry count tracking (`cache.entry_count` per level)
- Hit/miss counters for both cache levels

#### Native Anthropic Driver
- Native implementation of Anthropic's Messages API
- Extended thinking mode support
- Streaming with progress callbacks
- SSE parsing for real-time updates

#### Model Failover
When rate limits hit:
1. Rotate to next model in alias (immediate retry)
2. Exponential backoff if alias exhausted (2s → 4s → 8s → 16s → 32s)
3. Max 5 attempts before returning error

#### Token Usage Trickle-Up

Real-time token usage tracking that aggregates from child steps to parent tasks and displays in chat and sidebar.

**Flow:** AgentLoop → `llm.tokens.used` bus event → TacticalScheduler aggregates → Task.Metadata → periodic progress events → TUI display.

- Each `Task` and `TaskStep` tracks `TokenUsage` via `AddTokenUsage()` method
- AgentLoop publishes token counts after each LLM call via bus events
- TacticalScheduler aggregates step tokens into parent task on job completion
- Token counts displayed in sidebar (e.g., "1.5K tok") and chat progress messages
- Task completion messages include total token usage summary

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
client := llm.NewAnthropicClient(config, opts...)

// Simple chat
resp, err := client.Chat(ctx, messages)

// With progress reporting
resp, err := client.ChatWithProgress(ctx, messages, func(stage llm.ProgressStage, detail string) {
    log.Info("Progress", "stage", stage, "detail", detail)
})
```

---

### Model Reassignment

Users can override default agent model assignments via natural language instructions during chat. The dispatcher parses model reassignment directives, asks clarifying questions when ambiguous, and applies model overrides to specific tasks or task steps.

**Usage Examples:**
```bash
# Use specific model for a task type
meept chat "Research best practices, then use glm-4.7 for synthesis"

# Provider-specific models for different phases
meept chat "Use local models for research, GLM for coding"

# Interactive clarification (if ambiguous)
meept chat "Use GLM models for this"
# Dispatcher asks: "Which GLM model? glm-4.7 or glm-4.5-air?"
```

**Supported Patterns:**
| Pattern | Example | Result |
|---------|---------|--------|
| "use X for Y" | "use GLM for coding" | Model override for coding tasks |
| "X models for Y" | "GLM models for planning" | Model override for planning |
| "synthesize using X" | "synthesize using claude-opus" | Model override for synthesis |
| "code with X" | "code with qwen-coder" | Model override for coding |
| "I want X to handle Y" | "I want GLM to handle research" | Model override for research |

**Scope Keywords:**
| Scope | Intent Type | Agent |
|-------|-------------|-------|
| synthesis, planning, plan, design | `IntentPlan` | planner |
| coding, code, implementation | `IntentCode` | coder |
| research, analysis, analyze | `IntentResearch` | analyst |
| debugging, debug, fix | `IntentDebug` | debugger |

**Model Aliases:**
| Alias | Resolves To |
|-------|-------------|
| `opus`, `claude-opus` | `anthropic/claude-3-opus` |
| `sonnet` | `anthropic/claude-3-sonnet` |
| `glm`, `glm-4.7` | `zai/glm-4.7` |
| `qwen`, `qwen-coder` | `ollama/qwen2.5-coder` |
| `llama`, `llama3.2` | `ollama/llama3.2` |

**Implementation Details:**
- Parser: `internal/agent/model_parser.go` with regex pattern matching
- Directive: `ModelReassignmentDirective` captures parsed instructions
- Task metadata: Model overrides stored in `task.Metadata` for single-intent tasks
- Step overrides: `TaskStep.ModelOverride` for compound multi-step tasks
- AgentLoop: Reads model override from task metadata and switches models via `SwitchModel()`
- Clarification: Dispatcher asks clarifying questions for ambiguous references (e.g., "GLM models" without specific model)

**Learn more:** [Multi-Agent System - Model Reassignment](concepts/multi-agent.md#model-reassignment)

---

### Tools

Meept provides built-in tools and supports MCP (Model Context Protocol) for external tools.

#### Built-in Tools
- File operations: `file_read`, `file_write`, `list_directory`
- Memory: `memory_store`, `memory_search`, `memory_get_context`
- Platform: `platform_agents`, `platform_status`, `platform_tools`, `delegate_task`, `request_handoff`
- Git: `git_commit`, `git_diff`, `git_status`

#### Knowledge Graph Tools
| Tool | Description |
|------|-------------|
| `entity_create` | Create graph nodes |
| `entity_link` | Link entities (5 relation types) |
| `entity_query` | Query related entities |
| `graph_stats` | Graph statistics |
| `compute_pagerank` | Recompute importance scores |
| `detect_communities` | Find clusters |
| `community_siblings` | Find entities in same community |

#### Scheduling Tools
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

#### Web Search
- DuckDuckGo HTML search (no API key required)
- Rate limiting (configurable)
- Returns title, URL, snippet
- Automatic URL cleaning and HTML entity decoding

#### Code Intelligence Tools
| Tool | Category | Description |
|------|----------|-------------|
| `ast_parse` | AST | Parse source file into AST (tree-sitter) |
| `ast_symbols` | AST | Extract symbols (functions, types, imports) |
| `ast_query` | AST | Run tree-sitter queries |
| `lsp_goto_definition` | LSP | Navigate to symbol definition |
| `lsp_find_references` | LSP | Find symbol references |
| `lsp_hover` | LSP | Get type/documentation info |
| `lsp_workspace_symbols` | LSP | Search symbols across workspace |
| `lsp_diagnostics` | LSP | Get errors/warnings from language server |

**Configuration:**
```toml
[scheduler]
enabled = true
timezone = "UTC"
```

**API:**
```go
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

### Code Intelligence

Meept includes multi-language code understanding via tree-sitter parsing and LSP client integration.

**AST Tools (`internal/code/ast/`):**
- Tree-sitter parser for 10+ languages
- Symbol extraction (functions, types, imports, structs)
- Tree-sitter query execution
- Language detection from file extension
- AST-based code compression (`CompressCodeAtBoundaries`): truncates at function/block boundaries for 10+ languages with fallback to byte/line truncation

**LSP Client (`internal/code/lsp/`):**
- Multi-server management (different servers per language)
- JSON-RPC 2.0 communication
- Go-to-definition, find-references, hover, workspace symbols, diagnostics
- Document synchronization

---

### Markdown Agent Definitions

Agents can be defined using `AGENT.md` files with YAML frontmatter.

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

Global rules are injected into all agent prompts.

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

Agent report JSON is automatically stripped from user-facing responses.

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

#### Runtime Skill Discovery
- **CapabilityIndex**: Metadata-driven matching without loading full skill bodies
- **LazySkillLoader**: On-demand loading with caching
- **Tool Filtering**: When a skill declares `allowed-tools`, the registry is filtered for that execution
- **Confidence Threshold**: Minimum confidence for skill matching (default 0.5)

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

### Q Agent (Meta-Optimization)

The Q Agent (Quartermaster) is a meta-agent that analyzes system performance and designs improvements.

**Pipeline:**
1. **Fetch** completed sessions from memory
2. **Analyze** sessions for error patterns, duration variance, rejection rates
3. **Detect** recurring patterns across sessions
4. **Research** root causes via memory search
5. **Design** new agent configurations or skills
6. **Estimate** impact (token savings, time reduction)
7. **Validate** proposals before applying

**CLI Commands:**
```bash
./bin/meept q status                   # Show Q Agent status
./bin/meept q analyze                  # Analyze sessions
./bin/meept q analyze --force          # Force analysis
./bin/meept q analyze --json           # Output as JSON
```

**Configuration:**
```toml
[q_agent]
enabled = false
analysis_interval_hours = 24
session_idle_trigger_hours = 6
min_sessions_for_pattern = 5
high_error_rate_threshold = 0.3
high_rejection_rate_threshold = 0.25
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

Triggered asynchronously after every successful conversation.

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
| **Evidence-Based Execution** | All agent claims validated against tool-produced evidence |
| **Context Firewall** | Multi-stage proactive compression with LLM summarization, hierarchical re-summarization |
| **Context Compaction** | Three-layer system: LLM-based compaction, multi-stage compression, hard limit with `[Compacted Context]` entries replacing old messages |
| **Session Persistence & Branching** | SQLite-backed session resumption across restarts, tree-structured branching with LLM summarization, session forking |
| **Deterministic Execution** | Concurrency control, validation gates, checkpoints, retry hierarchy |
| **MCP Protocol Support** | First-class Model Context Protocol integration for external tools |
| **Agent Coworker Awareness** | Agents discover and delegate to each other via platform tools |
| **Steering & Follow-Up Queues** | Real-time message injection: urgent steering interrupts active agents, follow-up queues context for after current turn |
| **Compound Task ACK** | Enhanced async acknowledgment with subtask summary, duration estimates, multi-agent detection |
| **Markdown Agent Definitions** | User-customizable AGENT.md files with YAML frontmatter, 4-tier discovery |
| **Global Rules & Reporting** | Platform-wide rules with structured JSON reports |
| **Q Agent** | Meta-agent for session analysis and optimization design |
| **Learning Pipeline** | Shadow training, trajectory learning, and automated fixing |
| **ClawSkills Marketplace** | Third-party skill marketplace with security scanning |
| **Self-Improvement System** | Automated detection, fixing, and validation of code issues |
| **Advanced Knowledge Graph** | PageRank scoring, community detection, hybrid search |
| **Multi-Tier Memory** | Episodic, task, knowledge graph, distributed, and semantic memory |
| **Context Propagation** | Child steps inherit parent MemoryRefs and accumulate findings from prior steps |
| **Token Usage Trickle-Up** | Real-time token tracking aggregated from steps to tasks, displayed in chat and sidebar |
| **Progress Event Reliability** | All progress updates visible in chat with no rate limiting; immediate error escalation |
| **Taint Tracking** | Lattice-based information flow tracking for security |
| **Native Anthropic Driver** | Extended thinking mode with progress reporting |
| **Web Search (No API Key)** | DuckDuckGo integration without API requirements |
| **Code Intelligence (AST+LSP)** | Tree-sitter parsing, AST-based code compression, and LSP client tools |
| **Semantic Memory Clustering** | Union-find cosine similarity grouping for memory consolidation |
| **Responsive TUI** | Adaptive layout with fuzzy finder, message threading, task detail modal, branch navigator |
| **Validation Retry Loop** | Automatic step re-queue on validation failure with configurable max retries |
| **Model Failover** | Alias rotation with exponential backoff |
| **Hallucination Detection** | Pattern-based detection with configurable sensitivity |

### External Integrations

| Integration | Description |
|-------------|-------------|
| **Telegram Bot** | Two-way communication via Telegram |
| **Web API** | HTTP/JSON API for external clients |
| **HTTP REST** | REST API for macOS MenuBar app (disabled by default; cache invalidate/inspect endpoints) |
| **Google Calendar** | Calendar event management |
| **Git Worktrees** | Isolated task execution environments |
| **macOS MenuBar** | Native SwiftUI monitoring and control app |

---

### TUI (Terminal User Interface)

The interactive TUI provides a rich terminal interface built with Bubbletea v2.

#### Layout Modes
- **Compact** (<80 cols): No sidebar, single panel
- **Standard** (80-120 cols): Narrow sidebar (25 chars)
- **Wide** (>120 cols): Full sidebar (up to 35 chars)

#### Features
- **Responsive layout**: Automatically adapts to terminal width
- **Fuzzy finder**: `Ctrl+P` opens session/task search modal
- **Message threading**: Conversation turns grouped with visual separators
- **Task detail modal**: Enter on a task shows full step breakdown, progress bars, memory context
- **Slash commands**: `/tasks`, `/cancel`, `/amend` for task management
- **Multi-panel view**: Chat, tasks, and sidebar panels
- **Progress event reliability**: All progress updates visible in chat (not silently dropped). `chat_visible` flag replaces deprecated `silent` flag. No rate limiting on progress events — all are delivered. Error escalation path ensures step failures appear in chat immediately via `task.error` topic.

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

# Agent Loop
[agent]
max_iterations = 25
timeout_seconds = 300
max_conversation_tokens = 50000

[agent.memory]
recall_mode = "auto"
snapshot_caching_enabled = true

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

# Execution Framework
[execution]
max_concurrent_jobs = 10
max_concurrent_per_agent = 3
validation_gate_interval = 3

[validation]
require_evidence = true
enable_checkpoints = true
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

# TUI Slash Commands (Interactive Mode)
/tasks [state]                    # List tasks (optionally filter by state)
/cancel <task-id>                 # Cancel task by ID
/amend <type> <args>              # Submit amendment request

# Status
./bin/meept status                 # Show daemon status
./bin/meept agents                 # List agents
./bin/meept tools                  # List tools

# Branch
./bin/meept branch list <session>  # List branches in a session
./bin/meept branch navigate <id>   # Navigate to a branch point
./bin/meept branch tree <session>  # Show tree structure
./bin/meept branch summary <session> # Show branch summaries

# Jobs
./bin/meept jobs list              # List jobs
./bin/meept jobs run <job-id>      # Run job immediately

# Memory
./bin/meept memory search "query"  # Search memories
./bin/meept memory stats           # Memory statistics

# Q Agent
./bin/meept q status               # Q Agent status
./bin/meept q analyze              # Analyze sessions

# ClawSkills
./bin/meept clawskills list        # List installed skills
./bin/meept clawskills install <slug>  # Install skill

# Self-Improve
./bin/meept selfimprove detect     # Detect issues
./bin/meept selfimprove full-cycle # Run full improvement cycle

# Models
./bin/meept models setup           # Interactive model configuration
./bin/meept models list            # List configured models
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

### Evidence Pipeline
```go
// Tool produces evidence automatically
toolResult := &ToolResult{
    Result: "file written",
    Evidence: []models.Evidence{
        models.NewEvidence(models.EvidenceFileHash, "/tmp/test.txt", "sha256:abc...", "file_write"),
    },
}

// Executor propagates to ExecutionResult
execResult := executor.Execute(ctx, toolCall)

// Tactical scheduler persists to TaskStep
// Validator checks claims against evidence
```

---

## Agentic Pairs

Meept supports four pairing modalities for agent collaboration. The orchestrator selects the appropriate modality based on task characteristics.

### Pairing Modalities

| Modality | Use Case | Mechanism |
|----------|----------|-----------|
| **Spec-Driven Review** | Code/debug tasks with acceptance criteria | Reviewer checks step output against spec generated during planning; rejection creates revision steps with structured feedback |
| **Pair Session** | Complex multi-round tasks, security-sensitive changes | Two agents iterate on a full task with shared working memory and convergence tracking |
| **Bus Channel** | Research debates, brainstorming, exploratory debugging | Two agents share a named bus topic and take turns via PairOrchestrator |
| **Inline Review** | Lightweight self-review during development | Actor agent calls `request_review` tool within its own execution loop |

### Spec-Driven Review (Default)

When the strategic planner creates a plan, it generates acceptance criteria stored in task metadata:

```
Plan() → GenerateSpecFromSteps() → TaskSpec stored in task metadata
    ↓
Step completes → ReviewStep(spec) → reviewer checks against criteria
    ↓ (rejected)
BuildRevisionContext() → revision step carries feedback + spec to coder
    ↓ (max revisions exceeded)
Escalate to human with spec-aware feedback message
```

### Pair Session

For compound or security-sensitive tasks, two agents share context across multiple rounds:

```
StrategicPlanner detects complex task → creates PairSession
    ↓
Round N: actor executes → reviewer reviews → shared context updated
    ↓ (criteria remaining)
Round N+1: actor sees full history + remaining criteria
    ↓ (all criteria satisfied or max rounds reached)
Converged → task completed, or Exhausted → task failed
```

### Channel-Based Pairing

Two agents communicate via the message bus for free-form collaboration:

```
IntentPair detected → PairOrchestrator subscribes to pair.start
    ↓
Actor publishes output → reviewer evaluates → verdict classified
    ↓ (rejected)
Revision prompt constructed → actor runs again
    ↓ (approved)
Result published to pair.result → ChatHandler relays to user
```

### Inline Review Tool

The `request_review` tool lets any agent request synchronous review within its own loop:

```go
// Coder agent calls during execution:
request_review(
    message: "Implemented the auth handler",
    work_content: "<code>",
    caller_agent_id: "coder"
)
// Returns: InlineReviewResult{status: "approved/rejected", issues: [...]}
```

---

## See Also

- **CLAUDE.md**: Development guidelines and architecture
- **README.md**: Installation and quick start
- **diagram.md**: Architecture diagrams
- **docs/workflows/**: Feature specifications
