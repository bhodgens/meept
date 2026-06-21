# User Instructions: Implementation Plan

**Date:** 2026-06-21
**Status:** Implementation Plan
**Source Spec:** `docs/superpowers/specs/2026-06-21-user-instructions-design.md`
**Integration Notes:** Based on overlap analysis with Self-Improve, Q Agent, Learning, Scheduler

---

## Overview

This plan implements a **Natural Language Automation Layer** enabling users to instruct Meept to "always do X" when certain conditions are met.

**Key Design Decisions:**
1. **Storage:** Markdown + YAML frontmatter (consistent with AGENT.md, skills)
2. **Discovery:** Tiered (`.meept/instructions/` → `~/.meept/instructions/` → `~/.config/meept/instructions/`)
3. **Execution:** Reuse existing Scheduler (cron), bus listeners (post-hooks), git hooks
4. **Integration:** Merge context injection with Learning System; Q Agent recommends instructions

---

## Architecture Summary

```
┌─────────────────────────────────────────────────────────────────┐
│  User Input (natural language)                                   │
│  "Always run tests after I touch Go files"                       │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Dispatcher (IntentInstruction classification)                   │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  InstructionParser (NL → structured rule)                        │
│  - Extracts: trigger, action, scope, priority                    │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Validation + Confirmation                                       │
│  - Tool exists? Risk level? Confirm high-risk?                   │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  UserInstructionStore (persist to tiered storage)                │
└─────────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────┬───────┴───────┬─────────────┬─────────────┐
        ▼             ▼               ▼             ▼             ▼
┌──────────────┐ ┌──────────┐ ┌────────────┐ ┌──────────┐ ┌──────────┐
│ Scheduler    │ │ Bus      │ │ Git Hooks  │ │ Intent   │ │ Context  │
│ (cron jobs)  │ │ Listeners│ │ (pre/post) │ │ Router   │ │ Injector │
└──────────────┘ └──────────┘ └────────────┘ └──────────┘ └──────────┘
```

---

## Phase 1: Core Infrastructure (Days 1-5)

**Goal:** Users can add/list/delete instructions via CLI; instructions are persisted and loaded.

**Provides:**
- `IntentInstruction IntentType = "instruction"` — dispatcher routes NL automation requests to instruction handler
- `type ParsedInstruction struct{...}` — structured trigger/action config consumed by Phase 2
- `instruction.add|list|delete|execute|preview` bus topics — other components subscribe to instruction lifecycle events
- `func (s *UserInstructionStore) Discovery()`, `Save()`, `Delete()`, `GetActive()` — Phase 2/3/4 call these for tiered storage

**Consumes:**
- Message bus pub/sub infrastructure — Phase 1 handlers register via existing `bus.Subscribe()`
- Tool registry interface — verifier calls `registry.HasTool(toolName)` to validate actions
- Security engine interface — verifier calls `engine.CheckRisk()` for risk assessment

**Anti-completion signals:**
- `IntentInstruction.*=.*"instruction"$` absent from `internal/agent/intent.go` — intent type not defined
- `// TODO|// FIXME|XXX:` in `internal/agent/instruction_*.go` or `internal/preferences/` — unfinished implementation
- `func.*Parse.*{` with body under 10 lines — parser stub without real logic
- `return nil, nil$` or `return ParsedInstruction{}` in `instruction_parser.go` — no-op stub
- `would parse|should extract|placeholder|stub` in phase files — stub language
- Absence of `internal/agent/instruction_parser_test.go` or `internal/preferences/store_test.go`

**Behavioral acceptance:**
- Test: `go test ./internal/agent/... ./internal/preferences/... -run TestPhase1E2E -v`
- Asserts: `meept instructions add "Always run tests after I touch Go files"` → instruction persisted to `.meept/instructions/`
- Asserts: `meept instructions list` returns the added instruction with correct trigger/action parsed
- Asserts: `IntentInstruction` intent type routes to instruction handler (not coder/chat/other)
- Test file: `internal/agent/phase1_e2e_test.go`

---

### Tasks

#### 1.1 Define Intent Type (`internal/agent/intent.go`)

**File:** `internal/agent/intent.go`

**Changes:**
```go
const (
    IntentCode       IntentType = "code"
    IntentDebug      IntentType = "debug"
    // ... existing intents ...
    IntentInstruction IntentType = "instruction"  // NEW
)
```

**Testing:** Unit test for intent classification.

---

#### 1.2 Instruction Parser (`internal/agent/instruction_parser.go`)

**File:** `internal/agent/instruction_parser.go` (NEW)

**Structs:**
```go
type ParsedInstruction struct {
    Trigger    TriggerConfig
    Action     ActionConfig
    Scope      string  // "project" | "global"
    Priority   string  // "low" | "normal" | "high" | "critical"
    RawInput   string
}

type TriggerConfig struct {
    Type       string  // "cron" | "post_hook" | "event" | "intent" | "git"
    Pattern    string
    Conditions map[string]string
}

type ActionConfig struct {
    Tool       string
    Args       map[string]any
    AgentID    string  // for agent_trigger
}
```

