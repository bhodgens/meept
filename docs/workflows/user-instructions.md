# User Instructions

**Status:** Implemented (Phases 1-2 complete, Phase 3 partial, Phase 4 partial)

## Overview

User Instructions enable natural language automation in Meept. Users can instruct the system to "always do X when Y happens" using plain English.

**Example inputs:**
- "Always run tests after I touch Go files"
- "Never commit without running the linter"
- "Every morning at 9am, summarize my conversations"
- "Whenever I ask about APIs, fetch the documentation first"

## Architecture

```
User Input (natural language)
    → Dispatcher (IntentInstruction classification)
    → InstructionParser (NL → structured rule)
    → InstructionVerifier (tool exists? risk level?)
    → UserInstructionStore (persist to tiered storage)
    → Execution triggers:
        - Scheduler (cron jobs)
        - Bus Listeners (post-hook events)
        - Git Hooks (pre/post commit)
        - Intent Router (matching triggers)
        - Context Injector (system prompt enrichment)
```

## Trigger Types

| Type | Pattern | Example |
|------|---------|---------|
| `cron` | Time-based schedule | "Every day at 9am" |
| `post_hook` | After tool completion | "After write_file:\*.go" |
| `event` | Bus event | "When session starts" |
| `intent` | Intent match | "When user asks about APIs" |
| `git` | Git hook | "Before commit", "After commit" |

## Action Types

| Type | Description | Risk Level |
|------|-------------|------------|
| `shell_execute` | Run shell command | Medium-High (depends on command) |
| `agent_trigger` | Trigger specialist agent | Medium |
| `memory_retain` | Save to memory | Low |
| `notification` | Send notification | Low |
| `git_commit` | Git commit | Medium |
| `file_write` | Write file | Medium |

## Security

Instructions are validated before saving:

1. **Tool existence check** - Action tool must be registered
2. **Risk assessment** - Commands categorized as low/medium/high risk
3. **Confirmation required** - Medium and high risk need explicit approval

**High-risk patterns (blocked):**
- `rm -rf`, `curl | bash`, `sudo`, `chmod 777`

**Known-safe commands (low risk):**
- `go test ./...`, `go build ./...`, `git status`, `ls`, `cat`

## Tiered Storage

Instructions are stored with priority shadowing:

```
.meept/instructions/          # Project-local (highest priority)
~/.meept/instructions/        # User-global
~/.config/meept/instructions/ # System-wide (lowest)
```

Same ID in multiple tiers: project-local wins.

## CLI Commands

```bash
# List all active instructions
meept instructions list

# Add new instruction
meept instructions add "Always run tests after I touch Go files"

# Show instruction details
meept instructions show <id>

# Delete instruction
meept instructions delete <id>

# Preview parsed instruction (dry-run)
meept instructions preview "Every morning at 9am, run linter"
```

## HTTP API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/instructions` | List all instructions |
| POST | `/api/v1/instructions` | Create instruction |
| GET | `/api/v1/instructions/:id` | Get instruction |
| PUT | `/api/v1/instructions/:id` | Update instruction |
| DELETE | `/api/v1/instructions/:id` | Delete instruction |
| POST | `/api/v1/instructions/preview` | Preview parsed instruction |

## Integration Points

### Dispatcher (`internal/agent/dispatcher.go`)

- `IntentInstruction` intent type for NL automation requests
- `isInstructionInput()` detects keywords: "always", "never", "whenever"
- `SetInstructionStore()` wires store for action attachment
- `SetInstructionParser()` wires parser for NL parsing

### Context Injector (`internal/agent/context_injector.go`)

- `BuildSystemPrompt()` merges Learning patterns + User Instructions
- Injected as "## Standing Instructions" in system prompt
- Active instructions visible to all agents

### Scheduler (`internal/scheduler/instructions.go`)

- `SyncCronInstructions()` loads cron-type instructions as jobs
- Converts to `AgentJob` or `ShellJob` based on action type

### Bus Listeners (`internal/agent/instruction_listeners.go`)

- Subscribes to `tool.completed`, `task.completed`, etc.
- Matches trigger patterns against events
- Executes actions on match

### Git Hooks (`internal/preferences/git_hooks.go`)

- `GeneratePreCommitHook()` creates `.git/hooks/pre-commit-user`
- `GeneratePostCommitHook()` creates `.git/hooks/post-commit-user`
- Hooks dispatch to RPC `instruction.execute_git_hook`

## Implementation Status

| Phase | Status | Completion |
|-------|--------|------------|
| Phase 1: Core Infrastructure | Complete | 85% |
| Phase 2: Trigger Wiring | Complete | 100% |
| Phase 3: UI/API + Security | Partial | 75% |
| Phase 4: Integration + Docs | Partial | 25% |

**Missing components:**
- TUI confirmation dialog (Phase 3)
- Q Agent integration for recommendations (Phase 4)
- Full documentation suite (Phase 4)

## Related Documents

- Spec: `docs/superpowers/specs/2026-06-21-user-instructions-design.md`
- Plan: `docs/superpowers/plans/2026-06-21-user-instructions-implementation.md`
- Concept: `docs/concepts/instructions.md` (TODO)
