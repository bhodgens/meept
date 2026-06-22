# User Instructions

**User Instructions** are automated rules that execute actions when specific triggers occur. They enable natural language automation: "Always do X when Y happens."

## What Are User Instructions?

User Instructions transform Meept from a reactive assistant into a proactive automation engine. Instead of manually triggering actions every time, you define rules once and they execute automatically.

**Example:**
```
User: "Always run tests after I touch Go files"

System creates instruction:
  Trigger: post_tool_complete:write_file:*.go
  Action: shell_execute { command: "go test ./..." }
  Scope: project
```

## Anatomy of an Instruction

```yaml
id: run-tests-after-go-changes
trigger: post_tool_complete:write_file:*.go
action: shell_execute
action_args:
  command: "go test ./..."
  timeout: 60s
enabled: true
scope: project  # or "global"
priority: normal
created_at: 2026-06-22T10:30:00Z
```

### Fields

| Field | Description | Example |
|-------|-------------|---------|
| `id` | Unique identifier | `run-tests-after-go-changes` |
| `trigger` | What causes execution | `post_tool_complete:write_file:*.go` |
| `action` | What to execute | `shell_execute`, `agent_trigger` |
| `action_args` | Action parameters | `{ command: "go test ./..." }` |
| `enabled` | Whether instruction is active | `true` |
| `scope` | Where instruction applies | `project`, `global` |
| `priority` | Execution priority | `low`, `normal`, `high`, `critical` |

## Trigger Types

### 1. Cron (Time-based)

Execute on a schedule using cron syntax.

**Examples:**
- "Every day at 9am" → `cron:0 9 * * *`
- "Every Monday at 10am" → `cron:0 10 * * 1`
- "Every hour" → `cron:0 * * * *`

**Use cases:**
- Daily summaries
- Regular cleanup tasks
- Scheduled reports

### 2. Post-Hook (After Tool Completion)

Execute after a specific tool completes.

**Pattern:** `post_tool_complete:<tool>:<path_pattern>`

**Examples:**
- "After writing Go files" → `post_tool_complete:write_file:*.go`
- "After any file write" → `post_tool_complete:write_file:*`
- "After running tests" → `post_tool_complete:shell_execute:*test*`

**Use cases:**
- Auto-testing after code changes
- Linting after saves
- Notification after long-running tasks

### 3. Event (Bus Events)

Execute when a specific bus event fires.

**Pattern:** `event:<event_name>`

**Examples:**
- "When session starts" → `event:session.started`
- "When memory is stored" → `event:memory.stored`
- "When task completes" → `event:task.completed`

**Use cases:**
- Welcome messages on session start
- Auto-cleanup on task completion
- Cross-session notifications

### 4. Intent (Intent Matching)

Execute when user input matches a specific intent.

**Pattern:** `intent:<intent_type>`

**Examples:**
- "When user asks about APIs" → `intent:research`
- "When debugging starts" → `intent:debug`
- "When planning is requested" → `intent:plan`

**Use cases:**
- Auto-fetching docs for research questions
- Running diagnostics for debug requests
- Template injection for common tasks

### 5. Git (Git Hooks)

Execute before or after Git operations.

**Pattern:** `git_pre_commit`, `git_post_commit`

**Examples:**
- "Before committing" → `git_pre_commit`
- "After committing" → `git_post_commit`

**Use cases:**
- Pre-commit linting
- Post-commit notifications
- Auto-push after commits

## Action Types

### shell_execute

Run a shell command.

```yaml
action: shell_execute
action_args:
  command: "go test ./..."
  timeout: 60s  # optional, in seconds
```

**Risk levels:**
- **Low:** `go test`, `go build`, `git status`, `ls`, `cat`
- **Medium:** Unknown commands, `git push`, `chmod`
- **High:** `rm -rf`, `curl | bash`, `sudo`, `chmod 777`

### agent_trigger

Trigger a specialist agent.

```yaml
action: agent_trigger
action_args:
  agent_id: "researcher"
  prompt: "Fetch latest API documentation"
```

### memory_retain

Save information to memory.

```yaml
action: memory_retain
action_args:
  category: "preferences"
  content: "User prefers tab indentation"
```

### notification

Send a notification.

```yaml
action: notification
action_args:
  channel: "telegram"
  message: "Build completed successfully"
```

## Security Model

Instructions are validated at save time:

### 1. Tool Existence Check

