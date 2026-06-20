# Dynamic Tool Routing

## Overview
Dynamic tool routing enables Meept agents to discover and execute tools based on their capabilities and permissions. Tools are matched to agents dynamically, with caching for performance and MCP integration for external tool support.

## Problem
Without dynamic routing, agents would need hardcoded tool access, limiting flexibility and requiring code changes for new tools. Dynamic routing allows:
- Agents to discover tools at runtime
- Permission-based tool access control
- Integration of external tools via MCP
- Caching for performance optimization

## Behavior

### Tool Discovery
1. **Tool Registration**: Tools register with the system via the tool registry
2. **Agent Capability Matching**: Agents are matched to tools based on declared capabilities
3. **Permission Checking**: Security engine validates tool access permissions
4. **Caching**: Tool metadata is cached for performance

### Tool Execution Flow
```
Agent Request → Tool Registry → Security Check → Tool Execution → Result
```

### MCP Integration
- MCP servers register tools dynamically
- Tools are discovered via MCP protocol
- External tools integrate seamlessly with built-in tools

## MCP Default Catalog

Meept ships a default catalog of 21 preconfigured MCP (Model Context Protocol) servers in `config/mcp_servers.json5`. The template is copied to `~/.meept/mcp_servers.json5` on `make install` if no file exists there yet. Each entry is fully configured with the correct command (`npx` or `uvx` as appropriate), environment variables, category, and description.

### Default Enabled Set

Only the zero-config servers are enabled by default (no API keys or external services required):

| server | runtime | category | purpose |
|--------|---------|----------|---------|
| `fetch` | uvx | network | general-purpose http fetcher |
| `git` | uvx | vcs | local git repo operations (log, diff, blame) |
| `memory` | npx | data | local knowledge graph store |
| `sequential-thinking` | npx | reasoning | step-wise reasoning scratchpad |

The remaining 17 servers ship `enabled: false` because they need API keys, OAuth credentials, or external daemons. Enable only the ones you want.

### Enabling a Server

Three surfaces toggle the `enabled` flag:

1. **Edit the JSON5 file directly** — set `enabled: true` on the entry and fill in any required env vars, then restart the daemon (or trigger a config reload).
2. **TUI** — press `ctl-x o` (or type `/mcp`) to open the mcp servers menu. Select a row and press `e` to toggle enabled. The toggle persists and reloads immediately.
3. **Menubar app** — open settings, go to the "tools" tab, and flip the toggle on a row.

Toggling via TUI or menubar writes the change atomically to `~/.meept/mcp_servers.json5` (via `SaveMCPConfig`'s temp-file + rename) and triggers `Manager.Reload`, which starts newly-enabled servers and stops newly-disabled ones without restarting the daemon.

### Env Var Placeholders (`${VAR}`)

Env values in the catalog use `${VAR}` placeholders. Meept does not expand these itself; they are passed through to the subprocess environment at transport-creation time inside `Manager.StartServer`. Export the env vars in your shell before starting the daemon:

```bash
export GITHUB_TOKEN="ghp_xxx"
./bin/meept-daemon -f
```

The `${VAR:-default}` shell-default syntax is also supported. Unknown env vars expand to the empty string.

### Runtime States

Each configured server has a runtime state tracked in memory (resets on daemon restart):

| state | meaning |
|-------|---------|
| `active` | connected and ready to serve tool calls |
| `inactive` | enabled but not yet started |
| `error` | enabled, but failed to start or not connected |
| `disabled` | `enabled: false`; skipped at startup and on reload |

`CallTool` invocations increment the per-server `requests` counter (success + failure). Failed invocations increment `errors` and populate `last_error` / `last_error_at`. The daemon's health monitor flips enabled-but-disconnected servers to `error` every 60 seconds.

### Example Catalog Entry

```json5
{
  "name": "github",
  "enabled": false,
  "category": "vcs",
  "description": "github repos, issues, prs",
  "type": "stdio",
  "command": ["npx", "-y", "@modelcontextprotocol/server-github"],
  "env": {
    "GITHUB_PERSONAL_ACCESS_TOKEN": "${GITHUB_TOKEN}",
  },
}
```

### Management APIs

The catalog is reachable from several surfaces:

- RPC: `mcp.list` returns all `ServerStatusEntry` items; `mcp.set_enabled` toggles one server.
- HTTP: `GET /api/v1/mcp/servers` and `PUT /api/v1/mcp/servers/{name}/enabled` (see [http-api reference](../reference/http-api.md)).
- TUI: `ctl-x o` keybinding or `/mcp` slash command.
- Menubar: settings → tools tab.

### Agent-Tool Matching
- Agents declare required capabilities
- Tools declare provided capabilities
- Registry finds optimal tool-agent matches
- Each agent gets a `FilteredToolRegistry` wrapping the global registry, exposing only tools in its `BaselineTools` + `AdditionalTools` lists

### Dynamic Tool Categories
- **Platform tools**: `platform_agents`, `platform_status`, `platform_tools`, `delegate_task`, `request_handoff` — available to all agents via baseline
- **File tools**: `file_read`, `file_write`, `file_delete`, `list_directory` — coder, debugger
- **Shell tools**: `shell_execute` — coder, debugger, committer
- **Memory tools**: `memory_store`, `memory_search`, `memory_get_context` — all agents
- **Collaboration tools**: `workspace_yield`, `initiate_collaboration` — pair/collab sessions

## Configuration

```toml
[tools]
enabled = true
cache_ttl_seconds = 300
mcp_enabled = true

[tools.mcp]
servers = [
  "~/.meept/mcp_servers.json"
]
auto_discover = true

[tools.security]
require_confirmation_high = true
require_confirmation_critical = true
block_financial = true
```

## Observability

### Logging
- Tool registration events
- Permission denials
- Execution failures
- Cache hits/misses

### Metrics
- Tool execution latency
- Cache hit rate
- Permission check results
- MCP tool discovery status

### Debug Info
- Available tools per agent
- Tool capability mappings
- MCP server connections

## Edge Cases

### Tool Not Found
- Returns clear error message
- Suggests similar tools if available
- Logs missing tool requests

### Permission Denied
- Security engine blocks execution
- Audit log records denial
- Agent receives permission error

### MCP Server Unavailable
- External tools marked as unavailable
- Automatic retry with backoff
- Graceful degradation to built-in tools

### Cache Invalidation
- Cache cleared on tool registration changes
- Manual cache clear via admin tools
- Time-based TTL for freshness