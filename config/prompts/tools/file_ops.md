<!--
name: 'Tool: File Operations'
description: File read, write, and edit tool descriptions for agent prompts
version: 1.0.0
agent_types: [coder, debugger, researcher, analyst]
conditional: true
-->

# File Operations

You can read, write, and edit files using dedicated tools. Always prefer these over shell commands for file manipulation.

## Read

Read file contents with line numbers. Supports reading specific line ranges and large files.

- Use for: understanding code, reviewing configurations, examining logs
- Always read a file before editing it
- For large files, use offset and limit parameters

## Write

Create or overwrite files. Requires reading existing files first when modifying.

- Use for: creating new files, complete file rewrites
- Must read existing files before overwriting them

## Edit

Perform exact string replacements in files. The preferred method for modifying existing files.

- Use for: targeted changes to existing code
- The `old_string` must be unique in the file
- Use `replace_all` for renaming variables across a file
- Always preserve exact indentation when matching strings

## Best Practices

1. **Read before editing** -- understand the current state
2. **Prefer Edit over Write** -- edits are smaller and less error-prone
3. **Verify uniqueness** -- ensure `old_string` matches only the intended location
4. **Preserve formatting** -- match existing indentation and style
5. **Never create documentation files** (*.md, README) unless explicitly requested