**Functions:**
- `Parse(ctx, input) (*ParsedInstruction, error)` — Main parsing logic
- `extractTrigger(input) (TriggerConfig, error)` — Extract trigger pattern
- `extractAction(input) (ActionConfig, error)` — Extract action
- `extractScope(input) string` — Determine scope (project vs global)

**NL Parsing Strategy:**
- Start with **pattern matching** (regexp for cron times, "after X", "when Y")
- Fall back to **LLM-based extraction** for complex inputs
- Return structured `ParsedInstruction` with confidence score

**Testing:**
- Table-driven tests for trigger patterns (cron, post_hook, event, intent, git)
- Test action extraction (shell_execute, memory_retain, agent_trigger)
- Test scope detection (project keywords vs global)

---

#### 1.3 User Instruction Store (`internal/preferences/store.go`)

**File:** `internal/preferences/store.go` (NEW)

**Package:** `preferences` (new package)

**Structs:**
```go
type UserInstructionStore struct {
    tiers        []string  // discovery paths
    instructions map[string]*UserInstruction
    mu           sync.RWMutex
}

type UserInstruction struct {
    ID          string            `yaml:"id"`
    Trigger     string            `yaml:"trigger"`
    Action      string            `yaml:"action"`
    ActionArgs  map[string]any    `yaml:"action_args"`
    Enabled     bool              `yaml:"enabled"`
    Scope       string            `yaml:"scope"`
    Priority    string            `yaml:"priority"`
    CreatedAt   time.Time         `yaml:"created_at"`
    Body        string            // Markdown body (injected into context)
}
```

**Functions:**
- `NewUserInstructionStore(tiers []string) *UserInstructionStore`
- `Discovery() ([]*UserInstruction, error)` — Scan tiers, apply shadowing
- `Save(instr *UserInstruction, tier string) error` — Persist to specific tier
- `Delete(id string) error` — Remove instruction
- `GetActive() []*UserInstruction` — Return enabled instructions
- `Get(id string) *UserInstruction` — Get single instruction

**Tier Discovery (same as skills):**
```go
var DefaultTiers = []string{
    ".meept/instructions",           // Project-local (highest)
    "~/.meept/instructions",         // User-global
    "~/.config/meept/instructions",  // System-wide (lowest)
}
```

**Testing:**
- Test tier discovery and shadowing
- Test save/load round-trip
- Test GetActive filtering (enabled only)

---

#### 1.4 Instruction Handler (`internal/agent/instruction_handler.go`)

**File:** `internal/agent/instruction_handler.go` (NEW)

**Struct:**
```go
type InstructionHandler struct {
    store    *preferences.UserInstructionStore
    bus      *bus.MessageBus
    parser   *InstructionParser
    logger   *slog.Logger
    verifier *InstructionVerifier  // NEW, see 1.5
}
```

**Bus Subscriptions:**
- `instruction.add` — Add new instruction
- `instruction.list` — List all instructions
- `instruction.delete` — Remove instruction
- `instruction.execute` — Execute instruction by ID (manual trigger)
- `instruction.preview` — Preview parsed instruction (dry-run)

**Response Format:**
```go
type InstructionResponse struct {
    Success             bool              `json:"success"`
    Instruction         *UserInstruction  `json:"instruction,omitempty"`
    Instructions        []*UserInstruction `json:"instructions,omitempty"`
    ParsedInstruction   *ParsedInstruction `json:"parsed,omitempty"`
    ConfirmationRequired bool             `json:"confirmation_required"`
    Error               string            `json:"error,omitempty"`
}
```

**Testing:**
- Test bus message handling
- Test add → list → delete flow
- Test error responses (invalid input, tool not found)

---

#### 1.5 Instruction Verifier (`internal/preferences/verifier.go`)

**File:** `internal/preferences/verifier.go` (NEW)

**Purpose:** Validate instructions before saving; check tool existence, risk level.

**Struct:**
```go
type InstructionVerifier struct {
    toolRegistry *tools.Registry
    securityEngine *security.Engine
}

type VerificationResult struct {
    Valid            bool
    RiskLevel        string  // "low" | "medium" | "high"
    ConfirmationNeeded bool
    Errors           []string
    Warnings         []string
}
```

**Functions:**
- `Verify(instr *ParsedInstruction) VerificationResult`
- `checkToolExists(tool string) bool`
- `assessRisk(tool string, args map[string]any) string`
- `getKnownSafeCommands() []string`

**Risk Assessment:**
| Risk Level | Examples | Confirmation |
|------------|----------|--------------|
| Low | `memory_retain`, `notification` | Not required |
| Medium | `shell_execute` (known-safe commands) | Required |
| High | `shell_execute` (rm, curl, sudo), `file_delete` | Required + explicit ack |

**Testing:**
- Test tool existence checks
- Test risk assessment (known-safe vs dangerous commands)
- Test verification result structure

---

#### 1.6 CLI Commands (`cmd/meept/instructions.go`)

**File:** `cmd/meept/instructions.go` (NEW)

**Commands:**
```bash
meept instructions list                      # List all active instructions
meept instructions add "<natural language>"  # Add new instruction
meept instructions delete <id>               # Remove instruction
meept instructions show <id>                 # Show instruction details
meept instructions preview "<input>"         # Preview parsed instruction
```

