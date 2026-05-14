# Agent Loop, Models, and Tool Architecture

> **Related:** `internal/agent/`, `internal/llm/resolver.go`, `config/models.json5`, `config/agents.json5`

## Top-Level Request Flow

```
User Input (CLI / TUI / HTTP / Telegram)
        |
        v
  MessageBus ("chat.request")
        |
        v
  +--------------------+
  |   ChatHandler      |  internal/agent/handler.go
  |   (handler.go)     |  Bridges bus -> agent loop
  +--------+-----------+
           |
     has dispatcher?
           |
    +------+------+
    |             |
   no            yes
    |             |
  direct        +--v-----------+
  AgentLoop     | Dispatcher   |  internal/agent/dispatcher.go
  (chat)        | (dispatcher) |  Classifies intent, routes to specialist
                +--+-----------+
                   |
                   | LLMClassifier (uses classifier_model / small_model)
                   |   classifies user message -> IntentType
                   |   confidence scoring + threshold check
                   |
            +------+------+
            |             |
        inline          defer
        (handle         (create task,
         directly)       dispatch to agent)
            |             |
            |      +------v------+
            |      | AgentLoop   |  internal/agent/loop.go
            |      | (specialist)|
            |      +------+------+
            |             |
            |       +-----+-----+
            |       |           |
            |    Executor   ReviewManager
            |    (tools)    (optional review)
            |       |           |
            |       v           v
            |   Tool Registry  Reviewer Agent
            |
            v
      same AgentLoop path
```

## Agents

### Executor Agents (do work)

```
+----------------------------------------------------------+
| ID           | Extra Tools (beyond baseline)              |
|--------------|--------------------------------------------|
| chat         | web_fetch, web_search                      |
| coder        | file_read, file_write, file_delete,        |
|              | list_directory, shell_execute               |
| debugger     | file_read, file_write, shell_execute       |
| planner      | (baseline only)                            |
| analyst      | web_fetch, web_search                      |
| researcher   | web_fetch, file_read, list_directory,      |
|              | shell_execute                              |
| committer    | shell_execute, file_read, list_directory   |
| scheduler    | schedule_create, schedule_list,            |
|              | schedule_delete                            |
+----------------------------------------------------------+
```

### Dispatcher (intake + routing)

```
+----------------------------------------------------------+
| ID           | Extra Tools                                |
|--------------|--------------------------------------------|
| dispatcher   | (baseline only -- uses delegate_task       |
|              |  from baseline to route)                   |
+----------------------------------------------------------+
```

### Reviewer Agents (validate work)

```
+----------------------------------------------------------+
| ID               | Extra Tools                            |
|------------------|----------------------------------------|
| code-reviewer    | file_read, memory_search               |
| test-reviewer    | shell_execute, file_read               |
| debug-reviewer   | file_read, memory_search               |
| analyst-reviewer | web_search, web_fetch, memory_search   |
| planner-reviewer | memory_search                          |
+----------------------------------------------------------+
```

### Meta-Agents (optimize the platform)

```
+----------------------------------------------------------+
| ID           | Purpose                                   |
|--------------|-------------------------------------------|
| q            | Quartermaster: analyzes session           |
|              | transcripts, detects recurring patterns,  |
|              | suggests new agents or config changes     |
+----------------------------------------------------------+
```

### Baseline Tools (all agents)

```
+----------------------------------------------------------+
| Category    | Tools                                      |
|-------------|--------------------------------------------|
| Memory      | memory_store, memory_search,               |
|             | memory_get_context                         |
| Tasks       | task_create, task_get, task_list,          |
|             | task_update                                |
| Platform    | platform_status, platform_agents,          |
|             | platform_tools                             |
| Delegation  | delegate_task                              |
+----------------------------------------------------------+
```

All agents receive these 10 tools automatically via `BaselineTools` in `internal/agent/spec.go`. Additional tools are layered on per-agent by the `AgentRegistry.filterTools()` method.

## Intent Routing

The `LLMClassifier` (`internal/agent/llm_classifier.go`) uses the `classifier_model` (typically the `small_model`) to classify user messages into intent types. The `Dispatcher` then routes based on the result:

```
+------------------+---------------------------+----------+
| Intent           | Route To                  | Priority |
|------------------|---------------------------|----------|
| chat             | chat                      | follow   |
| code, review     | coder                     | steer    |
| debug            | debugger                  | steer    |
| plan             | planner                   | steer    |
| analyze, search  | analyst                   | follow   |
| research         | analyst                   | follow   |
| git              | committer                 | steer    |
| schedule         | scheduler                 | follow   |
| security         | chat                      | steer    |
| tooluse          | coder                     | steer    |
| skill            | skill executor            | follow   |
| compound         | orchestrator (decompose)  | follow   |
+------------------+---------------------------+----------+

steer  = interrupt current agent, redirect immediately
follow = queue as follow-up, wait for natural break
```

