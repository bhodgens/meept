# meept-daemon

The long-running process that owns sessions, the agent loop, the local LLM
runtimes and the RPC/HTTP transports. Everything the CLI and the GUI do goes
through it.

## Overview

`meept-daemon` is started by `meept daemon start` (or `meept-daemon -f` in the
foreground). It loads the configuration, builds its components, starts the
transports, and then serves requests until it is asked to stop.

```
meept daemon start     # background, writes ~/.meept/meept.log
meept-daemon -f        # foreground, logs to stderr
meept daemon stop      # graceful shutdown
meept daemon status    # running state, model, budget
```

Paths resolve under `$MEEPT_HOME` when that variable is set, otherwise under
`~/.meept`. See `docs/configuration/` for the full config reference.

## Problem

A daemon that outlives a shell is needed for sessions, schedules, queue jobs
and the local model endpoints to survive between commands. That long life is
also what makes its shutdown path load-bearing: the process spawns model
runtimes that hold ports and gigabytes of RAM, so a daemon that exits without
taking them along leaves the machine unusable for the next start.

## Behavior

Startup order that matters:

1. Configuration and components are built.
2. `StartupOrphanSweep` reaps runtimes left by an earlier hard kill (a
   leftover holds its endpoint port, and the spawn guard would refuse the
   spawn that has not been freed).
3. Local runtimes start (`ContainerManager.StartAll`).
4. Transports serve: Unix socket RPC (same-OS-user only) and, when
   configured, HTTP/WS.

Shutdown, on SIGINT/SIGTERM, stops owned runtimes through
`ContainerManager.StopAll` before the rest of the components. A runtime whose
endpoint sets `auto_stop_on_exit: false` is deliberately left running.

### Runtime supervision

Each runtime the daemon spawns runs under a supervisor process
(`internal/llm/supervisor.go`). The runtime keeps its original command line
and its own process group; the supervisor observes the daemon and, when the
daemon disappears, terminates the runtime's process group (SIGTERM, 10s
grace, then SIGKILL). This covers the hard kill that no shutdown handler can:
SIGKILL, a panic, or a terminal teardown.

- Parent loss is detected by a parent-death pipe (closes in the kernel when
  the daemon dies, including an unreaped zombie) plus a 2 second pid poll.
- The PID file names the runtime, not the supervisor, so `meept runtime
  status`, the ownership token and the orphan sweep keep addressing the
  process that holds the port.
- Per-endpoint escape hatch: `supervise: false`.
- The wrapper is only used by the daemon binary itself. A CLI or eval process
  that starts a runtime spawns it directly, because that process's exit is not
  the runtime's death.

See `docs/configuration/llm-lifecycle.md` for the spawn, health and reaping
rules in full.

## Configuration

| Key | Where | Effect |
|---|---|---|
| `transport.rpc` / `transport.http` | `meept.json5` | which transports run |
| `log_level` | `meept.json5` | log verbosity, case-insensitive; invalid values warn and fall back to info |
| `daemon.default_working_dir` | `meept.json5` | last-resort working directory for a chat or dispatched turn; empty means none |
| `providers.<name>.lifecycle` | `models.json5` | how a local runtime is spawned and stopped |
| `providers.<name>.options.tool_choice` | `models.json5` | `required` asks for a tool call on executor turns that carry tools |
| `supervise` | `models.json5` per endpoint | `false` disables the parent-death supervisor |
| `auto_stop_on_exit` | `models.json5` per endpoint | `false` leaves the runtime running after exit |

## Turn working directory

Every chat and dispatched turn resolves one working directory. Tools receive it
through the tool context, never from the daemon's own process CWD.

1. Session worktree path (a provisioned phase/session worktree)
2. Session project path
3. Session detection-context CWD (what `meept chat` sends as the client CWD)
4. The user's ACTIVE project
5. `daemon.default_working_dir` (empty by default)

When all five are empty, the filesystem tools fail with
`tools.ErrNoWorkingDir` ("no working directory for this session; pass an
explicit path") and the daemon logs a WARN naming the source as `none`. It never
synthesizes a project and never falls back to the daemon's own directory.

Sessions are bound at creation to an explicit `project_id`, else the client CWD,
else the active project. A session with none of those stays unbound and is
logged at WARN.

## Chat reply guard

`internal/agent/reply_guard.go` replaces machine-shaped replies with a
user-language fallback, because the reply is the user's only window into the
daemon. Four rules, checked in this order:

| Rule | Shape |
|---|---|
| `raw_platform_json` | a raw `platform_*` payload document |
| `agents_header` | the agent roster's `## Available Agents` header |
| `tools_totals_line` | a catalog line such as `*Total: 75 tools*` |
| `tool_result_json` | pure JSON carrying a tool-result key (`memory_id`, `task_id`, `job_id`, `step_id`, `tool_result`, `tool_output`) |

The roster and tool-result rules always fire; the other two have a prose escape
hatch (a reply that is more than 40 percent running text passes through). Every
replacement emits exactly one WARN naming the rule, the matched token, the
agent, the intent, the session and conversation ids, and a bounded preview.

## Edge Cases

- `daemon already running` is refused at startup by the PID file, not by the
  socket.
- A runtime whose endpoint port is already served is never spawned into: the
  spawn is refused loudly instead of producing a healthy-looking runtime with
  no socket.
- A runtime started by another instance is adopted as observed-not-owned, so
  this daemon will not kill it.
- macOS has no parent-death signal; the supervisor exists precisely because
  of that. Windows gets no orphan sweep (no `ps`), so it relies on the
  supervisor and the graceful path.

## Related

- `docs/configuration/llm-lifecycle.md` - runtime spawn, health, orphan sweep
- `docs/workflows/classification-architecture.md` - the intent pipeline
- `docs/workflows/intent-routing.md` - lane to agent routing
