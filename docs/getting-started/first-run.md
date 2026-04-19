# First Run

What happens during your first Meept session and how to verify everything is working.

## Daemon Startup Sequence

When you run `./bin/meept-daemon -f`, the daemon initializes in this order:

1. **Config loading** — Reads `~/.meept/meept.toml` and `~/.meept/models.json5`
2. **Component registry** — Registers all internal components
3. **RPC server** — Opens Unix socket at `~/.meept/meept.sock`
4. **Message bus** — Starts pub/sub system
5. **Agent registry** — Registers 8 specialist agents + 5 reviewers
6. **Tool registry** — Registers all built-in tools
7. **Memory system** — Opens SQLite database, loads existing memories
8. **Scheduler** — Loads scheduled jobs (if enabled)
9. **Ready** — Daemon accepts connections

## Verifying the Daemon

```bash
# Check daemon status
./bin/meept status
```

Expected output:
```
daemon: running
uptime: 2m15s
socket: ~/.meept/meept.sock
agents: 13 registered
tools: 17 registered
memory: 42 items
```

## First Chat Session

When you start `./bin/meept chat`, the TUI opens with:

- **Main area** — Chat messages with markdown rendering
- **Input bar** — Type messages (vim keybindings available)
- **Sidebar** — Agent activity panel (toggle with `Ctrl+S`)

### First Message Flow

```
You: "Hello, what can you do?"
  → RPC request to daemon
  → Message bus publishes chat.request
  → Dispatcher agent receives message
  → Dispatcher calls platform_agents to discover capabilities
  → Dispatcher responds with capability summary
  → Response delivered back through RPC
```

## Key Verification Checks

### 1. Agent Discovery Works

```
You: "What agents are available?"
```

The dispatcher should query `platform_agents` and list all registered agents with their purposes.

### 2. Tool Execution Works

```
You: "Read the file config/meept.toml"
```

This should be routed to the coder agent, which uses `file_read` to display the file.

### 3. Memory Storage Works

```
You: "Remember that my preferred language is Go"
```

Then in a new session:

```
You: "What is my preferred programming language?"
```

The agent should recall the stored preference.

### 4. Task Routing Works

```
You: "Create a task to refactor the auth module"
```

The planner agent creates a task with steps. Check with `./bin/meept tasks list`.

## TUI Keybindings

| Key | Action |
|-----|--------|
| `Enter` | Send message (in insert mode) |
| `Esc` | Switch to normal mode |
| `i` | Enter insert mode |
| `:w` | Send message (vim-style) |
| `Ctrl+N` | New session |
| `Ctrl+R` | Rename session |
| `Ctrl+S` | Toggle sidebar |
| `Ctrl+C` | Quit |

## Log Files

If something isn't working, check the logs:

```bash
# Daemon logs (stdout in foreground mode)
# Or check the log file
ls ~/.meept/meept.log
```

## Next Steps

- [Troubleshooting](troubleshooting.md) — Common issues and fixes
- [Configuration](../configuration/index.md) — Customize agents, memory, and security
- [Concepts](../concepts/index.md) — Deep dive into architecture