Steering heuristics are defined in `SteeringHeuristicTable` (`internal/agent/dispatcher.go`). Users can force steering with ctrl+s.

## Models

### Provider Layout

```
+----------------------------------------------------------+
| Provider     | Model             | Capabilities           |
|--------------|-------------------|------------------------|
| zai          | glm-4.7           | completion, code,      |
|              |                   | reasoning, tool_use    |
|              |                   | ctx: 128k              |
|              |-------------------|------------------------|
|              | glm-4.5-air       | completion, code,      |
|              |                   | reasoning              |
|              |                   | ctx: 32k               |
|--------------|-------------------|------------------------|
| ollama       | llama3.2          | code, tool_use,        |
| (localhost)  |                   | reasoning, ctx: 128k   |
|              |-------------------|------------------------|
|              | qwen2.5-coder     | code, tool_use,        |
|              |                   | ctx: 32k               |
+----------------------------------------------------------+
```

### Model Resolution

```
                    AgentSpec.Model
                         |
                    is it empty?
                    /          \
                  yes           no
                   |             |
              default model   is it an alias?
              (zai/glm-4.7)   /           \
                             yes           no
                              |             |
                         ResolveForAlias  ResolveModelRef
                         (failover)       (direct ref)
                              |
                         +----+----+
                         |         |
                    primary    fallback
                    model      model
                    (index 0)  (index 1..)
                         |
                    if fail, rotate
                    to next + cooldown
```

### Model Aliases

```
+----------------------------------------------------------+
| Alias    | Failover Order             | Used By          |
|----------|----------------------------|------------------|
| coder    | zai/glm-4.7                | coder agent      |
|          |  -> ollama/llama3.2        | (when overridden)|
|----------|----------------------------|------------------|
| planner  | zai/glm-4.5-air            | planner agent    |
|          |  -> ollama/llama3.2        | (when overridden)|
|----------|----------------------------|------------------|
| analyst  | zai/glm-4.5-air            | analyst agent    |
|          |  -> ollama/qwen2.5-coder   | (when overridden)|
+----------------------------------------------------------+
```

Aliases are configured in `config/models.json5` under `model_aliases`. When an agent's `Model` field matches an alias name, the resolver uses failover rotation with cooldown-based backoff (`RecordAliasFailure` / `RecordAliasSuccess`).

All default agent specs in `internal/agent/spec.go` set `Model: ""`, which means they use the default model (`zai/glm-4.7`). The `config/agents.json5` file can override this per-agent.

### Classification Model

Intent classification uses the `classifier_model` (falls back to `small_model`), which defaults to `zai/glm-4.5-air` -- a lighter, cheaper model suitable for short classification prompts.

## Agent Loop Internals

```
AgentLoop.RunOnce(ctx, message, conversationID)
        |
        v
  getOrCreateConversation(id)
        |
   in-memory hit? ---yes--> use cached Conversation
        |
   no (miss)
        |
   session persistence on?
        |
   yes --> RestoreConversationFromStore()
        |     (SQLite -> AssembleBranch -> Conversation)
        |
   no  --> create empty Conversation
        |
        v
  AddUserMessage(message)
        |
        v
  reasoningCycle(ctx, conv, id)
        |
        v
  +------ loop (max_iterations) ------+
  |                                    |
  |  GetWindowedMessages(budget)       |
  |       |                            |
  |  build tool list for this agent    |
  |       |                            |
  |  LLM.Chat(messages, tools)         |
  |       |                            |
  |  response has tool_calls?          |
  |    |            |                  |
  |   yes          no                  |
  |    |            |                  |
  |  execute     return response       |
  |  tools          |                  |
  |    |            |                  |
  |  add tool   <--+                  |
  |  results                         |
  |    |                              |
  |  terminate signal?                |
  |    |           |                  |
  |   yes         no ---> continue    |
  |    |                              |
  |  return tool results directly     |
  |    |                              |
  +----+------------------------------+
        |
        v
  persistConversation(id)
        |   delta persist: only new messages -> SQLite
        |   chains parent_id pointers
        |   persists tool_calls to session_tool_calls
        |
        v
  maybeCompact(id)
        |   compaction enabled?
        |     GetCompactionCandidates()
        |     InsertCompaction() -> SQLite
        |     ReparentAfterCompaction() -> SQLite tree
        |     RemoveCompactedMessages() -> in-memory
        |
        v
  return finalResponse
```

