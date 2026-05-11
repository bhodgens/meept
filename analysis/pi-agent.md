# Pi Agent (`@earendil-works/pi-agent-core`) - Analysis & Meept Comparison

> Source: https://github.com/earendil-works/pi/tree/main/packages/agent
> Version analyzed: 0.74.0 (2026-05-07)
> Author: Mario Zechner (badlogic)
> License: MIT
> Language: TypeScript (Node.js >= 20)

## 1. Executive Summary

Pi Agent is a **general-purpose agent loop library** focused on one thing extremely well: orchestrating LLM reasoning turns with tool execution. It is a library, not a standalone application -- it provides the core building blocks (loop, session, compaction, skills, harness) that a consuming application (like the Pi IDE) composes into a full agent experience.

Compared to Meept, which is a **full-stack autonomous daemon** with its own CLI, daemon process, message bus, multi-agent routing, security engine, and memory subsystem, Pi Agent is a focused, embeddable library with no opinions about transport, storage backend, or deployment model. The comparison surface is therefore limited to the agent loop, context management, session handling, tool execution, and skills -- the areas where both systems solve the same problems.

---

## 2. Pi Agent Architecture

### 2.1 Layered Design

```
Application Layer  (Pi IDE, etc.)
        │
   AgentHarness    ← session persistence, resources, hooks, queues, phase management
        │
      Agent        ← stateful wrapper: transcript, lifecycle, abort, queueing
        │
   agent-loop      ← pure-function double-nested loop, event-sourced
        │
   pi-ai           ← provider streaming (Anthropic, OpenAI, etc.)
```

### 2.2 Core Components

| Component | File(s) | Purpose |
|-----------|---------|---------|
| `agent-loop.ts` | Core loop | Pure-function LLM+tool iteration with event emission |
| `agent.ts` | Agent class | Stateful wrapper with transcript, queues, abort |
| `agent-harness.ts` | Harness | Session persistence, resources, hooks, compaction, branching |
| `session/` | Session tree | Append-only tree-structured conversation persistence (JSONL) |
| `compaction/` | Compaction | LLM-based context window summarization with file tracking |
| `skills.ts` | Skills | Filesystem-based SKILL.md discovery with YAML frontmatter |
| `proxy.ts` | Proxy | Bandwidth-optimized server-mediated LLM streaming |
| `execution-env.ts` | Env | Abstract filesystem/shell interface (Node.js implementation) |

---

## 3. Feature-by-Feature Analysis

### 3.1 Agent Loop

**Pi Agent:**
- Double-nested while loop: inner loop handles tool-call/steering continuation, outer loop handles follow-up messages
- Pure-function design: the loop communicates entirely through events, with no shared mutable state
- Two-phase tool pipeline: prepare (validate + beforeToolCall hook) then execute
- Sequential or parallel tool execution (configurable globally and per-tool)
- Unanimous termination: tool batch only stops when every result sets `terminate: true`
- Steering messages: inject mid-run after current tool batch completes
- Follow-up messages: queue for after the agent would otherwise stop
- `shouldStopAfterTurn` hook for graceful exit before next LLM call
- `prepareNextTurn` hook for context/model/thinking swap between turns

**Meept:**
- Single iterative loop in `reasoningCycle()` with max 25 iterations
- Stateful design: loop owns a `Conversation` object with embedded truncation logic
- Tool execution via `Executor` with security checks, caching, and adaptive compression
- Parallel tool execution with semaphore-based concurrency (default 4)
- Cycle detection: 3 consecutive identical tool calls triggers abort
- Convergence detection: 3 consecutive identical text responses triggers abort
- Hallucination detection: analyzes LLM output for fabricated claims
- Warning zone: at 80% of budget, tools are withheld to force wrap-up
- Budget tracking: total conversation (50K), per-iteration (30K), per-turn (10 turns)

### 3.2 Context / Context Window Management

**Pi Agent:**
- **Compaction** is the primary mechanism: when `contextTokens > contextWindow - reserveTokens` (default reserve: 16,384), older messages are summarized via a dedicated LLM call
- **Cut point algorithm**: walks backwards from newest messages, keeps ~20K most recent tokens, finds valid cut boundary (never cuts at a tool result)
- **Iterative summarization**: when a previous compaction exists, uses an update prompt that merges old summary with new progress
- **Split-turn handling**: if the cut lands mid-turn, generates two parallel summaries (history + turn prefix) and merges them
- **File operation tracking**: cumulative sets of read/written/edited files across compactions, appended as XML tags to summaries
- **Serialized summarization**: conversations are flattened to text before sending to summarization LLM to prevent continuation
- `transformContext` hook allows arbitrary message-level pruning before LLM calls

