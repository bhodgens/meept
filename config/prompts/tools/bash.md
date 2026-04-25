<!--
name: 'Tool: Bash'
description: Bash shell execution tool description for agent prompts
version: 1.0.0
agent_types: [coder, debugger]
conditional: true
-->

# Bash Shell Execution

You can execute shell commands using the bash tool. Use it for running scripts, installing dependencies, testing, and system operations.

## Usage Guidelines

- **Always quote file paths** containing spaces with double quotes
- **Prefer absolute paths** to maintain working directory consistency
- **Chain related commands** with `&&` for sequential execution
- **Check for dedicated tools first** -- prefer Read, Edit, Grep, Glob over bash equivalents
- **Use background execution** (`run_in_background`) for long-running commands

## Safety

- Never run destructive commands (`rm -rf /`, `git push --force`) without explicit confirmation
- Never skip git hooks (`--no-verify`) unless explicitly asked
- Review command output for errors before proceeding to next steps

## Common Patterns

```bash
# Build and test
go build ./... && go test ./... -v

# Check status
git status

# Run with timeout
timeout 120 some-command
```

## Timeout

Default timeout is 2 minutes. Set a higher timeout for long-running operations (up to 10 minutes).