The action tool must be registered:
- Built-in tools: `shell_execute`, `memory_retain`, `notification`, `agent_trigger`
- Custom tools: Must exist in tool registry

### 2. Risk Assessment

Commands are analyzed for potential harm:

**High Risk (requires explicit confirmation + warning):**
```
rm -rf /path
curl http://... | bash
sudo apt-get install
chmod 777 /path
dd if=/dev/zero
```

**Medium Risk (requires confirmation):**
```
git push
git reset --hard
chmod 644 file
file_write operations
```

**Low Risk (no confirmation):**
```
go test ./...
go build ./...
git status
git diff
ls, cat, echo, head, tail
```

### 3. Confirmation Dialog

For medium/high risk instructions:
- TUI shows risk level and command details
- User must explicitly confirm
- `--force` flag bypasses confirmation (CLI only)

## Tiered Storage

Instructions are stored in three tiers with priority shadowing:

```
Project (.meept/instructions/)
    ↑ Highest priority - shadows lower tiers
User (~/.meept/instructions/)
    ↑ Medium priority
System (~/.config/meept/instructions/)
    ↑ Lowest priority - default fallback
```

**Shadowing behavior:**
- Same instruction ID in multiple tiers: project wins
- Disabling in project tier: user tier becomes active
- Deleting in all tiers: instruction removed

**Use cases:**
- **Project tier:** Go test automation for specific repo
- **User tier:** Personal preferences (always use tabs)
- **System tier:** Organization-wide policies

## Natural Language Parsing

The `InstructionParser` converts natural language to structured instructions:

### Pattern Matching

```
"Always run tests after I touch Go files"
  → Trigger: post_tool_complete:write_file:*.go
  → Action: shell_execute { command: "go test ./..." }

"Every morning at 9am, summarize my conversations"
  → Trigger: cron:0 9 * * *
  → Action: agent_trigger { agent_id: "chat", prompt: "Summarize..." }
```

### Detection Keywords

The `Dispatcher.isInstructionInput()` detects automation requests:

```
"always", "never", "every time", "whenever"
"from now on", "remember to", "make sure to"
"automatically", "auto-"
```

## Context Injection

Active instructions are injected into system prompts:

```
# Active Context

## Standing Instructions
The following automated actions are configured and will execute when their triggers match:

1. **run-tests-after-go-changes** (trigger: `post_tool_complete:write_file:*.go`, action: `shell_execute`)
   _Scope: This project only_
2. **daily-summary** (trigger: `cron:0 9 * * *`, action: `agent_trigger`)

## Learned Patterns
...

When triggers occur, execute associated actions automatically.
```

## Best Practices

### 1. Start Simple

Begin with low-risk, high-frequency tasks:
```bash
meept instructions add "Always run go fmt after I save Go files"
```

### 2. Test with Preview

```bash
meept instructions preview "Every day at 9am, run linter"
# Review parsed trigger/action before saving
```

### 3. Use Project Scope for Repo-Specific Rules

```bash
meept instructions add "Run tests after Go changes" --scope=project
```

### 4. Avoid Redundant Instructions

Don't create instructions for things Meept already does automatically.

### 5. Review Periodically

```bash
meept instructions list
# Remove unused or outdated instructions
```

## Troubleshooting

### Instruction Not Executing

1. **Check enabled status:**
   ```bash
   meept instructions show <id>
   ```

2. **Verify trigger pattern matches:**
   - For `post_hook`: Check tool name and path pattern
   - For `cron`: Validate cron syntax
   - For `intent`: Confirm intent type matches

3. **Check logs:**
   ```bash
   tail -f ~/.meept/logs/daemon.log | grep instruction
   ```

### False Trigger Matches

Narrow your patterns:
```
# Too broad: triggers on any file write
post_tool_complete:write_file:*

# Better: only Go files
post_tool_complete:write_file:*.go

# Best: specific directory
post_tool_complete:write_file:internal/agent/*.go
```

### High-Risk Command Blocked

Use explicit confirmation:
```bash
meept instructions add "Always force push after commit" --force
```

Or use safer alternatives:
```
# Instead of: rm -rf /tmp/*
# Use: find /tmp -type f -mtime +7 -delete
```

## Related Documents

- Feature Spec: `docs/workflows/user-instructions.md`
- Implementation Plan: `docs/superpowers/plans/2026-06-21-user-instructions-implementation.md`
- Design Spec: `docs/superpowers/specs/2026-06-21-user-instructions-design.md`
- CLI Reference: `docs/reference/cli/instructions.md` (TODO)