**Meept:**
- **ContextFirewall** wraps the LLM client with budget ratios: 30% iteration, 50% conversation history
- **Multiple truncation strategies**: by message count (LRU, max 200), by tokens, by importance (4-level priority: Critical > High > Medium > Low)
- **Hierarchical summarization**: recursive re-summarization of summaries exceeding threshold
- **Adaptive tool result compression**: dynamic budget shrinks from 3K to 600 tokens as overall budget depletes
- **AST-aware compression**: for code content, uses tree-sitter to preserve function signatures and type definitions
- **Anchor messages**: validation instructions that survive all truncation
- **Windowed messages**: smart context selection preserving system prompt, original user message, anchors, and recent messages
- **Warning zone**: at 80% budget, removes tools from context to force text-only wrap-up

### 3.3 Session Persistence

**Pi Agent:**
- **Tree-structured sessions**: append-only tree with parent pointers, enabling conversation branching
- **JSONL storage**: one JSON object per line, append-only writes, no truncation or rewriting
- **Full history preservation**: compaction only affects context assembly, never deletes stored data
- **Branch navigation**: users can navigate to any point in the tree, with automatic branch summarization
- **Session forking**: copy entries to a new session with parent reference
- **In-memory storage**: alternative backend for testing/ephemeral sessions
- **Session metadata**: CWD, timestamps, parent session tracking
- **Directory organization**: sessions grouped by working directory (encoded in directory name)

**Meept:**
- **Conversation store**: in-memory LRU cache (max 100 conversations) with no persistent session storage
- **SessionTracker**: tracks intent history (last 20), metrics, and timestamps per session
- **Background persistence**: hourly goroutine persists idle sessions to memvid as summaries
- **No branching**: conversations are linear, no tree structure or branch navigation
- **No session resumption**: conversations live only in memory for the daemon's lifetime

### 3.4 Tool Execution

**Pi Agent:**
- **4-phase pipeline**: prepare → execute → finalize → emit
- **beforeToolCall hook**: can block execution with `{ block: true, reason }`
- **afterToolCall hook**: can override content, details, isError, terminate fields
- **Parallel mode**: all tools prepared sequentially, then executed concurrently via Promise.all; results emitted in source order
- **Sequential mode**: each tool goes through all 4 phases before next begins
- **Per-tool override**: individual tools can force sequential execution
- **Tool streaming**: tools can emit progress updates via `onUpdate` callback
- **terminate hint**: tools can signal the loop should skip the follow-up LLM call

**Meept:**
- **Security-gated execution**: `PermissionChecker` maps tool names to action categories before execution
- **Fail-closed policy**: if no security configured, only safe introspection tools allowed
- **Result caching**: `ResultCache` stores tool results for deduplication
- **Parallel execution**: semaphore-based (default concurrency 4)
- **Adaptive compression**: tool results compressed based on remaining budget
- **AST-aware compression**: code results preserve structural elements via tree-sitter
- **Evidence extraction**: tools can return structured evidence for validation
- **Filtered registries**: per-agent tool filtering via `FilteredToolRegistry`

### 3.5 Skills System

**Pi Agent:**
- **SKILL.md convention**: skills defined by `SKILL.md` files with YAML frontmatter
- **Directory traversal**: two-pass scan (SKILL.md first, then root .md files)
- **Ignore file support**: honors .gitignore, .ignore, .fdignore
- **Validation**: name matching, length limits, description requirements
- **System prompt injection**: skills formatted as XML block in system prompt
- **On-demand loading**: skill content loaded when invoked, not at discovery time
- **disableModelInvocation**: flag to hide skills from model while allowing app invocation
- **Prompt templates**: separate system for parameterized .md templates with argument substitution ($1, $@, ${@:N:L})

**Meept:**
- **Three-tier discovery**: project-local > user-global > system-wide, with ClawSkills for third-party
- **YAML frontmatter**: skills declare `requires` (capabilities) and `allowed-tools`
- **Capability index**: metadata-driven matching without body loading
- **Lazy skill loading**: `LazySkillLoader` loads body only when skill is selected
- **Model resolution**: skills influence model selection via `Resolver.ResolveForSkill()`
- **Tool filtering**: skills with `AllowedTools` temporarily filter the tool registry
- **MaxSkillContextTokens**: skill context capped at 4,000 tokens
- **ClawSkills marketplace**: third-party skill registry with search/install