### Token Budget Management

Two modes controlled by `session.Compaction` / `session.LegacyTruncation`:

```
Legacy mode (Compaction=false or LegacyTruncation=true):
  TruncateByTokens(budget) -> silently deletes old messages
  GetWindowedMessages(budget) -> windows remaining for LLM

Compaction mode (Compaction=true, LegacyTruncation=false):
  [skip TruncateByTokens entirely]
  GetWindowedMessages(budget) -> windows messages for LLM (no deletion)
  maybeCompact() -> [after turn] emits compaction entries to SQLite
```

## Tool Access Matrix

```
Tool                    chat  coder  debug  plan  analyst  researcher  commit  sched  disp  reviewers
-----------------------+-----+------+-------+-----+--------+-----------+-------+------+-----+----------
file_read               |     |  X   |  X    |     |        |     X     |   X   |      |     |   X
file_write              |     |  X   |  X    |     |        |           |       |      |     |
file_delete             |     |  X   |       |     |        |           |       |      |     |
list_directory          |     |  X   |       |     |        |     X     |   X   |      |     |
shell_execute           |     |  X   |  X    |     |        |     X     |   X   |      |     |   X
web_fetch               |  X  |      |       |     |   X    |     X     |       |      |     |   X
web_search              |  X  |      |       |     |   X    |           |       |      |     |   X
schedule_create         |     |      |       |     |        |           |       |  X   |     |
schedule_list           |     |      |       |     |        |           |       |  X   |     |
schedule_delete         |     |      |       |     |        |           |       |  X   |     |
memory_search (baseline)|  X  |  X   |  X    |  X  |   X    |     X     |   X   |  X   |  X  |   X
memory_store (baseline) |  X  |  X   |  X    |  X  |   X    |     X     |   X   |  X   |  X  |
memory_get_ctx(baseline)|  X  |  X   |  X    |  X  |   X    |     X     |   X   |  X   |  X  |
task_create (baseline)  |  X  |  X   |  X    |  X  |   X    |     X     |   X   |  X   |  X  |
task_get (baseline)     |  X  |  X   |  X    |  X  |   X    |     X     |   X   |  X   |  X  |
task_list (baseline)    |  X  |  X   |  X    |  X  |   X    |     X     |   X   |  X   |  X  |
task_update (baseline)  |  X  |  X   |  X    |  X  |   X    |     X     |   X   |  X   |  X  |
platform_* (baseline)   |  X  |  X   |  X    |  X  |   X    |     X     |   X   |  X   |  X  |
delegate_task (baseline)|  X  |  X   |  X    |  X  |   X    |     X     |   X   |  X   |  X  |
```

## Review Flow

When enabled via `AgentConfig.ReviewEnabled`, the `ReviewManager` runs a reviewer agent after the executor finishes:

```
  Executor Agent finishes
        |
        v
  ReviewManager.ShouldReview(agentID)?
        |
       yes
        |
  +-----v-----------+
  | Lookup reviewer  |  config/schema.go ReviewerAgentMap:
  | for this agent   |    coder -> code-reviewer
  |                  |    debugger -> debug-reviewer
  |                  |    planner -> planner-reviewer
  |                  |    analyst -> analyst-reviewer
  +-----+-----------+
        |
        v
  Spawn reviewer AgentLoop
  (with conversation context + task summary)
        |
        v
  Reviewer returns:
    {status: approved|rejected|needs_info,
     feedback: "...",
     issues: [...],
     confidence: 0.0-1.0}
        |
        v
  approved -> task marked complete
  rejected -> task sent back to executor
  needs_info -> user prompted for input
```

## Q Agent (Quartermaster)

The Q Agent is a meta-agent that runs asynchronously to optimize the platform itself:

```
  Session completes (idle for session_idle_trigger_hours)
        |
        v
  QAgent.Analyze(sessions)
        |
        +--> PatternDetector: find recurring patterns
        |    (high error rates, long durations, frequent rejections)
        |
        +--> SessionAnalyzer: extract actionable insights
        |
        +--> ResearchEngine: search for solutions
        |
        +--> AgentDesigner: propose new agents or config changes
        |
        v
  Recommendations written to ~/.meept/q_analysis/
  Notifications sent via configured channels
```

Configuration: `~/.meept/q_agent.json5`