**Implementation:**
- `list` → RPC call `instruction.list`
- `add` → RPC call `instruction.add { input: "..." }`
- `delete` → RPC call `instruction.delete { id: "..." }`
- `preview` → RPC call `instruction.preview { input: "..." }`

**Testing:**
- Manual testing of all commands
- Test connection error handling (daemon not running)

---

#### 1.7 Integration with Daemon (`internal/daemon/components.go`)

**File:** `internal/daemon/components.go`

**Changes:**
```go
// In loadComponents() or similar:
instructionStore := preferences.NewUserInstructionStore(preferences.DefaultTiers)
instructionParser := agent.NewInstructionParser(logger)
instructionHandler := agent.NewInstructionHandler(instructionStore, msgBus, instructionParser, verifier)
instructionHandler.Start(ctx)
```

**Testing:**
- Verify handler starts with daemon
- Verify instructions loaded on startup

---

### Deliverables (Phase 1)

- [ ] `IntentInstruction` type added
- [ ] `InstructionParser` implemented with pattern matching
- [ ] `UserInstructionStore` with tiered discovery
- [ ] `InstructionHandler` with bus subscriptions
- [ ] `InstructionVerifier` with risk assessment
- [ ] CLI commands (`add`, `list`, `delete`, `show`, `preview`)
- [ ] Daemon integration (handler starts with daemon)
- [ ] Unit tests for parser, store, verifier
- [ ] Manual testing of CLI commands
- [ ] **CONTRACT VERIFICATION:** Pass 1 (anti-stub scan), Pass 2 (Provides/Consumes wired), Pass 3 (e2e test passes)

---

## Phase 2: Trigger Wiring (Days 6-10)

**Goal:** Instructions execute automatically when triggers match.

**Provides:**
- `func (s *InstructionScheduler) SyncCronInstructions() error` — Phase 4 calls to verify cron sync
- `func (l *InstructionListener) Start(ctx)` — starts bus event listeners for post_hook/event triggers
- `func GeneratePreCommitHook(hookPath string) error` — git hook file generation, called by daemon on instruction save
- `func (d *Dispatcher) isInstructionInput(input) bool` — detects NL automation requests in user input

**Consumes:**
- Phase 1's `UserInstructionStore.GetActive()` — scheduler/listeners query for enabled instructions
- Phase 1's `ParsedInstruction.Trigger` / `.Action` — trigger type routing (cron vs post_hook vs git)
- Existing `Scheduler.CreateJob()` / `Scheduler.CreateJobWithDeps()` — cron instructions become scheduled jobs
- Message bus `Subscribe()` / `Publish()` — listeners subscribe to tool/task/memory events
- Existing git hook infrastructure — reuses `.git/hooks/` path conventions from `pre-commit-deferred`

**Anti-completion signals:**
- `func.*SyncCron.*{` with body under 15 lines — scheduler stub without real job creation
- `return nil$` in `checkPostHookInstructions` or `executeAction` — no-op listener
- `// TODO|// FIXME` in `internal/scheduler/instructions.go` or `internal/agent/instruction_listeners.go`
- `would sync|should listen|placeholder|stub` in phase files — stub language
- `if.*Trigger.*==.*"cron"` absent from `internal/scheduler/instructions.go` — cron routing not implemented
- `bus.Subscribe` absent from `internal/agent/instruction_listeners.go` — event listeners not wired
- Absence of `internal/scheduler/phase2_cron_test.go` or `internal/agent/phase2_listener_test.go`

**Behavioral acceptance:**
- Test: `go test ./internal/scheduler/... ./internal/agent/... -run TestPhase2TriggerE2E -v`
- Asserts: cron instruction "Every day at 9am, run tests" → job scheduled, executes at mocked time
- Asserts: post_hook instruction "After write_file:*.go, run linter" → listener fires on `tool.completed` event
- Asserts: git_pre_commit instruction → `.git/hooks/pre-commit-user` exists and runs on commit
- Test file: `internal/scheduler/phase2_e2e_test.go`

---

### Tasks

#### 2.1 Cron Instructions (`internal/scheduler/instructions.go`)

**File:** `internal/scheduler/instructions.go` (NEW)

**Struct:**
```go
type InstructionScheduler struct {
    scheduler *Scheduler
    store     *preferences.UserInstructionStore
    logger    *slog.Logger
}
```

**Functions:**
- `SyncCronInstructions() error` — Load cron-type instructions as jobs
- `instructionToJob(instr *UserInstruction) (Job, error)` — Convert to AgentJob or ShellJob

**Job Creation:**
```go
if instr.Action == "agent_trigger" {
    return NewAgentJob(JobConfig{
        ID:       "instruction_" + instr.ID,
        Name:     "Instruction: " + instr.ID,
        Schedule: strings.TrimPrefix(instr.Trigger, "cron:"),
        Type:     JobTypeAgent,
        AgentConfig: &AgentJobConfig{
            Prompt: instr.Body,
            Model:  instr.ActionArgs["model"].(string),
        },
    }, scheduler.Bus())
} else if instr.Action == "shell_execute" {
    return NewShellJob(JobConfig{
        ID:       "instruction_" + instr.ID,
        Name:     "Instruction: " + instr.ID,
        Schedule: strings.TrimPrefix(instr.Trigger, "cron:"),
        Type:     JobTypeShell,
        ShellConfig: &ShellJobConfig{
            Command: instr.ActionArgs["command"].(string),
            Timeout: instr.ActionArgs["timeout"].(time.Duration),
        },
    }, scheduler.Bus())
}
```

