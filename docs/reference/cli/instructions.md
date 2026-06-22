# meept instructions

Manage user instructions (automation rules).

## Synopsis

```bash
meept instructions <command> [arguments]
```

## Description

User instructions enable natural language automation in Meept. You can instruct the system to "always do X when Y happens" using plain English.

**Examples:**
- "Always run tests after I touch Go files"
- "Never commit without running the linter"
- "Every morning at 9am, summarize my conversations"
- "Whenever I ask about APIs, fetch the documentation first"

Instructions are stored with tiered priority:
1. Project-local (`.meept/instructions/`) - highest priority
2. User-global (`~/.meept/instructions/`)
3. System-wide (`~/.config/meept/instructions/`) - lowest priority

## Subcommands

### list

List all active instructions.

```bash
meept instructions list [--scope=project|global]
```

**Flags:**
- `--scope` - Filter by scope (`project` or `global`)

**Example output:**
```
ID                              Trigger                              Action
run-tests-after-go              post_tool_complete:write_file:*.go   shell_execute
daily-summary                   cron:0 9 * * *                       agent_trigger
```

### add

Add a new instruction from natural language.

```bash
meept instructions add "<natural language input>" [--tier=project|user|system] [--force]
```

**Flags:**
- `--tier` - Storage tier (default: project)
- `--force` - Skip confirmation for high-risk instructions

**Examples:**
```bash
# Simple automation
meept instructions add "Always run go fmt after I save Go files"

# Scheduled task
meept instructions add "Every day at 5pm, summarize my conversations"

# With confirmation bypass (use carefully)
meept instructions add "Always run rm -rf /tmp/* daily" --force
```

**Response:**
On success, displays the parsed instruction with trigger and action details.

### preview

Preview how an instruction would be parsed without saving.

```bash
meept instructions preview "<natural language input>"
```

**Example output:**
```
Input: Every morning at 9am, summarize my conversations

Parsed:
  Trigger Type: cron
  Trigger Pattern: 0 9 * * *
  Action: agent_trigger
  Scope: global
  Priority: normal
  Confidence: 0.85

Confirmation Required: No
```

### show

Show detailed information about a specific instruction.

```bash
meept instructions show <instruction-id>
```

**Example output:**
```
ID: run-tests-after-go
Trigger: post_tool_complete:write_file:*.go
Action: shell_execute
Action Args:
  command: go test ./...
  timeout: 60s
Enabled: true
Scope: project
Priority: normal
Created: 2026-06-22T10:30:00Z
```

### delete

Remove an instruction by ID.

```bash
meept instructions delete <instruction-id>
```

**Example:**
```bash
meept instructions delete run-tests-after-go
```

### enable / disable

Enable or disable an instruction without deleting it.

```bash
meept instructions enable <instruction-id>
meept instructions disable <instruction-id>
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Invalid input or parse error |
| 2 | Validation failed (tool not found, risk too high) |
| 3 | Instruction not found |
| 4 | Daemon connection error |

## Security

Instructions are validated before saving:

**Risk Levels:**
- **Low** - No confirmation required (e.g., `go test`, `git status`)
- **Medium** - Confirmation required (e.g., `git push`, unknown commands)
- **High** - Explicit confirmation + warning (e.g., `rm -rf`, `curl | bash`, `sudo`)

**Blocked patterns:**
- `rm -rf /`
- `curl ... | bash`
- `sudo ...`
- `chmod 777`

Use `--force` to bypass confirmation (not recommended for high-risk instructions).

## RPC Methods

The CLI uses these RPC methods internally:

| CLI Command | RPC Method |
|-------------|------------|
| `list` | `instruction.list` |
| `add` | `instruction.add` |
| `preview` | `instruction.preview` |
| `show` | `instruction.get` |
| `delete` | `instruction.delete` |
| `enable` | `instruction.set_enabled` |

## HTTP API

If the daemon HTTP transport is enabled, instructions can be managed via REST API:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/instructions` | GET | List all instructions |
| `/api/v1/instructions` | POST | Create instruction |
| `/api/v1/instructions/:id` | GET | Get instruction |
| `/api/v1/instructions/:id` | PUT | Update instruction |
| `/api/v1/instructions/:id` | DELETE | Delete instruction |
| `/api/v1/instructions/preview` | POST | Preview parsed instruction |

See `docs/reference/http-api.md` for full API documentation.

## Related Commands

- `meept config` - Edit configuration files
- `meept selfimprove` - Self-improvement system
- `meept chat` - Interactive chat mode

## Related Documentation

- Feature Spec: `docs/workflows/user-instructions.md`
- Conceptual Guide: `docs/concepts/instructions.md`
- Implementation Plan: `docs/superpowers/plans/2026-06-21-user-instructions-implementation.md`

## Examples

### Example 1: Go Test Automation

```bash
# Add instruction
meept instructions add "Always run tests after I touch Go files"

# Verify it was created
meept instructions list

# Preview what was parsed
meept instructions preview "Always run tests after I touch Go files"
```

### Example 2: Daily Summary

```bash
# Schedule daily summary at 9am
meept instructions add "Every morning at 9am, summarize my conversations"

# Check the cron schedule
meept instructions show daily-summary
# Trigger: cron:0 9 * * *
```

### Example 3: Git Pre-commit Hook

```bash
# Add pre-commit linting
meept instructions add "Before committing, run golangci-lint"

# Verify hook was generated
ls -la .git/hooks/pre-commit-user
```

### Example 4: High-Risk Command

```bash
# This will prompt for confirmation
meept instructions add "Always force push after commit"

# To bypass confirmation (use carefully)
meept instructions add "Always force push after commit" --force
```

## Troubleshooting

### "Parse error: invalid instruction"

The natural language input couldn't be parsed. Try:
- Using clearer trigger words: "always", "every", "whenever"
- Being more specific about the action
- Using `preview` to see how it's being parsed

### "Validation failed: tool not found"

The action tool doesn't exist. Valid tools:
- `shell_execute` - Run shell commands
- `agent_trigger` - Trigger specialist agent
- `memory_retain` - Save to memory
- `notification` - Send notification
- `file_write` - Write file
- `git_commit` - Git commit

### "Daemon not running"

Start the daemon:
```bash
make go-daemon
# or
./bin/meept-daemon
```

### Instruction not executing

1. Check if it's enabled: `meept instructions show <id>`
2. Verify trigger pattern matches your action
3. Check daemon logs: `tail -f ~/.meept/logs/daemon.log | grep instruction`