### 3.6 Security

**Pi Agent:**
- **beforeToolCall hook**: blocks individual tool calls with reason
- **ExecutionEnv abstraction**: filesystem operations go through interface, allowing sandboxed implementations
- No built-in input/output scanning, prompt injection detection, or audit logging

**Meept:**
- **InputSanitizer**: prompt injection pattern detection on all user input
- **SecurityEngine**: SQLite-backed permission checks with tool/action category mapping
- **Tirith**: pre-execution shell command scanning
- **Output scanning**: credential leak detection on LLM output
- **Fail-closed policy**: blocks everything except safe introspection tools when unconfigured
- **Audit logging**: all security decisions logged to SQLite
- **Evidence requirements**: system prompt enforces verifiable evidence for claims

### 3.7 Error Handling & Resilience

**Pi Agent:**
- **"Must not throw" contracts**: all hooks/callbacks have explicit contracts preventing loop corruption
- **Immediate error outcomes**: tool prep failures produce error results without aborting the batch
- **afterToolCall hook error handling**: hook throws are converted to error tool results, not batch aborts
- **Provider error handling**: `stopReason: "error"` or `"aborted"` causes graceful loop termination
- **No retry at loop level**: retries are delegated to the provider layer (pi-ai)
- **Single-active-run guard**: concurrent `prompt()` calls throw

**Meept:**
- **chatWithFailover**: up to 5 LLM call attempts with model rotation on rate limits
- **Exponential backoff**: base 2s, max 30s between retries
- **Cycle detection**: 3 identical consecutive tool calls → abort
- **Convergence detection**: 3 identical consecutive text responses → abort
- **Hallucination detection**: correction prompt injection for fabricated claims
- **Empty response nudging**: re-prompts on empty LLM output
- **Escalation manager**: failed tasks can trigger re-planning
- **Validation loops**: step results validated up to MaxValidationLoops times

### 3.8 Event System

**Pi Agent:**
- **Rich discriminated-union events**: 20+ event types covering lifecycle, turns, messages, tool execution, session, configuration
- **Type-safe hook results**: each hook event maps to its return type
- **Sequential listener invocation**: subscribers called in registration order, awaited
- **Settlement semantics**: `agent_end` listeners must settle before `waitForIdle()` resolves
- **Dual event model**: subscribers (fire-and-forget) + hooks (return values to influence behavior)

**Meept:**
- **Message bus (pub/sub)**: decoupled event-driven architecture via `internal/bus/`
- **Domain events**: task.planned, task.completed, task.failed, orchestrator.schedule, etc.
- **Progress publishing**: tool execution progress published to chat channel
- **Episodic memory recording**: interactions asynchronously recorded to memvid

---

## 4. Comparative Feature Matrix

### 4.1 Core Agent Loop

| Feature | Pi Agent | Meept | Notes |
|---------|:--------:|:-----:|-------|
| Iterative LLM+tool loop | **9** | **8** | Both solid; Pi's pure-function design is cleaner, Meept's is more feature-rich |
| Tool call pipeline | **9** | **8** | Pi's 4-phase pipeline with hooks is well-factored; Meept adds security gating |
| Sequential tool execution | **9** | **8** | Both support it; Pi has per-tool override |
| Parallel tool execution | **9** | **8** | Pi uses Promise.all; Meept uses semaphore with configurable concurrency |
| Steering (mid-run injection) | **9** | **3** | Pi has first-class steer/followUp queues; Meept has no equivalent |
| Follow-up queuing | **9** | **3** | Pi queues messages for after agent stops; Meept has no equivalent |
| Error resilience | **7** | **9** | Meept has retry, failover, cycle/convergence/hallucination detection |
| Budget management | **7** | **9** | Meept has conversation budget, iteration budget, turn budget, warning zone |

### 4.2 Context Management

