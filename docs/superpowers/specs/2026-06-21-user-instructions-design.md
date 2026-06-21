# User Instructions: Natural Language Automation Layer

**Date:** 2026-06-21
**Status:** Design Spec
**Author:** Meept Architecture Session

---

## Problem

Users cannot persistently instruct Meept to "always do X" when certain conditions are met. Current capabilities:

| Capability | Agent-Triggered | User-Triggered |
|------------|-----------------|----------------|
| Store memory | ✅ `memory_store`, `retain` tools | ❌ No CLI/API command |
| Run scheduled jobs | ✅ `scheduler.Schedule()` | ❌ No natural language interface |
| Execute hooks | ✅ Git pre-commit hooks | ❌ No user configuration |
| Route by intent | ✅ Dispatcher classifies tasks | ❌ No `IntentInstruction` type |

**The gap:** Users must rely on agent initiative to store preferences or set up automation. There is no direct "instruct the platform" mechanism.

**Examples of unactionable user statements:**
- "Always run tests after I touch Go files"
- "Every morning, summarize my conversations"
- "When I commit, lint the code"
- "Remember my preference for using `glm-4.7` for synthesis"

---

## Goal

Enable users to instruct Meept (via natural language) to:
1. **Persist preferences** — "always do X in the future"
2. **Trigger actions** — cron, events, git hooks, intent classification
3. **Automate workflows** — chain tools/agents based on triggers
4. **Override defaults** — model choices, routing decisions, tool parameters

Using existing platform capabilities:
- Dispatcher intent classification
- Scheduler (cron jobs)
- Message bus event listeners
- Git hooks
- Memory store (for persistence + context injection)

---

## Design Decisions

### 1. Storage Format: Markdown + YAML Frontmatter

**Consistent with AGENT.md and skill files:**

```markdown
---
id: run-tests-after-go-changes
trigger: post_tool_complete:write_file:*.go
action: shell_execute
action_args:
  command: go test ./...
  timeout: 5m
enabled: true
scope: project  # project | global
created_at: 2026-06-21
priority: high
---

# Run Tests After Go File Changes

Always run `go test ./...` after any Go file is modified.
This ensures changes don't break existing functionality.
```

**Why Markdown:**
- Frontmatter → Go struct (`UserInstructionConfig`)
- Body → injected into agent context ("User has these standing instructions...")
- Human-readable and editable
- Consistent with `config/agents/*/AGENT.md`, `config/prompts/*.md`, skills

### 2. Tiered Storage (Same as Skills)

```
.meept/instructions/*.md           # Project-local (highest priority)
~/.meept/instructions/*.md         # User-global
~/.config/meept/instructions/*.md  # System-wide (lowest priority)
```

**Shadowing:** Higher-priority instructions override lower by `id`.

### 3. Trigger Types (All Use Existing Infrastructure)

| Trigger Type | Syntax | Existing System |
|-------------|--------|-----------------|
| **Cron** | `cron:0 0 9 * * *` | `internal/scheduler/` |
| **Post-hook (tool)** | `post_tool_complete:write_file` | Bus `tool.completed` topic |
| **Post-hook (task)** | `post_task_complete:coder` | Bus `agent.task.completed` topic |
| **Event-based** | `event:memory.stored` | Bus event listeners |
| **Intent-based** | `intent:IntentResearch` | Dispatcher routing |
| **Git pre-hook** | `git_pre_commit` | `.git/hooks/pre-commit-user` |
| **Git post-hook** | `git_post_commit` | `.git/hooks/post-commit-user` |

### 4. Action Types

| Action | Tool Mapping |
|--------|--------------|
| `shell_execute` | `builtin.ShellExecuteTool` |
| `memory_retain` | `builtin.RetainTool` |
| `agent_trigger` | Publish to `chat.request` bus topic |
| `http_request` | `builtin.HTTPRequestTool` (if exists) |
| `notification` | Bus → notification channel |

### 5. Security Gates (User Confirmation Required)

**Validation pipeline:**

```
1. Tool exists? → registry.Lookup(action)
2. User has permission? → SecurityEngine.Check()
3. High-risk? → Require explicit confirmation
   - File deletion
   - Shell commands (except known-safe)
   - Network operations
4. Save to disk + reload
```