**Sync on Startup:**
```go
// In scheduler.Start():
if err := s.SyncCronInstructions(); err != nil {
    logger.Warn("failed to sync cron instructions", "error", err)
}
```

**Testing:**
- Test cron instruction → job conversion
- Test job execution (verify action runs)
- Test sync on startup

---

#### 2.2 Bus Listeners (`internal/agent/instruction_listeners.go`)

**File:** `internal/agent/instruction_listeners.go` (NEW)

**Struct:**
```go
type InstructionListener struct {
    store        *preferences.UserInstructionStore
    bus          *bus.MessageBus
    toolExecutor *tools.Registry
    logger       *slog.Logger
}
```

**Bus Subscriptions:**
- `tool.completed` — For `post_tool_complete:*` triggers
- `task.completed` — For `post_task_complete:*` triggers
- `memory.stored` — For `event:memory.stored` triggers
- `session.started` — For `event:session.started` triggers

**Trigger Matching:**
```go
func (l *InstructionListener) checkPostHookInstructions(msg *BusMessage) {
    instructions := l.store.GetActive()
    for _, instr := range instructions {
        if !strings.HasPrefix(instr.Trigger, "post_tool_complete:") {
            continue
        }

        // Parse: post_tool_complete:tool=path_pattern
        parts := strings.Split(strings.TrimPrefix(instr.Trigger, "post_tool_complete:"), ":")
        if len(parts) != 2 {
            continue
        }
        toolName := parts[0]
        pathPattern := parts[1]

        // Check match
        if msg.Payload["tool"] != toolName {
            continue
        }
        if pathPattern != "*" && !matchPattern(msg.Payload["path"].(string), pathPattern) {
            continue
        }

        // Execute
        l.executeAction(instr)
    }
}
```

**Pattern Matching:**
```go
func matchPattern(path, pattern string) bool {
    // Handle *.go, **/*.go, etc.
    matched, _ := filepath.Match(pattern, path)
    return matched
}
```

**Testing:**
- Test trigger pattern matching
- Test action execution on match
- Test multiple instructions with same trigger

---

#### 2.3 Git Hooks Integration

**File:** `.git/hooks/pre-commit-user` (generated)

**Generation:** When user saves `git_pre_commit` instruction, generate hook:

```bash
#!/bin/bash
# Auto-generated by Meept User Instructions
# DO NOT EDIT MANUALLY

meept rpc call instruction.execute_git_hook '{"type": "pre_commit"}'
exit_code=$?

if [ $exit_code -ne 0 ]; then
    echo "Git pre-commit instruction failed"
    exit 1
fi
```

**File:** `internal/preferences/git_hooks.go` (NEW)

**Functions:**
- `GeneratePreCommitHook(hookPath string) error`
- `GeneratePostCommitHook(hookPath string) error`
- `ExecuteGitHook(type string) error` — RPC handler

**RPC Handler:**
```go
func (h *InstructionHandler) handleExecuteGitHook(ctx context.Context, msg *BusMessage) {
    var req struct {
        Type string `json:"type"`  // "pre_commit" | "post_commit"
    }
    json.Unmarshal(msg.Payload, &req)

    instructions := h.store.GetActive()
    for _, instr := range instructions {
        if instr.Trigger == "git_"+req.Type {
            // Execute action
            h.executeAction(instr)
        }
    }
}
```

**Testing:**
- Test hook generation
- Test hook execution (mock RPC)
- Test git commit with pre-commit hook

---

#### 2.4 Intent-Based Instructions (`internal/agent/dispatcher.go`)

**File:** `internal/agent/dispatcher.go`

**Changes:**
```go
func (d *Dispatcher) ClassifyAndRoute(ctx context.Context, input string) (*DispatchResult, error) {
    // Existing classification...

    // NEW: Check for IntentInstruction
    if d.isInstructionInput(input) {
        return &DispatchResult{
            Intent:      IntentInstruction,
            AgentID:     d.config.InstructionAgent,
            Instruction: d.parser.Parse(ctx, input),
        }, nil
    }

    // NEW: Check for intent-based instruction matches
    instructions := d.instructionStore.GetActive()
    for _, instr := range instructions {
        if strings.HasPrefix(instr.Trigger, "intent:") {
            triggerIntent := strings.TrimPrefix(instr.Trigger, "intent:")
            if d.classifyIntent(input) == IntentType(triggerIntent) {
                // Route normally, but attach instruction action
                result.AttachedActions = append(result.AttachedActions, instr.Action)
            }
        }
    }

    return result, nil
}
```