| Feature | Pi Agent | Meept | Notes |
|---------|:--------:|:-----:|-------|
| LLM-based summarization | **10** | **7** | Pi's compaction is thorough: iterative updates, split-turn, file tracking |
| Token estimation | **7** | **8** | Pi uses chars/4 heuristic; Meept uses actual LLM usage + 3 chars/token |
| Truncation strategies | **6** | **9** | Meept has count-based, token-based, importance-based, windowed |
| AST-aware compression | **0** | **9** | Meept only; tree-sitter-based code structure preservation |
| Hierarchical summarization | **0** | **8** | Meept only; recursive re-summarization of old summaries |
| Adaptive compression | **0** | **9** | Meept only; shrinking tool result budgets as context depletes |
| Anchor preservation | **0** | **8** | Meept only; validation anchors survive all truncation |

### 4.3 Session Persistence

| Feature | Pi Agent | Meept | Notes |
|---------|:--------:|:-----:|-------|
| Tree-structured sessions | **10** | **0** | Pi only; enables branching and navigation |
| Branch navigation | **9** | **0** | Pi only; navigate to any prior point with auto-summarization |
| Session forking | **8** | **0** | Pi only; copy entries to new session |
| Append-only persistence | **9** | **0** | Pi only; no data ever deleted, full history recoverable |
| JSONL storage format | **8** | **0** | Pi only; human-readable, append-friendly |
| In-memory conversation store | **6** | **8** | Pi has in-memory option for testing; Meept's LRU is production-grade |
| Session resume from disk | **9** | **0** | Pi only; full session reconstruction from JSONL |

### 4.4 Skills

| Feature | Pi Agent | Meept | Notes |
|---------|:--------:|:-----:|-------|
| SKILL.md convention | **8** | **7** | Both use YAML frontmatter; Pi's is more structured |
| Multi-tier discovery | **6** | **9** | Meept has 4 tiers + ClawSkills marketplace |
| Lazy loading | **7** | **8** | Both lazy-load; Meept adds capability-based filtering |
| Tool filtering per skill | **7** | **8** | Both support it; Meept integrates with model resolution |
| Prompt templates | **8** | **0** | Pi only; parameterized templates with arg substitution |
| Third-party marketplace | **0** | **9** | Meept only; ClawSkills with search/install/security |

### 4.5 Security

| Feature | Pi Agent | Meept | Notes |
|---------|:--------:|:-----:|-------|
| Tool call blocking | **8** | **9** | Pi's beforeToolCall hook; Meept has full SecurityEngine |
| Input sanitization | **0** | **9** | Meept only; prompt injection pattern detection |
| Output scanning | **0** | **8** | Meept only; credential leak detection |
| Shell command scanning | **0** | **8** | Meept only; Tirith pre-execution scanner |
| Audit logging | **0** | **8** | Meept only; SQLite-backed security audit trail |
| Fail-closed policy | **0** | **9** | Meept only; blocks all but safe tools when unconfigured |

### 4.6 Event & Hook System

| Feature | Pi Agent | Meept | Notes |
|---------|:--------:|:-----:|-------|
| Typed events | **9** | **6** | Pi has 20+ typed events; Meept uses bus topics |
| Hook pipeline | **9** | **4** | Pi has type-safe hooks with return values; Meept hooks are informal |
| Event settlement | **8** | **6** | Pi's waitForIdle awaits listener settlement |
| Pub/sub architecture | **6** | **9** | Meept's message bus is more flexible for system-wide events |

---

## 5. Comparative Score Summary

Scores are relative to each other on a 1-10 scale, where 10 = best-in-class implementation.

### 5.1 Overall Category Scores

```
Category                    Pi Agent    Meept
─────────────────────────────────────────────
Agent Loop Core             8.5         7.5
Context Management          7.0         8.5
Session Persistence         9.0         2.5
Tool Execution              8.5         8.0
Skills System               7.5         8.0
Security                    3.0         9.0
Event System                8.5         6.5
Error Resilience            7.0         8.5
─────────────────────────────────────────────
Overall Average             7.4         7.3
```

### 5.2 Radar Chart (ASCII)

```
                    Agent Loop
                      9.0
                       |
                       |
           Security  --+--  Context Mgmt
            3.0/9.0    |    7.0/8.5
                       |
                      7.0
                      / \
                    /     \
                  /         \
  Error Resil.  /             \  Session
    7.0/8.5   /               \  9.0/2.5
             |                 |
             |                 |
            Events            Skills
           8.5/6.5           7.5/8.0
             |                 |
              \               /
               \             /
                \           /
                 \         /
                  \       /
                   \     /
                    \   /
                     \ /
                      +
                   Tools
                  8.5/8.0

                Pi Agent / Meept
```