**Known-safe commands (configurable):**
- `go test ./...`
- `go build ./...`
- `git status`
- `gofmt -w .`

---

## Architecture

### Component Diagram

```dot
digraph user_instructions {
    rankdir=TB;

    subgraph cluster_input {
        label="Input Layer";
        UserInput["User Input\n(natural language)"];
    }

    subgraph cluster_parse {
        label="Parsing Layer";
        Dispatcher["Dispatcher\n(IntentInstruction)"];
        Parser["InstructionParser\n(NL → structured rule)"];
    }

    subgraph cluster_store {
        label="Storage Layer";
        Validation["Validation +\nConfirmation"];
        Store["UserInstructionStore\n(~/.meept/instructions/)"];
    }

    subgraph cluster_exec {
        label="Execution Layer";
        Scheduler["Scheduler\n(cron jobs)"];
        BusListeners["Bus Listeners\n(post-hooks, events)"];
        GitHooks["Git Hooks\n(pre/post-commit)"];
        IntentRouter["Intent Router\n(intent-based)"];
    }

    UserInput -> Dispatcher;
    Dispatcher -> Parser;
    Parser -> Validation;
    Validation -> Store;
    Store -> Scheduler;
    Store -> BusListeners;
    Store -> GitHooks;
    Store -> IntentRouter;
}
```

### Data Flow

```
┌─────────────────────────────────────────────────────────────────┐
│  User: "Always run tests after I touch Go files"                 │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Dispatcher                                                      │
│  - Classifies as IntentInstruction (new intent type)            │
│  - Routes to InstructionHandler                                  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  InstructionParser                                               │
│  - Extracts trigger: "after I touch Go files"                    │
│    → pattern: post_tool_complete:write_file:*.go                │
│  - Extracts action: "run tests"                                  │
│    → tool: shell_execute(command="go test ./...")               │
│  - Extracts scope: current project                               │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Validation                                                      │
│  - Tool lookup: shell_execute exists ✅                          │
│  - Risk assessment: MEDIUM (shell command)                       │
│  - Confirm: "This will run shell commands. Continue? [y/N]"     │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Persistence                                                     │
│  - Generate ID: run-tests-after-go-changes                       │
│  - Write to: .meept/instructions/run-tests-after-go-changes.md  │
│  - Reload store + inject into context                            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Execution Wiring                                                │
│  - Register bus listener on tool.completed                       │
│  - Filter: tool=write_file, path=*.go                            │
│  - Action: call shell_execute with {command: "go test ./..."}   │
└─────────────────────────────────────────────────────────────────┘
```

---

## Go Code Changes

### 1. New Intent Type (`internal/agent/intent.go`)

```go
const (
    IntentCode       IntentType = "code"
    IntentDebug      IntentType = "debug"
    // ... existing intents ...
    IntentInstruction IntentType = "instruction"  // NEW
)
```

### 2. Instruction Parser (`internal/agent/instruction_parser.go`)

```go
package agent

// InstructionParser extracts structured rules from natural language.
type InstructionParser struct {
    logger *slog.Logger
}

// ParsedInstruction holds the extracted rule.
type ParsedInstruction struct {
    Trigger    TriggerConfig
    Action     ActionConfig
    Scope      string  // "project" | "global"
    Priority   string  // "low" | "normal" | "high" | "critical"
    RawInput   string
}

// TriggerConfig holds trigger configuration.
type TriggerConfig struct {
    Type       string  // "cron" | "post_hook" | "event" | "intent" | "git"
    Pattern    string  // e.g., "0 0 9 * * *" or "post_tool_complete:write_file"
    Conditions map[string]string
}

// ActionConfig holds action configuration.
type ActionConfig struct {
    Tool       string
    Args       map[string]any
    AgentID    string  // for agent_trigger actions
}

// Parse extracts a ParsedInstruction from user input.
func (p *InstructionParser) Parse(ctx context.Context, input string) (*ParsedInstruction, error)
```

### 3. User Instruction Store (`internal/preferences/store.go`)