**Detection:**
```go
func (d *Dispatcher) isInstructionInput(input string) bool {
    // Keywords that indicate instruction
    instructionKeywords := []string{
        "always", "never", "every time", "whenever",
        "from now on", "remember to", "make sure to",
    }
    lower := strings.ToLower(input)
    for _, kw := range instructionKeywords {
        if strings.Contains(lower, kw) {
            return true
        }
    }
    return false
}
```

**Testing:**
- Test instruction detection
- Test intent-based trigger matching
- Test attached action execution

---

### Deliverables (Phase 2)

- [ ] `InstructionScheduler` for cron instructions
- [ ] `InstructionListener` for bus event triggers
- [ ] Git hook generation (`pre-commit-user`, `post-commit-user`)
- [ ] Intent-based instruction matching in Dispatcher
- [ ] Integration tests for full trigger → execution flow
- [ ] Manual testing of all trigger types
- [ ] **CONTRACT VERIFICATION:** Pass 1 (anti-stub scan), Pass 2 (Provides/Consumes wired), Pass 3 (cron + listener + git tests pass)

---

## Phase 3: UI/API + Security (Days 11-15)

**Goal:** REST API, confirmation dialogs, security gates.

**Provides:**
- `POST/GET/PUT/DELETE /api/v1/instructions` — HTTP CRUD endpoints for instruction management
- `POST /api/v1/instructions/preview` — dry-run parsing endpoint for TUI/menubar
- `func (v *InstructionValidator) Validate(instr) error` — security validation called before save
- `ConfirmationRequired bool` in response — TUI displays dialog when true

**Consumes:**
- Phase 1's `InstructionVerifier.Verify()` — HTTP handler calls verifier before persisting
- Phase 1's `UserInstructionStore.Save()` — HTTP POST/PUT persist to tiered storage
- Phase 2's `InstructionScheduler.SyncCronInstructions()` — API triggers re-sync on cron instruction create/update
- Security engine `CheckToolPermission()`, `CheckRisk()` — validator delegates to existing security layer

**Anti-completion signals:**
- `func.*handle.*Instruction.*{` with body under 20 lines — HTTP handler stub
- `// TODO|// FIXME` in `internal/comm/http/instructions_handlers.go` or `internal/tui/instructions.go`
- `would show|should validate|placeholder|stub` in phase files — stub language
- `http.Error.*BadRequest` absent from handlers — no validation wiring
- `bubbletea` import absent from `internal/tui/instructions.go` — TUI dialog not implemented
- `isHighRiskCommand` always returns false — security gate stub
- Absence of `internal/comm/http/phase3_api_test.go` or `internal/tui/phase3_dialog_test.go`

**Behavioral acceptance:**
- Test: `go test ./internal/comm/http/... ./internal/tui/... -run TestPhase3SecurityE2E -v`
- Asserts: `POST /api/v1/instructions` with high-risk command → 400 + "requires explicit approval"
- Asserts: `POST /api/v1/instructions` with known-safe command → 200 + instruction persisted
- Asserts: TUI shows confirmation dialog for risk=medium/high, skips for risk=low
- Test file: `internal/comm/http/phase3_e2e_test.go`

---

### Tasks

#### 3.1 HTTP Endpoints (`internal/comm/http/instructions_handlers.go`)

**File:** `internal/comm/http/instructions_handlers.go` (NEW)

**Endpoints:**
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/instructions` | List all instructions |
| POST | `/api/v1/instructions` | Create new instruction |
| GET | `/api/v1/instructions/:id` | Get instruction details |
| PUT | `/api/v1/instructions/:id` | Update instruction |
| DELETE | `/api/v1/instructions/:id` | Remove instruction |
| POST | `/api/v1/instructions/preview` | Preview parsed instruction |

**Handler Example:**
```go
func (s *Server) handleCreateInstruction(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Input string `json:"input"`
        Tier  string `json:"tier,omitempty"`
    }
    json.NewDecoder(r.Body).Decode(&req)

    // Parse
    parsed := s.instructionParser.Parse(r.Context(), req.Input)

    // Verify
    result := s.verifier.Verify(parsed)
    if !result.Valid {
        http.Error(w, result.Errors[0], http.StatusBadRequest)
        return
    }

    // Save
    instr := &UserInstruction{
        ID:        generateID(parsed),
        Trigger:   parsed.Trigger.Pattern,
        Action:    parsed.Action.Tool,
        ActionArgs: parsed.Action.Args,
        Enabled:   true,
        Scope:     parsed.Scope,
        Priority:  parsed.Priority,
        CreatedAt: time.Now(),
    }
    s.instructionStore.Save(instr, req.Tier)

    json.NewEncoder(w).Encode(InstructionResponse{
        Success:             true,
        Instruction:         instr,
        ConfirmationRequired: result.ConfirmationNeeded,
    })
}
```

**Testing:**
- Test all endpoints (happy path + errors)
- Test authentication (if enabled)
- Test CORS headers

---

#### 3.2 Confirmation UI (TUI Dialog)

**File:** `internal/tui/instructions.go` (NEW)

**Dialog:**
```
┌─────────────────────────────────────────────────────────────┐
│  Confirm Instruction                                         │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  This instruction will execute shell commands:               │
│                                                              │
│    Command: go test ./...                                    │
│    Trigger: After write_file:*.go                            │
│                                                              │
│  Risk Level: MEDIUM                                          │
│                                                              │
│  Continue? [Y/n]                                             │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