### 5.3 Strength Distribution

| Pi Agent Excels At | Meept Excels At |
|--------------------|-----------------|
| Session persistence & branching | Security defense-in-depth |
| Clean separation of concerns | Context management variety |
| Pure-function loop design | Error resilience & retry |
| Steering/follow-up queues | Adaptive compression |
| Branch summarization | Hallucination detection |
| Hook/type system design | Multi-agent orchestration |
| Proxy streaming | Tool result AST compression |
| Prompt templates | Third-party skill marketplace |

---

## 6. Key Architectural Differences

### 6.1 Library vs. Application

**Pi Agent** is a library. It has no CLI, no daemon, no message bus, no transport layer opinions. It exports a clean TypeScript API that a consuming application (the Pi IDE) wraps. This gives it maximum composability and testability.

**Meept** is an application. It is a compiled Go binary with a daemon process, Unix socket RPC, HTTP API, CLI, TUI, Telegram bot, and macOS menu bar app. The agent loop is deeply integrated with security, memory, planning, and multi-agent routing.

This difference is by far the most significant architectural distinction and explains why Pi Agent has no equivalent for most of Meept's infrastructure (message bus, daemon lifecycle, scheduler, calendar, etc.).

### 6.2 Stateful vs. Stateless Loop Core

Pi's `agent-loop.ts` is a **pure function**. It takes messages and config, emits events, and returns. All state lives in the caller. This makes it trivially testable and composable.

Meept's `AgentLoop.RunOnce()` is a **method on a struct** with dozens of optional subsystems. This gives it rich behavior out of the box (security, memory, shadow training, hallucination detection) but makes it harder to test in isolation.

### 6.3 Session Model

Pi's sessions are **append-only trees** with parent pointers. This enables:
- Branching: navigate to any prior message, fork the conversation
- Full history: no data ever deleted, compaction only affects context assembly
- Session forking: copy entries to a new session

Meept's conversations are **in-memory linear arrays** with LRU eviction. This means:
- No branching or history navigation
- No persistent session resumption across daemon restarts
- Simpler implementation but data loss on daemon exit

### 6.4 Compaction vs. Multi-Strategy Truncation

Pi has one strategy: **LLM-based compaction** with iterative summarization, file tracking, and split-turn handling. It is thorough and well-engineered.

Meept has **five strategies**: count-based truncation, token-based truncation, importance-based truncation, windowed message selection, and hierarchical summarization. Plus AST-aware compression for code content and adaptive shrinking of tool result budgets.

Pi's single strategy is more polished. Meept's multiple strategies provide more knobs for tuning at the cost of complexity.

### 6.5 Error Handling Philosophy

Pi's philosophy: **hooks must not throw**. Errors are converted to error tool results. The loop never breaks. Retry is delegated to the provider layer.

Meept's philosophy: **detect and recover**. The loop has explicit handling for rate limits, timeouts, cycle detection, convergence detection, hallucination detection, and empty responses. Failed tasks can be re-planned by the escalation manager.

---

## 7. Feature Gap Analysis (Where Meept Can Learn from Pi)

### 7.1 High-Value Gaps

| Gap | Impact | Complexity | Notes |
|-----|--------|------------|-------|
| Session persistence | High | Medium | JSONL append-only storage with tree structure |
| Conversation branching | High | High | Tree-structured sessions enable undo/explore workflows |
| Steering/follow-up queues | High | Low | Mid-run message injection is powerful for interactive use |
| LLM-based compaction | Medium | Medium | Iterative summarization with file tracking is more thorough than truncation |
| Prompt templates | Medium | Low | Parameterized templates with arg substitution |
| Tool streaming updates | Medium | Low | Tools can emit progress updates during execution |
| Hook pipeline | Medium | Medium | Type-safe hooks with return value influence on behavior |

### 7.2 Low-Value Gaps (Meept Already Covers)

| Feature | Pi | Meept Equivalent |
|---------|-----|-------------------|
| Parallel tool execution | Promise.all | Semaphore (concurrency 4) |
| beforeToolCall blocking | Hook | SecurityEngine permission check |
| Dynamic model switching | setModel | Model alias resolution with failover |
| Dynamic API key | getApiKey | LLM client config |
| Session ID for caching | sessionId | N/A (different caching strategy) |

---