```go
package preferences

// UserInstructionStore manages persisted user instructions.
type UserInstructionStore struct {
    tiers []string  // discovery paths (same as skills)
    instructions map[string]*UserInstruction
    mu sync.RWMutex
}

// UserInstruction matches the Markdown frontmatter schema.
type UserInstruction struct {
    ID          string
    Trigger     string
    Action      string
    ActionArgs  map[string]any
    Enabled     bool
    Scope       string
    Priority    string
    CreatedAt   time.Time
    Body        string  // Markdown body (injected into context)
}

// Discovery returns all discovered instructions (higher tiers shadow lower).
func (s *UserInstructionStore) Discovery() ([]*UserInstruction, error)

// Save persists an instruction to the appropriate tier.
func (s *UserInstructionStore) Save(instr *UserInstruction, tier string) error

// Delete removes an instruction by ID.
func (s *UserInstructionStore) Delete(id string) error

// GetActive returns enabled instructions for context injection.
func (s *UserInstructionStore) GetActive() []*UserInstruction
```

### 4. Instruction Handler (`internal/agent/instruction_handler.go`)

```go
package agent

// InstructionHandler bridges bus to InstructionStore.
type InstructionHandler struct {
    store *preferences.UserInstructionStore
    bus *bus.MessageBus
    parser *InstructionParser
    logger *slog.Logger
}

// Start subscribes to instruction.* topics.
func (h *InstructionHandler) Start(ctx context.Context) error

// handleAdd processes "add instruction" requests.
func (h *InstructionHandler) handleAdd(ctx context.Context, msg *BusMessage)

// handleList returns all active instructions.
func (h *InstructionHandler) handleList(ctx context.Context, msg *BusMessage)

// handleDelete removes an instruction.
func (h *InstructionHandler) handleDelete(ctx context.Context, msg *BusMessage)
```

### 5. Trigger Wiring

#### A. Cron Triggers (`internal/scheduler/instructions.go`)

```go
package scheduler

// InstructionScheduler wires user instructions to cron jobs.
type InstructionScheduler struct {
    scheduler *Scheduler
    store *preferences.UserInstructionStore
}

// SyncCronInstructions loads cron-type instructions as jobs.
func (s *InstructionScheduler) SyncCronInstructions() error {
    instructions := s.store.GetActive()
    for _, instr := range instructions {
        if !strings.HasPrefix(instr.Trigger, "cron:") {
            continue
        }
        schedule := strings.TrimPrefix(instr.Trigger, "cron:")

        // Create AgentJob or ShellJob based on action
        var job Job
        if instr.Action == "agent_trigger" {
            job = NewAgentJob(JobConfig{
                ID: "instruction_" + instr.ID,
                Name: "Instruction: " + instr.ID,
                Schedule: schedule,
                Type: JobTypeAgent,
                AgentConfig: &AgentJobConfig{
                    Prompt: instr.Body,
                    Model: instr.ActionArgs["model"].(string),
                },
            }, s.scheduler.Bus())
        }
        // ... handle other action types ...

        s.scheduler.Schedule(job)
    }
    return nil
}
```

#### B. Bus Listeners (`internal/agent/instruction_listeners.go`)

```go
package agent

// InstructionListener watches for trigger events.
type InstructionListener struct {
    store *preferences.UserInstructionStore
    bus *bus.MessageBus
    toolExecutor *tools.Registry
}

// Start subscribes to relevant bus topics.
func (l *InstructionListener) Start(ctx context.Context) error {
    // Post-hook listeners
    toolCompleteSub := l.bus.Subscribe("instruction-listener", "tool.completed")

    go func() {
        for msg := range toolCompleteSub.Channel {
            l.checkPostHookInstructions(msg)
        }
    }()

    return nil
}

func (l *InstructionListener) checkPostHookInstructions(msg *BusMessage) {
    instructions := l.store.GetActive()
    for _, instr := range instructions {
        if !strings.HasPrefix(instr.Trigger, "post_tool_complete:") {
            continue
        }

        // Parse trigger pattern: post_tool_complete:tool=path_pattern
        parts := strings.Split(strings.TrimPrefix(instr.Trigger, "post_tool_complete:"), ":")
        if len(parts) != 2 {
            continue
        }
        toolName := parts[0]
        pathPattern := parts[1]

        // Check if this message matches
        if msg.Payload["tool"] != toolName {
            continue
        }
        if pathPattern != "*" && !matchPattern(msg.Payload["path"].(string), pathPattern) {
            continue
        }

        // Execute action
        l.executeAction(instr)
    }
}
```

