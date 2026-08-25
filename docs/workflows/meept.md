# meept CLI

The `meept` binary is the user-facing client for the daemon: chat, session
management, config, and operational commands.

## Command Reference

Command-level documentation lives beside each command's functional area:

| Command | Purpose | Docs |
|---------|---------|------|
| `meept chat` | Interactive/one-shot chat | [tui](tui.md) |
| `meept agents` | AI employee management | [employees](employees.md) |
| `meept plans` | Plan approval workflow | [collaborative-planning](collaborative-planning.md) |
| `meept projects` | Project binding/sync | [projects](projects.md) |
| `meept changes` | List/revert journaled staged writes | [change-journal](change-journal.md) |
| `meept config` | Config editor/getter/setter | [configuration index](../configuration/index.md) |
| `meept daemon start/stop/restart/status` | Daemon lifecycle | [daemon operations](#daemon-operations) |

## Daemon Operations

`meept daemon start` launches the daemon in the background; `stop` sends
SIGTERM and waits for graceful drain; `restart` stops then starts. Status
reports PID and uptime from the pidfile at `~/.meept/meept.pid`.

## See Also

- [change-journal](change-journal.md) for `meept changes list/revert`
- [AGENTS.md](../../AGENTS.md) for build/run conventions