## 8. Feature Gap Analysis (Where Pi Can Learn from Meept)

| Gap | Impact | Notes |
|-----|--------|-------|
| Security defense-in-depth | High | Input sanitization, output scanning, shell command scanning, audit logging |
| Hallucination detection | Medium | Analyzes LLM output for fabricated claims |
| Cycle/convergence detection | Medium | Prevents infinite loops and stuck states |
| AST-aware compression | Medium | Tree-sitter-based code structure preservation in tool results |
| Adaptive result compression | Medium | Dynamic shrinking of tool result budgets |
| Importance-based truncation | Medium | 4-level priority system for message retention |
| Multi-agent orchestration | High | Planner, dispatcher, specialist agents, validation gates |
| Third-party skill marketplace | Medium | ClawSkills search/install workflow |

---

## 9. File Structure Comparison

```
Pi Agent (~25 files)                     Meept Agent (~30+ files)
──────────────────────────               ──────────────────────────────
src/                                     internal/agent/
  agent.ts            (Agent class)        loop.go            (AgentLoop struct)
  agent-loop.ts       (pure loop)          loop.go            (reasoningCycle)
  types.ts            (type definitions)   spec.go            (agent specs)
  proxy.ts            (stream proxy)       dispatcher.go      (intake routing)
  index.ts            (barrel export)      registry.go        (agent registry)
  harness/                                executor.go        (tool execution)
    agent-harness.ts  (orchestrator)       conversation.go    (chat history)
    types.ts          (harness types)      session_tracker.go (session tracking)
    messages.ts       (message types)      strategic.go       (planner)
    system-prompt.ts  (prompt builder)     tactical.go        (scheduler)
    skills.ts         (skill loading)      collaborative.go   (review workflow)
    prompt-templates.ts (templates)        workspace.go       (task tracking)
    execution-env.ts  (barrel)             cache.go           (response cache)
    env/                                  report.go          (reporting)
      nodejs.ts        (Node impl)       prompt/
    session/                                loader.go        (prompt loading)
      session.ts       (tree API)       internal/security/
      storage/                              engine.go        (permissions)
        jsonl.ts        (JSONL store)       sanitizer.go     (input sanitization)
        memory.ts       (in-memory)         tirith.go        (command scanning)
      repo/                                audit.go         (audit logging)
        jsonl.ts        (JSONL repo)
        memory.ts       (in-memory repo)  internal/skills/
        shared.ts       (utilities)         registry.go      (skill lookup)
    compaction/                            executor.go      (skill execution)
      compaction.ts     (compaction)      internal/memory/
      branch-summarization.ts              manager.go       (memory system)
      utils.ts          (shared utils)     internal/tools/
    utils/                                 registry.go      (tool registry)
      shell-output.ts   (shell capture)     builtin/         (30+ built-in tools)
      truncate.ts       (truncation)
```

---

## 10. Summary of Key Takeaways

1. **Pi's session system is its crown jewel.** The append-only tree-structured JSONL storage with branching, navigation, and forking is sophisticated and well-engineered. Meept has no equivalent -- conversations are ephemeral in-memory arrays.

2. **Pi's steering/follow-up queues are unique.** The ability to inject messages mid-run (steering) or queue them for after completion (follow-up) with configurable drain modes is a first-class feature Meept lacks entirely.

3. **Meept's security is far more mature.** Defense-in-depth with input sanitization, output scanning, command scanning, audit logging, and fail-closed policies. Pi has only a beforeToolCall hook.

4. **Meept's context management has more strategies.** Five truncation strategies vs. Pi's one compaction strategy. However, Pi's single strategy is more polished (iterative updates, split-turn handling, file tracking).

5. **Pi is a library; Meept is an application.** This is the fundamental difference. Pi provides building blocks; Meept provides a running system. The comparison only makes sense for the shared concern of agent loop orchestration.

6. **Pi's pure-function loop is cleaner but less resilient.** No retry, no cycle detection, no hallucination detection. Meept trades purity for robustness.

7. **Pi's type system is exemplary.** Discriminated unions, declaration merging, type-safe hooks, and exhaustive pattern matching make the codebase highly maintainable. Go's type system can't match this expressiveness.

8. **Both have solid tool execution.** Parallel and sequential modes, hook pipelines, and error handling. The differences are in the surrounding infrastructure (Meept adds security gating and caching; Pi adds tool streaming updates and terminate hints).