#### C. Git Hooks (`.git/hooks/pre-commit-user`)

```bash
#!/bin/bash
# Pre-commit hook for user instructions
# Generated by Meept when git_pre_commit instructions are saved

# Call Meept daemon to check for git_pre_commit instructions
meept rpc call instruction.execute_git_hook "{\"type\": \"pre_commit\"}"
```

#### D. Intent Router (`internal/agent/dispatcher.go`)

```go
func (d *Dispatcher) ClassifyAndRoute(ctx context.Context, input string) (*DispatchResult, error) {
    // Existing classification...

    // NEW: Check for IntentInstruction match
    if d.isInstructionInput(input) {
        return &DispatchResult{
            Intent:    IntentInstruction,
            AgentID:   d.config.InstructionAgent,  // Could be "dispatcher" or dedicated "orchestrator"
            Instruction: d.parser.Parse(ctx, input),
        }, nil
    }

    // Check for intent-based instruction matches
    for _, instr := range d.instructions.GetActive() {
        if strings.HasPrefix(instr.Trigger, "intent:") {
            triggerIntent := strings.TrimPrefix(instr.Trigger, "intent:")
            if d.classifyIntent(input) == IntentType(triggerIntent) {
                // Route normally, but attach instruction action
                result.Instructions = append(result.Instructions, instr)
            }
        }
    }

    return result, nil
}
```

### 6. Context Injection (`internal/agent/loop.go`)

```go
func (l *AgentLoop) buildSystemPrompt() string {
    var prompt strings.Builder

    // Base system prompt (from AGENT.md)
    prompt.WriteString(l.spec.Purpose)

    // NEW: Inject user instructions
    instructions := l.instructionStore.GetActive()
    if len(instructions) > 0 {
        prompt.WriteString("\n\n# User Instructions (Active)\n\n")
        prompt.WriteString("You have the following standing instructions from the user:\n\n")
        for i, instr := range instructions {
            prompt.WriteString(fmt.Sprintf("%d. **%s** (trigger: %s, action: %s)\n",
                i+1, instr.ID, instr.Trigger, instr.Action))
            if instr.Body != "" {
                prompt.WriteString(fmt.Sprintf("   %s\n", instr.Body))
            }
        }
        prompt.WriteString("\nWhen these triggers occur, execute the associated action automatically.\n")
    }

    return prompt.String()
}
```

---

## API Design

### CLI Commands

```bash
# List all active instructions
meept instructions list

# Add a new instruction
meept instructions add "Always run tests after I touch Go files"

# Delete an instruction by ID
meept instructions delete run-tests-after-go-changes

# Show instruction details
meept instructions show <id>

# Preview what an instruction would do (dry-run)
meept instructions preview "Every morning at 9am, summarize my conversations"
```

### RPC Endpoints

| Method | Parameters | Response |
|--------|------------|----------|
| `instruction.add` | `{ input: string, tier: string }` | `{ id: string, confirmation_required: bool }` |
| `instruction.list` | `{ scope: "project"|"global" }` | `{ instructions: [...] }` |
| `instruction.delete` | `{ id: string }` | `{ success: bool }` |
| `instruction.execute` | `{ id: string }` | `{ output: any, error: string }` |
| `instruction.preview` | `{ input: string }` | `{ parsed: {...}, actions: [...] }` |

### HTTP Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/instructions` | GET | List all instructions |
| `/api/v1/instructions` | POST | Create new instruction |
| `/api/v1/instructions/:id` | GET | Get instruction details |
| `/api/v1/instructions/:id` | DELETE | Remove instruction |
| `/api/v1/instructions/preview` | POST | Preview parsed instruction |

---

## Examples

### Example 1: Post-Hook Instruction

**User input:** "Always run tests after I touch Go files"

**Parsed instruction:**
```markdown
---
id: run-tests-after-go-changes
trigger: post_tool_complete:write_file:*.go
action: shell_execute
action_args:
  command: go test ./...
  timeout: 5m
enabled: true
scope: project
created_at: 2026-06-21
---

Run `go test ./...` after any Go file modification.
```