**Implementation:**
- Use `bubbletea` for TUI dialog
- Show on `add` command if `ConfirmationRequired == true`
- Support `--force` flag to skip confirmation

**Testing:**
- Manual testing of dialog flow
- Test --force flag

---

#### 3.3 Security Gates

**File:** `internal/security/instruction_validator.go` (NEW)

**Integration with Security Engine:**
```go
type InstructionValidator struct {
    engine *security.Engine
}

func (v *InstructionValidator) Validate(instr *UserInstruction) error {
    // Check tool permissions
    if err := v.engine.CheckToolPermission(instr.Action); err != nil {
        return err
    }

    // Check command risk (for shell_execute)
    if instr.Action == "shell_execute" {
        cmd := instr.ActionArgs["command"].(string)
        if isHighRiskCommand(cmd) {
            return fmt.Errorf("high-risk command requires explicit approval")
        }
    }

    return nil
}

func isHighRiskCommand(cmd string) bool {
    highRiskPatterns := []string{
        `rm\s+-(rf|\-force)`,  // Forced deletion
        `curl.*\|\s*bash`,     // Curl pipe bash
        `sudo\s+`,             // Sudo commands
        `chmod\s+777`,         // World-writable
    }
    for _, pattern := range highRiskPatterns {
        if matched, _ := regexp.MatchString(pattern, cmd); matched {
            return true
        }
    }
    return false
}
```

**Known-Safe Commands Config:**
```go
var KnownSafeCommands = []string{
    "go test ./...",
    "go build ./...",
    "go fmt ./...",
    "gofmt -w .",
    "git status",
    "git diff",
}
```

**Testing:**
- Test command pattern matching
- Test known-safe allowlist
- Test high-risk detection

---

### Deliverables (Phase 3)

- [ ] HTTP REST API (CRUD + preview endpoints)
- [ ] TUI confirmation dialog
- [ ] Security validator with risk assessment
- [ ] Known-safe commands configuration
- [ ] Integration tests for confirmation flow
- [ ] Manual testing of TUI + API
- [ ] **CONTRACT VERIFICATION:** Pass 1 (anti-stub scan), Pass 2 (Provides/Consumes wired), Pass 3 (API + security tests pass)

---

## Phase 4: Integration + Testing + Documentation (Days 16-20)

**Goal:** Full integration with Learning/Q Agent, comprehensive tests, documentation.

**Provides:**
- `func (d *PatternDetector) RecommendInstruction() string` — Q Agent suggests instructions from patterns
- `func (c *ContextInjector) BuildSystemPrompt(base string) string` — merged Learning + Instructions context
- `internal/agent/phase4_e2e_test.go` — comprehensive E2E test suite
- `docs/workflows/user-instructions.md`, `docs/concepts/instructions.md` — user-facing documentation

**Consumes:**
- Phase 1's `InstructionHandler` — Q Agent calls `instruction.add` bus topic for suggestions
- Phase 1's `UserInstructionStore.GetActive()` — context injector queries active instructions
- Phase 2's `InstructionListener` — E2E tests verify full trigger → execution pipeline
- Phase 3's `InstructionValidator` — E2E tests verify security gates block high-risk instructions

**Anti-completion signals:**
- `func.*RecommendInstruction.*{` with body under 10 lines — Q Agent integration stub
- `func.*BuildSystemPrompt.*{` without instructions/learning merge — context injector stub
- `// TODO|// FIXME` in `internal/agent/q/pattern_detector.go` or `internal/agent/context_injector.go`
- `would integrate|should merge|placeholder|stub` in phase files — stub language
- `instructions.GetActive()` absent from `context_injector.go` — instructions not injected
- Absence of `docs/workflows/user-instructions.md` or `docs/concepts/instructions.md`
- Absence of `internal/agent/phase4_e2e_test.go`

**Behavioral acceptance:**
- Test: `go test ./internal/agent/... -run TestPhase4IntegrationE2E -v`
- Asserts: Q Agent detects recurring pattern → suggests "Always run X after Y" instruction
- Asserts: System prompt includes both Learning patterns AND User Instructions when both active
- Asserts: E2E scenario "Go test automation": add instruction → edit Go file → tests run → result persisted
- Test file: `internal/agent/phase4_e2e_test.go`

---

### Tasks

#### 4.1 Q Agent Integration (Recommend Instructions)

**File:** `internal/agent/q/pattern_detector.go`

**Changes:**
```go
func (d *PatternDetector) detectSkillOpportunity(analyses []*SessionAnalysis) []PatternReport {
    // Existing detection...

    for toolPattern, count := range intentCommands {
        if count >= 5 {
            reports = append(reports, PatternReport{
                ID:                   fmt.Sprintf("skill_opportunity_%s", toolPattern),
                PatternType:          "skill_opportunity",
                RecommendedAction:    "suggest_user_instruction",
                SuggestedInstruction: fmt.Sprintf(
                    "Always run %s when %s happens",
                    toolPattern, triggerEvent,
                ),
                Confidence: min(1.0, float64(count)/10.0),
            })
        }
    }
    return reports
}
```

