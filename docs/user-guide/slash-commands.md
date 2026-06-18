# Slash Commands User Guide

## Overview

Slash commands provide quick access to reusable prompts, skills, and automation workflows in Meept. Type `/` followed by a command name and optional arguments.

## Command Discovery

Commands are discovered from multiple locations (highest priority first):

1. `.meept/commands/*.md` - Project-local commands
2. `~/.meept/commands/*.md` - User-global commands
3. `~/.claude/commands/*.md` - Claude Code compatible commands

This means Claude Code slash commands work out-of-the-box in Meept.

## Using Commands

### Basic Syntax

```
/command-name [arguments]
```

Examples:
```
/research build a quantum computing timeline
/qa-docker api
/playwright-test http://localhost:3000
/skill code-review
```

### Argument Substitution

Commands support templating with positional and collective arguments:

| Pattern | Meaning | Example |
|---------|---------|---------|
| `$ARGUMENTS` | All arguments joined | `/cmd a b c` -> `a b c` |
| `$1`, `$2`, ... | Positional argument | `/cmd hello world` -> `$1=hello`, `$2=world` |
| `${@:N}` | Arguments from index N | `${@:2}` -> from 2nd onward |
| `${@:N:L}` | L arguments from index N | `${@:2:1}` -> one arg at index 2 |

## Built-in Commands

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/skill` | List or search installed skills |
| `/new`, `/clear` | Start fresh conversation |
| `/retry` | Retry last response |
| `/undo` | Remove last exchange |
| `/usage` | Show token usage |
| `/stop` | Stop current work |
| `/status` | Show platform health |
| `/vim` | Toggle vim mode |
| `/tasks` | List tasks |
| `/cancel` | Cancel a task |
| `/amend` | Amend a task |
| `/interrupt` | Interrupt a task |
| `/diff` | Show git diff |
| `/model` | Show or switch model |
| `/compact` | Compact context |
| `/edit` | Edit a file |
| `/plan` | Enter planning mode |
| `/review` | Review changes |
| `/project` | Manage projects |

## Custom Commands

### Creating Commands

Create a `.md` file in `~/.meept/commands/`:

```markdown
---
name: summarize
description: summarize text concisely
---
Summarize the following in 2-3 sentences:

$ARGUMENTS
```

### Claude Code Compatibility

Meept supports Claude Code's command format. Commands in `~/.claude/commands/` work automatically:

```markdown
---
name: explain
description: explain code step by step
---
Explain this code:

$ARGUMENTS
```

## Pre-built Templates

Meept includes these commands by default:

### /research
In-depth research with Open Knowledge Format output.

Usage: `/research <topic>`

Creates structured findings in `docs/knowledge/{topic}/` with:
- Executive summary
- Detailed findings
- Annotated bibliography
- OKF metadata

### /qa-docker
Docker-based QA automation for recent commits.

Usage: `/qa-docker [scope]`

Runs:
1. Git diff analysis
2. Docker test environment setup
3. Test suite execution
4. Results reporting

### /playwright-test
Playwright E2E test execution.

Usage: `/playwright-test [url]`

Features:
- Automatic browser setup
- HTML report generation
- Screenshot and trace capture
- Result comparison

## Skills Integration

The `/skill` command lists all installed skills:

```
/skill              # List all skills
/skill code-review  # Show skill details
/skill search code  # Fuzzy search
```

Skill names also appear in slash autocomplete when typing `/`.

## Installing Custom Commands

### System-wide
```bash
cp my-command.md ~/.config/meept/commands/
```

### User-global
```bash
cp my-command.md ~/.meept/commands/
```

### Project-local
```bash
mkdir -p .meept/commands
cp my-command.md .meept/commands/
```

## Template Format Reference

```markdown
---
name: command-name          # Required: unique identifier
description: Does something # Required: short description
arguments:                  # Optional: documented arguments
  - arg1: First argument
  - arg2: Second argument
---
# Command body

Use $1 for first argument
Use $2 for second argument
Use $ARGUMENTS for all arguments

$@
```

## Troubleshooting

### Command not found
- Check discovery paths: `.meept/commands/`, `~/.meept/commands/`, `~/.claude/commands/`
- Ensure file has `.md` extension
- Verify frontmatter has `name:` field

### Arguments not substituted
- Use `$1`, `$2`, etc. for positional
- Use `$ARGUMENTS` for all arguments joined
- Check spacing: `$1` not `$ 1`

### Skills not showing
- Ensure skills are installed: `meept skills list`
- Restart TUI to reload skills
- Check skill registry is initialized