**Execution flow:**
1. `write_file` tool completes with path `internal/foo/bar.go`
2. Bus publishes `tool.completed` with payload `{tool: "write_file", path: "internal/foo/bar.go"}`
3. `InstructionListener` matches pattern `write_file:*.go`
4. Calls `shell_execute(command="go test ./...")`
5. Output logged or returned to user

---

### Example 2: Cron Instruction

**User input:** "Every morning at 9am, summarize my conversations"

**Parsed instruction:**
```markdown
---
id: daily-conversation-summary
trigger: cron:0 0 9 * * *
action: agent_trigger
action_args:
  agent: analyst
  prompt: Summarize all conversations from the past 24 hours. Identify key decisions, unresolved questions, and actionable insights.
enabled: true
scope: global
---

Generate a daily summary of conversations, decisions, and insights.
```

**Execution flow:**
1. Scheduler loads instruction as `AgentJob`
2. At 9:00 AM, publishes to `chat.request` bus topic
3. `analyst` agent processes prompt
4. Summary stored in memory or sent to user

---

### Example 3: Intent-Based Instruction

**User input:** "When I ask about APIs, always fetch the latest documentation"

**Parsed instruction:**
```markdown
---
id: fetch-docs-for-api-questions
trigger: intent:IntentResearch
action: agent_trigger
action_args:
  agent: researcher
  tool_hints: ["web_fetch", "web_search"]
enabled: true
scope: global
---

For API-related research questions, fetch current documentation before answering.
```

**Execution flow:**
1. User asks: "How does the Stripe API handle webhooks?"
2. Dispatcher classifies as `IntentResearch`
3. Matches instruction trigger `intent:IntentResearch`
4. Routes to `researcher` agent with `tool_hints`
5. Researcher fetches docs, then answers

---

### Example 4: Git Pre-Commit Instruction

**User input:** "Before I commit, always run the linter"

**Parsed instruction:**
```markdown
---
id: lint-before-commit
trigger: git_pre_commit
action: shell_execute
action_args:
  command: golangci-lint run
  timeout: 2m
enabled: true
scope: project
---

Run the Go linter before every commit to catch issues early.
```

**Execution flow:**
1. User runs `git commit`
2. `.git/hooks/pre-commit-user` executes
3. RPC call: `instruction.execute_git_hook {type: "pre_commit"}`
4. `shell_execute(command="golangci-lint run")` runs
5. If linter fails, commit is blocked

---

### Example 5: Event-Based Instruction

**User input:** "When I create a memory, tag it with the current project context"

**Parsed instruction:**
```markdown
---
id: tag-memory-with-project
trigger: event:memory.stored
action: memory_curation
action_args:
  operation: add_metadata
  metadata_key: project
  metadata_value: ${current_project}
enabled: true
scope: global
---

Automatically tag new memories with the active project name.
```

**Execution flow:**
1. `memory_store` tool stores a memory
2. Bus publishes `memory.stored` event
3. `InstructionListener` matches `event:memory.stored`
4. Updates memory metadata with project context

---

## Testing Strategy

### Unit Tests

| Component | Test Cases |
|-----------|------------|
| `InstructionParser.Parse()` | Cron patterns, post-hook patterns, intent patterns, git hooks, edge cases |
| `UserInstructionStore.Discovery()` | Tier discovery, shadowing, invalid files, missing fields |
| `InstructionListener.checkPostHookInstructions()` | Pattern matching, action execution, error handling |
| `InstructionScheduler.SyncCronInstructions()` | Job creation, validation, error handling |

### Integration Tests

1. **Full pipeline test:**
   - Add instruction via CLI
   - Trigger matching event
   - Verify action executed

2. **Persistence test:**
   - Save instruction to project tier
   - Restart daemon
   - Verify instruction loaded and active

3. **Confirmation flow test:**
   - Add high-risk instruction (shell command)
   - Verify confirmation prompt
   - Confirm and verify execution

### E2E Tests

1. **Go test workflow:**
   ```
   User: "Always run tests after Go file changes"
   → Write Go file
   → Observe test execution
   → Verify output
   ```