**Notification:**
- Add to Q Agent notifications: "Detected opportunity for User Instruction"
- Link to `meept instructions preview "<suggested>"`

**Testing:**
- Test pattern → instruction suggestion
- Test notification generation

---

#### 4.2 Learning System Integration (Merged Context Injection)

**File:** `internal/agent/context_injector.go` (NEW)

**Struct:**
```go
type ContextInjector struct {
    learning     *selfimprove.LearningSystem
    instructions *preferences.UserInstructionStore
    logger       *slog.Logger
}
```

**Function:**
```go
func (c *ContextInjector) BuildSystemPrompt(base string) string {
    var prompt strings.Builder
    prompt.WriteString(base)

    learning := c.learning.GetActive()
    instructions := c.instructions.GetActive()

    if len(instructions) > 0 || len(learning) > 0 {
        prompt.WriteString("\n\n# Active Context\n")

        if len(instructions) > 0 {
            prompt.WriteString("\n## Standing Instructions\n")
            for i, instr := range instructions {
                prompt.WriteString(fmt.Sprintf("%d. **%s** (trigger: `%s`, action: `%s`)\n",
                    i+1, instr.ID, instr.Trigger, instr.Action))
                if instr.Body != "" {
                    prompt.WriteString(fmt.Sprintf("   _%s_\n", instr.Body))
                }
            }
        }

        if len(learning) > 0 {
            prompt.WriteString("\n## Learned Patterns\n")
            for i, p := range learning {
                prompt.WriteString(fmt.Sprintf("%d. %s (confidence: %.2f, type: %s)\n",
                    i+1, p.Description, p.Confidence, p.Type))
            }
        }

        prompt.WriteString("\nWhen triggers occur, execute associated actions automatically.\n")
    }

    return prompt.String()
}
```

**Wiring:**
```go
// In AgentLoop.buildSystemPrompt():
prompt := c.contextInjector.BuildSystemPrompt(l.spec.Purpose)
return prompt
```

**Testing:**
- Test context injection (instructions + learning)
- Test format rendering
- Test empty case (no instructions/learning)

---

#### 4.3 Comprehensive Testing

**Unit Tests:**
- Parser (all trigger types, action types, scope detection)
- Store (tiered discovery, save/load, shadowing)
- Verifier (tool existence, risk assessment, known-safe)
- Scheduler (cron → job conversion)
- Listeners (pattern matching, action execution)
- Git hooks (generation, execution)

**Integration Tests:**
- Full pipeline: add instruction → trigger → execute
- Persistence: restart daemon → instructions loaded
- Confirmation flow: high-risk → confirm → execute

**E2E Tests:**
| Scenario | Steps | Expected |
|----------|-------|----------|
| Go test automation | Add instruction → Write Go file → Observe test run | Tests executed |
| Daily summary | Add cron instruction → Wait/mock time → Check summary | Summary generated |
| Git pre-commit | Add hook instruction → Commit → Hook runs | Linter executed |
| Research docs | Add intent instruction → Ask API question → Docs fetched | Researcher agent triggered |

---

#### 4.4 Documentation

**Files:**
- `docs/workflows/user-instructions.md` — Feature specification
- `docs/reference/cli/instructions.md` — CLI reference
- `docs/concepts/instructions.md` — Conceptual guide
- `docs/tutorial/automate-tasks.md` — Tutorial with examples

**CLI Reference Structure:**
```markdown
# meept instructions

Manage user instructions (automation rules).

## Subcommands

### list
List all active instructions.

```bash
meept instructions list [--scope=project|global]
```

### add
Add a new instruction from natural language.

```bash
meept instructions add "Always run tests after I touch Go files"
```

### delete
Remove an instruction by ID.

```bash
meept instructions delete run-tests-after-go-changes
```

### preview
Preview how an instruction would be parsed.

```bash
meept instructions preview "Every morning at 9am, summarize my conversations"
```
```

**Conceptual Guide Topics:**
- What are User Instructions?
- Trigger types (cron, post-hook, event, intent, git)
- Action types (shell, memory, agent, notification)
- Security and confirmation
- Tiered storage (project vs global)
- Best practices and examples

---

### Deliverables (Phase 4)

- [ ] Q Agent integration (suggestion notifications)
- [ ] Learning System integration (merged context)
- [ ] Unit tests (parser, store, verifier, scheduler, listeners)
- [ ] Integration tests (full pipeline, persistence, confirmation)
- [ ] E2E tests (4 scenarios)
- [ ] Documentation (feature spec, CLI reference, conceptual guide, tutorial)
- [ ] Update existing docs (reference Scheduler, Learning integration)
- [ ] **CONTRACT VERIFICATION:** Pass 1 (anti-stub scan), Pass 2 (Provides/Consumes wired), Pass 3 (Q Agent + Learning integration + E2E tests pass)

---

## Success Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Instructions added per user (week 1) | >3 | Analytics |
| Instruction execution success rate | >95% | Logs |
| Time-to-first-instruction | <2 min | User testing |
| Trigger latency (event → action) | <500ms | Metrics |
| User retention (4-week) | >80% | Analytics |

---

