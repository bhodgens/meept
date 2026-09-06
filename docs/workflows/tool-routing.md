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

Meept ships a default catalog of 22 preconfigured MCP (Model Context Protocol) servers in `config/mcp_servers.json5`. The template is copied to `~/.meept/mcp_servers.json5` on `make install` if no file exists there yet. Each entry is fully configured with the correct command (`npx` or `uvx` as appropriate), environment variables, category, and description.

### Installing Missing MCP Dependencies

`meept doctor --fix --install-missing` audits the enabled stdio entries in
`~/.meept/mcp_servers.json5` for missing binaries and offers to install them.
For each missing server it prints the entry's `install_hint`, asks
`run this command? [y/N]`, and only on an explicit `y`/`yes` runs the hint
verbatim under `sh -c` (10 minute ceiling). Hints are never constructed by
meept, refusals and command failures skip to the next server, and the run
requires an interactive terminal. Answer `n` or press enter to skip any
command you would rather run yourself.

### MCP Security Considerations

> **Important:** MCP tools are external servers that return arbitrary content. As of 2026-06-23:
>
> | Protection | Status | Tracking |
> |------------|--------|----------|
> | Boundary marker wrapping | Gap (Phase 5) | `docs/plans/agent-security-gap-closure.md` |
> | Output sanitization | Gap (Phase 5) | Same as above |
> | Taint label propagation | Gap (Phase 5) | Same as above |
>
> **Until Phase 5 is complete:** Only enable MCP servers you trust. External MCP servers could potentially inject prompts that bypass agent constraints.
>
> See [Adversarial Input Defense](adversarial-input-defense.md) for the full security architecture.

### Default Enabled Set

Only the zero-config servers are enabled by default (no API keys or external services required):

| server | runtime | category | purpose |
|--------|---------|----------|---------|
| `sequential-thinking` | npx | reasoning | step-wise reasoning scratchpad |
| `everything` | npx | reasoning | MCP reference test server |
| `memory` | npx | data | local knowledge graph store |
| `fetch` | uvx | network | general-purpose http fetcher |
| `git` | uvx | vcs | local git repo operations (log, diff, blame) |
| `time` | uvx | data | timezone-aware time and conversion |

The remaining 15 servers ship `enabled: false` because they need API keys, OAuth credentials, external platform instances, or a natively-installed binary. Enable only the ones you want.

The `cua-driver` entry (category `automation`) adds background desktop computer-use via a native binary — install commands, enable steps, and its LOW/HIGH risk-rule table are documented under [Cua-Driver Computer-Use Integration](external-integrations.md#cua-driver-computer-use-integration).

The `obscura` entry (category `browser`) adds the Obscura headless browser engine — a Rust, V8-based, CDP-compatible browser purpose-built for agents, exposing the full `browser_*` tool family over stdio MCP. Install commands and enable steps are documented under [Obscura Browser Integration](external-integrations.md#obscura-browser-integration).

The `excel` entry (category `data`, **disabled by default**) is a higher-capability xlsx fallback (`uvx excel-mcp-server`: pivot tables, charts, conditional formatting) for when the built-in `spreadsheet_write` tool is not enough. It ships disabled because an MCP subprocess write bypasses the built-in tools' session fence and `pending_changes` review gate — enable it only for reports that genuinely need pivot-table-class output.

## Built-in Web & Output Tools

| tool | category | purpose |
|------|----------|---------|
| `web_fetch` | web | HTTP(S) fetch, HTML stripped to text; PDF responses are detected (content-type or `%PDF` magic bytes) and return a pointer to `pdf_read` instead of binary garbage |
| `web_search` | web | DuckDuckGo search |
| `pdf_read` | web | Read a PDF's text layer from a local path or URL (page ranges, char cap); scanned PDFs return an explicit "no text layer" note instead of failing |
| `spreadsheet_write` | filesystem | Write CSV or XLSX audit/tabular output (headers, typed cells, yellow row highlighting in xlsx) inside the session working dir |

All three web-side tools (`web_fetch`, `web_search`, `pdf_read`) run under the `[security.ssrf]` guard; `spreadsheet_write` writes resolve inside the session working-dir fence and refuse paths that escape it.

### Enabling a Server

Three surfaces toggle the `enabled` flag:

1. **Edit the JSON5 file directly** — set `enabled: true` on the entry and fill in any required env vars, then restart the platform (or trigger a config reload).
2. **Interactive config editor** — run `meept config` and open the "mcp servers" section to edit entries; save writes atomically via the same path.
3. **Menubar app** — open settings, go to the "tools" tab, and flip the toggle on a row.

Toggling via the config editor or menubar writes the change atomically to `~/.meept/mcp_servers.json5` (via `SaveMCPConfig`'s temp-file + rename) and triggers `Manager.Reload`, which starts newly-enabled servers and stops newly-disabled ones without restarting the platform.

### Env Var Placeholders (`${VAR}`)

Env values in the catalog use `${VAR}` placeholders. Meept does not expand these itself; they are passed through to the subprocess environment at transport-creation time inside `Manager.StartServer`. Export the env vars in your shell before starting the platform:

```bash
export GITHUB_TOKEN="ghp_xxx"
./bin/meept-daemon -f
```

The `${VAR:-default}` shell-default syntax is also supported. Unknown env vars expand to the empty string.

### Runtime States

Each configured server has a runtime state tracked in memory (resets on platform restart):

| state | meaning |
|-------|---------|
| `active` | connected and ready to serve tool calls |
| `inactive` | enabled but not yet started |
| `error` | enabled, but failed to start or not connected |
| `disabled` | `enabled: false`; skipped at startup and on reload |

`CallTool` invocations increment the per-server `requests` counter (success + failure). Failed invocations increment `errors` and populate `last_error` / `last_error_at`. The platform's health monitor flips enabled-but-disconnected servers to `error` every 60 seconds.

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
- Config editor: `meept config` → "mcp servers" section.
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