2. **Daily summary workflow:**
   ```
   User: "Summarize my conversations every day at 9am"
   → Mock time to 9am
   → Verify summary generated
   → Check memory store
   ```

---

## Implementation Plan

### Phase 1: Core Infrastructure (Week 1)

**Files to create:**
- `internal/agent/intent.go` — Add `IntentInstruction`
- `internal/agent/instruction_parser.go` — Parser implementation
- `internal/preferences/store.go` — Instruction store
- `internal/agent/instruction_handler.go` — Bus handler

**Files to modify:**
- `internal/agent/loop.go` — Context injection
- `internal/agent/dispatcher.go` — Intent routing
- `cmd/meept/instructions.go` — CLI commands

**Deliverable:** User can add/list/delete instructions via CLI, instructions are persisted and loaded.

---

### Phase 2: Trigger Wiring (Week 2)

**Bus Listeners:**
- `internal/agent/instruction_listeners.go` — Post-hook + event listeners

**Scheduler Integration:**
- `internal/scheduler/instructions.go` — Cron instruction sync

**Git Hooks:**
- Generate `.git/hooks/pre-commit-user` on save
- RPC endpoint for hook execution

**Deliverable:** Instructions execute on matching triggers.

---

### Phase 3: UI/API + Security (Week 3)

**HTTP Endpoints:**
- `internal/comm/http/instructions_handlers.go` — REST API

**Confirmation UI:**
- TUI dialog for high-risk instructions
- Menubar notification for confirmations

**Security Gates:**
- `internal/security/instruction_validator.go` — Tool lookup, risk assessment
- Known-safe commands configuration

**Deliverable:** Full user-facing feature with confirmation flows.

---

### Phase 4: Testing + Documentation (Week 4)

**Tests:**
- Unit tests for parser, store, listeners
- Integration tests for full pipeline
- E2E tests for common workflows

**Documentation:**
- `docs/workflows/user-instructions.md` — Feature spec
- `docs/reference/cli/instructions.md` — CLI reference
- `docs/concepts/instructions.md` — Conceptual guide

**Deliverable:** Production-ready feature with tests and docs.

---

## Open Questions

### 1. Instruction Conflicts

**Scenario:** Two instructions with overlapping triggers:
- "Run `go test` after Go file changes"
- "Run `make test` after Go file changes"

**Options:**
- **Execute both** — simplest, but may be redundant or conflicting
- **Priority-based** — higher priority wins
- **Merge actions** — combine into sequence

**Recommended:** Execute both, but log potential conflicts.

---

### 2. Instruction Lifecycle

**Question:** When should instructions be disabled or archived?

**Proposal:**
- **Disabled by user** — `enabled: false` in frontmatter
- **Auto-disable on repeated failure** — After N consecutive failures, disable and notify
- **Deprecation** — Mark as deprecated when tool/action becomes unavailable

---

### 3. Context Window Limits

**Question:** What if user has 100+ instructions?

**Proposal:**
- **Inject only active instructions** — `enabled: true` only
- **Scope filtering** — Project instructions only injected for project sessions
- **Summarization** — "You have 12 active instructions" + on-demand lookup

---

### 4. Cross-User Instructions

**Question:** Should instructions be user-specific?

**Current design:** Single instruction store per tier.

**Future extension:** Add `user_id` field for multi-user scenarios:
```yaml
user_id: caimlas  # optional, defaults to "anonymous"
```

---

## Backward Compatibility

- **No breaking changes** — existing tools, agents, scheduler unchanged
- **Opt-in** — instructions only execute if added by user
- **Graceful degradation** — If instruction parsing fails, log warning and continue

---

## Success Metrics

| Metric | Target |
|--------|--------|
| Instructions added per user | >3/week |
| Instruction execution success rate | >95% |
| Time-to-first-instruction | <2 minutes |
| User retention (4-week) | >80% |

---

## References

- Agent Roster Consolidation Design: `docs/superpowers/specs/2026-06-20-agent-roster-consolidation-design.md`
- Skill Discovery Architecture: `docs/concepts/skills.md`
- Scheduler Job Types: `internal/scheduler/jobs.go`
- Dispatcher Intent Routing: `internal/agent/dispatcher.go`