## Risk Mitigation

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| NL parsing fails for complex input | Medium | Medium | Fall back to LLM parsing; manual override |
| Users create dangerous instructions | Low | High | Security gates + confirmation dialogs |
| Performance impact (many listeners) | Low | Low | Lazy loading; instruction limits |
| Conflicts between instructions | Medium | Medium | Execute both; log conflicts |
| Instructions lost on upgrade | Low | Medium | Persist to stable paths; migration script |

---

## Timeline Summary

| Phase | Duration | Deliverables |
|-------|----------|--------------|
| **Phase 1: Core Infrastructure** | Days 1-5 | Parser, Store, Handler, CLI |
| **Phase 2: Trigger Wiring** | Days 6-10 | Scheduler, Listeners, Git hooks |
| **Phase 3: UI/API + Security** | Days 11-15 | REST API, TUI, Security gates |
| **Phase 4: Integration + Docs** | Days 16-20 | Q Agent, Learning, Tests, Docs |

**Total:** 20 days (4 weeks)

---

## GitHub Issues (Phase 1 Breakdown)

Create these issues for tracking. Each issue MUST include the contract verification checklist from its respective phase.

| Issue | Task | Contract Verification Tasks |
|-------|------|----------------------------|
| #XXX | Add `IntentInstruction` type | Anti-stub: grep `return.*IntentInstruction{}` absent; Test: intent routes to instruction handler |
| #XXX | Implement `InstructionParser` | Anti-stub: body >10 lines, no `return nil, nil`; Test: table-driven trigger/action extraction |
| #XXX | Create `UserInstructionStore` | Anti-stub: `Discovery()`, `Save()`, `Delete()` implemented; Test: tier shadowing round-trip |
| #XXX | Implement `InstructionHandler` | Anti-stub: all 5 bus subscriptions wired; Test: add→list→delete flow |
| #XXX | Add `InstructionVerifier` | Anti-stub: `Verify()`, `assessRisk()` implemented; Test: known-safe vs dangerous |
| #XXX | CLI commands | Anti-stub: all 5 commands (`add`, `list`, `delete`, `show`, `preview`) work; Manual: daemon running |
| #XXX | Daemon integration | Anti-stub: `InstructionHandler.Start(ctx)` called; Test: handler starts on daemon boot |

---

## Three-Pass Verifier Protocol

Before marking any phase complete, run the three-pass protocol from `contract-driven-verification`:

### Pass 1: Anti-Stub Scan (Gate — any hit = BLOCKED)

```bash
echo "=== PASS 1: Anti-Stub Scan ==="
# Run all anti-completion greps from the phase contract
for pattern in "${ANTI_COMPLETION_PATTERNS[@]}"; do
  echo "--- Checking: $pattern ---"
  grep -rn "$pattern" "$PHASE_FILES" && echo "HIT: $pattern at line" || echo "clean"
done
```

**Any hit = BLOCKED.** Output: `BLOCKED: Phase N incomplete - anti-completion signal matched: [pattern] at [file:line]`

### Pass 2: Contract Verification (Gate — any missing wiring = BLOCKED)

```bash
echo "=== PASS 2: Contract Verification ==="
# Check Provides exist
grep -rn "func.*SyncCronInstructions" internal/scheduler/ || echo "MISSING: SyncCronInstructions"
# Check Consumes wired
grep -rn "instructionStore.GetActive" internal/scheduler/instructions.go || echo "MISSING: not using Phase 1 contract"
# Check Layer 7: data flow
grep -rn "result.*Execute\|Execute.*result" internal/agent/ || echo "MISSING: return value not consumed"
```

**Any missing wiring = BLOCKED.** Output: `BLOCKED: Phase N incomplete - [contract] not wired: [details]`

### Pass 3: Cumulative Regression (Gate — any failure = BLOCKED)

```bash
echo "=== PASS 3: Cumulative Regression ==="
go test ./internal/phase1/... -run TestPhase1E2E -v || echo "REGRESSION: Phase 1 broken"
go test ./internal/phase2/... -run TestPhase2E2E -v || echo "REGRESSION: Phase 2 broken"
go test ./internal/phaseN/... -run TestPhaseNE2E -v || echo "FAILED: Phase N acceptance test"
go build ./... || echo "BUILD FAILED"
```

**Any failure = BLOCKED.** Output: `BLOCKED: Phase N incomplete - [regression/failure]: [details]`

### VERIFIED Output Format

Only after all three passes:

```
VERIFIED: Phase N complete
- Pass 1 (anti-stub): 0 hits across N patterns
- Pass 2 (contracts): all Provides exist, all Consumes wired, Layer 7 data flow confirmed
- Pass 3 (regression): all prior phase acceptance tests pass, build passes
```

---

## Notes for Implementers

1. **Reuse existing patterns:** Follow the same tiered discovery as skills (`.meept/skills/` → `~/.meept/skills/`)
2. **Test parsing extensively:** NL parsing is the most failure-prone component; use table-driven tests
3. **Security first:** Always validate tool existence and assess risk before saving
4. **Start simple:** Begin with pattern matching; add LLM fallback later
5. **Integration points:** Coordinate with Q Agent and Learning teams for seamless experience
