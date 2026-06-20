# MCP Default Catalog, Toggle, and UI Design

**Date:** 2026-06-19
**Status:** Implemented (2026-06-20) — all 10 sections verified via oneshot-yeet
**Topic:** Ship 21 popular MCP servers as preconfigured defaults with per-server enable/disable, runtime status tracking, and TUI/GUI management surfaces.

## Goals

1. Ship a default catalog of 21 commonly-used MCP servers, fully and correctly configured, in `config/mcp_servers.json5` (template copied on `make install`).
2. Add a per-server `enabled` boolean to the MCP server config schema; gate startup on it.
3. Track per-server runtime state (`active`/`inactive`/`error`/`disabled`) and request counts in memory.
4. Expose management surfaces in both clients:
   - **TUI** (`cmd/meept-lite`): `ctl-x o` opens an MCP menu with columns: enabled, status, requests, errors, description. `e` toggles enabled.
   - **Menubar app** (`menubar/`): new "tools" tab in Settings with the same columns; toggle via SwiftUI `Toggle`.
5. Toggling enabled persists the change atomically to `mcp_servers.json5` and triggers the existing `Manager.Reload` path (start newly-enabled, stop newly-disabled).

## Non-Goals (Out of Scope)

- Per-MCP request rollups in SQLite metrics store.
- OAuth flow automation (env-var placeholders only; user exports vars and reloads).
- Auto-install of `npx`, `uvx`, `docker`, or other MCP runtimes.
- Bulk enable/disable operations.
- Filtering/search in the menus (21 rows fits one screen).
- Hot-apply start/stop without reload (persist+reload only for this iteration).

## Design Decisions (from brainstorm)

| Decision | Choice | Rationale |
|----------|--------|-----------|
| TUI keybinding | `ctl-x o` | `ctl-x m` is already memory menu; avoid conflicts |
| Python MCPs without `npx` | Use `uvx <pkg>` | Modern Python tool runner; matches MCP community convention |
| Default `enabled` state | Per-server: only zero-config MCPs ship `enabled: true` (git, fetch, searxng-local); rest ship `enabled: false` with full config | Quiet first-run; no false errors for servers needing API keys |
| Toggle application | Persist to JSON5 + `Manager.Reload` | Reuses well-tested reload path; no new start/stop failure surface |
| Request counting | Per `CallTool` invocation (success + failure), in-memory only | Matches user intuition; resets on daemon restart |
| Error state semantics | `error` = server failed to start OR enabled-but-not-connected | Answers the user's question: "I enabled it, why isn't it working?" |

## Section 1: Configuration Schema

Extend `internal/tools/mcp/manager.go` `ServerConfig`:

```go
type ServerConfig struct {
    Name        string            `json:"name"`
    Enabled     *bool             `json:"enabled,omitempty"`     // nil/absent = true (backward compat)
    Command     []string          `json:"command,omitempty"`     // stdio
    URL         string            `json:"url,omitempty"`         // http
    Type        string            `json:"type,omitempty"`        // "stdio" | "http"
    Env         map[string]string `json:"env,omitempty"`
    Headers     map[string]string `json:"headers,omitempty"`
    Description string            `json:"description,omitempty"` // optional, for UI display
    Category    string            `json:"category,omitempty"`    // optional, for UI grouping
}
```

**Semantics:**

- `Enabled == nil` → treated as `true` (preserves existing behavior: configs without the field keep working).
- `Enabled == &false` → skipped at startup and on reload; shown as `disabled` in UI.
- Global `cfg.MCP.Enabled` still gates whether the Manager is constructed at all.

**Helper method:**

```go
func (sc ServerConfig) IsEnabled() bool {
    return sc.Enabled == nil || *sc.Enabled
}
```

**Default catalog** lives in `config/mcp_servers.json5` (template, copied on `make install` if absent). Each entry is fully configured with the correct `Command` (npx or uvx as appropriate), `Env` (with `${VAR}` placeholders), `Category`, and `Description`. Enabled defaults per the (b) policy: only zero-config ones (`git`, `fetch`, `searxng` if local URL) get `true`; everything else `false`.

**Example entry:**

```json5
{
  name: "github",
  enabled: false,
  category: "vcs",
  description: "github repos, issues, prs",
  type: "stdio",
  command: ["npx", "-y", "@modelcontextprotocol/server-github"],
  env: {
    GITHUB_PERSONAL_ACCESS_TOKEN: "${GITHUB_TOKEN}",
  },
},
```

## Section 2: Per-Server Stats and Runtime State

Add to `internal/tools/mcp/manager.go`:

```go
// ServerState represents the runtime status of an MCP server.
type ServerState string

const (
    StateDisabled ServerState = "disabled" // enabled: false
    StateInactive ServerState = "inactive" // enabled, not yet started
    StateActive   ServerState = "active"   // connected and ready
    StateError    ServerState = "error"    // failed to start, or enabled but not connected
)

// ServerStats holds per-server runtime stats. In-memory only.
type ServerStats struct {
    State         ServerState `json:"state"`
    Requests      int64       `json:"requests"`       // CallTool invocations (success + failure)
    Errors        int64       `json:"errors"`         // failed CallTool invocations
    LastError     string      `json:"last_error"`     // trimmed; "" if none
    LastErrorAt   *time.Time  `json:"last_error_at"`
    LastRequestAt *time.Time  `json:"last_request_at"`
}

// ServerStatusEntry pairs a config with its runtime stats for UI consumption.
type ServerStatusEntry struct {
    Config ServerConfig `json:"config"`
    Stats  ServerStats  `json:"stats"`
}
```

**Manager additions** (in `internal/tools/mcp/manager.go`):

- `stats map[string]*ServerStats` under the existing `mu` (in-memory only).
- `configs map[string]ServerConfig` — snapshot of all configured servers (including disabled), updated by `SetConfigs`. Enables `AllServerStatuses` to list disabled servers that never entered `clients`.
- `stats` entries: created on `StartServer` (state=`active` or `error`); on `StopServer`, state is set to `inactive` (entry retained so UI continues to show counts); on `Reload`, disabled configs keep their entry but state flips to `disabled` (counts preserved), removed configs have their entry deleted.
- `StartServer` failure → set state=`error`, `LastError=err.Error()`.
- `CallTool` increments `Requests` under lock before dispatching; on return, increments `Errors` + sets `LastError`/`LastErrorAt` if non-nil.
- `StartHealthMonitor(ctx)` — spawns a goroutine that every 60s walks `clients` and flips any non-connected enabled server to `error`. Cancelled via `ctx` when daemon shuts down.

**New public methods:**

```go
// SetConfigs records the full set of configured servers (including disabled)
// so AllServerStatuses can report on them.
func (m *Manager) SetConfigs(configs []ServerConfig)

// ServerStatus returns the full status of a single server.
func (m *Manager) ServerStatus(name string) (ServerConfig, ServerStats, bool)

// AllServerStatuses returns configs + stats for every configured server,
// including disabled ones. Used by UIs.
func (m *Manager) AllServerStatuses() []ServerStatusEntry

// StartHealthMonitor launches a background goroutine that periodically
// updates state for enabled-but-disconnected servers to "error".
// Cancels when ctx is done.
func (m *Manager) StartHealthMonitor(ctx context.Context)
```

**AllServerStatuses merge logic:**

1. Start with all entries from `configs` map.
2. For each, look up `stats[name]` (may be nil for disabled servers).
3. If config disabled (`!IsEnabled()`), force state=`disabled`.
4. If config enabled and stats entry exists, use its state.
5. If config enabled and stats entry missing, state=`inactive`.
6. Sort by `Category` then `Name` for stable UI display.

**Backward compatibility:** If `Manager` is constructed without `SetConfigs` having been called (old code path), `configs` is empty and `AllServerStatuses` falls back to listing just the started clients. No regression.

## Section 3: Daemon Wiring

**Startup path** (`internal/daemon/components.go:1148`):

1. Load `mcpCfg` as today.
2. Call `Manager.SetConfigs(mcpCfg.Servers)` — populates `configs` map for `AllServerStatuses`.
3. Loop over `mcpCfg.Servers`:
   - If `!serverCfg.IsEnabled()`: log at debug `"skipping disabled MCP server"` with name, continue.
   - Else: call `StartServer`. On failure, the new `stats` entry will hold state=`error` with `LastError` set (handled inside `StartServer`).
4. After loop: `Manager.StartHealthMonitor(ctx)` — uses a daemon-lifetime context (cancelled on shutdown).
5. Startup summary log: `"MCP servers: X enabled, Y active, Z errors, W disabled (configure at ~/.meept/mcp_servers.json5)"`.

**Reload path** (`internal/daemon/daemon.go:1213`):

- Unchanged except: `Manager.Reload` now calls `SetConfigs(newConfigs)` before phase-1 stop.
- Disabled entries are filtered out of "configs to start" in phase 2 (skip with debug log, do not treat as error).
- Stats entries for disabled servers are preserved (state updated to `disabled`) so UI continues to show them.

**Reload signature change:** `Reload(ctx, configs)` already takes `[]ServerConfig`; no signature change needed. The `SetConfigs` call happens inside `Reload` before phase 1.

## Section 4: Atomic Config Persistence

New helper in `internal/config/mcp.go`:

```go
// SaveMCPConfig writes the MCP server configuration atomically.
// Writes to path+".tmp" then renames into place (POSIX atomic).
// ${VAR} placeholders in env values are preserved as-is.
func SaveMCPConfig(path string, cfg *MCPServersConfig) error
```

**Toggle flow safety:** The toggle handler (RPC/HTTP) reads the on-disk config fresh (not cached), finds the entry by name, mutates only its `Enabled` field, writes atomically, then triggers `Manager.Reload`. This avoids lost-update if the user edited the file by hand between reads.

**`${VAR}` preservation:** The env map contains literal `"${VAR}"` strings. No `SaveJSON5` helper exists in `internal/config/json5_loader.go` — `SaveMCPConfig` uses `encoding/json` (standard JSON, valid JSON5) with indent, preserving the struct tags already on `ServerConfig`. The `"${VAR}"` strings round-trip cleanly through standard JSON serialization. Expansion happens at transport-creation time in `Manager.StartServer` (existing behavior). Comments from the original template are lost on rewrite — acceptable trade-off; the template is the source of truth for human-readable comments.

## Section 5: RPC + HTTP Endpoints (persist + reload)

**RPC** (extend `internal/rpc/`):

| Method | Params | Result | Notes |
|--------|--------|--------|-------|
| `mcp.list` | `{}` | `[]ServerStatusEntry` | Calls `Manager.AllServerStatuses`. Each entry has config + stats. |
| `mcp.set_enabled` | `{name: string, enabled: bool}` | `ServerStatusEntry` | Updates `mcp_servers.json5` via `SaveMCPConfig`, then triggers daemon reload. Returns the updated entry. |

**HTTP** (extend `internal/comm/http/api_handlers.go`):

| Endpoint | Method | Body | Result |
|----------|--------|------|--------|
| `/api/v1/mcp/servers` | `GET` | — | `{"servers": [ServerStatusEntry, ...]}` |
| `/api/v1/mcp/servers/{name}/enabled` | `PUT` | `{"enabled": true}` | `ServerStatusEntry` |

**Toggle flow:**

```
TUI/GUI ──RPC/HTTP──> daemon
  ├─ Read mcp_servers.json5 fresh
  ├─ Find entry by name
  ├─ Set entry.Enabled = &enabled
  ├─ SaveMCPConfig (atomic write)
  ├─ Manager.Reload(fullCfg)
  │    ├─ SetConfigs (refresh configs map)
  │    ├─ Phase 1: stop servers no longer enabled
  │    └─ Phase 2: start newly-enabled servers
  └─ return updated ServerStatusEntry
TUI/GUI refreshes from returned entry
```

**OpenAPI spec** (`docs/reference/http-api/openapi.yaml`): add the two new endpoints.

## Section 6: TUI Menu (`cmd/meept-lite`)

**New menu file:** `internal/sharedclient/menus/mcp.go`, mirroring `memory.go` structure.

**Binding:** `cmd/meept-lite/tui.go:515` — add `case "o":` that opens `t.mcpMenu.Show()`. Add `mcpMenu *menus.MCPMenu` field to the `TUI` struct alongside existing menu fields (line 30 area). Wire callbacks the same way as `memoryMenu` (line 149 area).

**Display layout** (termbox, all lowercase per CLAUDE.md UI convention):

```
┌─ mcp servers ─────────────────────────────────────────────┐
│  e  toggle enabled on selected                            │
│  ↑↓ move   r refresh   esc close                          │
├──┬──────────┬─────────┬──────────┬────────┬──────────────┤
│en│ server   │ status  │ reqs     │ errors │ description  │
├──┼──────────┼─────────┼──────────┼────────┼──────────────┤
│■ │ github   │ error   │        0 │      0 │ github repos │
│□ │ slack    │ disabled│        0 │      0 │ slack mes... │
│■ │ git      │ active  │       23 │      0 │ local git    │
│■ │ postgres │ error   │        0 │      1 │ db access    │
└──┴──────────┴─────────┴──────────┴────────┴──────────────┘
```

**Column semantics:**

- `en`: `■` enabled / `□` disabled (single glyph, left-padded).
- `server`: the config `Name`, left-aligned.
- `status`: one of `active` / `inactive` / `error` / `disabled`, right-padded.
- `reqs`: `ServerStats.Requests`, right-aligned numeric.
- `errors`: `ServerStats.Errors`, right-aligned numeric.
- `description`: `Config.Description`, truncated with `...` to fit column.

**Interactions:**

- Arrow keys (`↑`/`↓`) move selection; selected row inverted.
- `e` toggles `Enabled` on selected server. Shows inline `[toggling...]` while RPC in flight, then refreshes with returned entry. On error: `[toggle failed: <msg>]`.
- `r` forces a refresh from `mcp.list`.
- `esc` closes the menu.
- Category color stripe on the left edge (optional, simple to add later; not required for v1).

**Slash command alias:** Add `/mcp` to open the same menu, consistent with existing slash commands.

## Section 7: Menubar App (`menubar/`)

**New SwiftUI view:** `menubar/MeeptMenuBar/Views/Tools/MCPServersView.swift` — uses `Table` with columns matching the TUI.

**Integration:** Add a "tools" tab to `SettingsWindow.swift`'s `TabView`:

```swift
MCPServersView(viewModel: mcpViewModel)
    .tabItem { Label("tools", systemImage: "wrench.and.screwdriver") }
    .tag(3)
```

**New view model:** `menubar/MeeptMenuBar/ViewModels/MCPViewModel.swift`:

- `@Published var servers: [MCPServer]`
- Polls `GET /api/v1/mcp/servers` every 5s while the view is visible (matches existing `MetricsViewModel` polling pattern).
- `toggleEnabled(name: String, enabled: Bool)` — calls `PUT /api/v1/mcp/servers/{name}/enabled`; on success, updates local state optimistically then re-fetches on next poll. On error, shows alert and reverts.

**New model:** `menubar/MeeptMenuBar/Models/MCPServer.swift`:

```swift
struct MCPServer: Codable, Identifiable {
    let id: String          // == config.name
    let name: String
    let enabled: Bool
    let category: String?
    let description: String?
    let state: String       // "active" | "inactive" | "error" | "disabled"
    let requests: Int
    let errors: Int
    let lastError: String?
}
```

**Column layout** (SwiftUI `Table`):

| Column | Content | Notes |
|--------|---------|-------|
| enabled | `Toggle("", isOn: ...)` | Disabled interaction while PUT in flight |
| server | `Text(server.name)` | |
| status | `Text(server.state)` | Colored: green=active, gray=disabled, yellow=inactive, red=error |
| requests | `Text("\(server.requests)")` | |
| errors | `Text("\(server.errors)")` | |
| description | `Text(server.description ?? "")` | `.lineLimit(1)` with truncation |

**UX details:**

- Toggle disabled briefly while PUT in flight to prevent double-clicks.
- On PUT error: alert dialog with the message; local state reverts on next poll.
- Lowercase labels per CLAUDE.md UI convention.

## Section 8: Default Catalog Research

The 21 requested MCPs, grouped by runtime:

**npx-based (Node):**

- Playwright (`@executeautomation/playwright-mcp-server` or `@playwright/mcp`)
- Puppeteer (`@modelcontextprotocol/server-puppeteer`)
- GitHub (`@modelcontextprotocol/server-github`)
- Slack (`@modelcontextprotocol/server-slack`)
- Docker (`@modelcontextprotocol/server-docker` or `ckreiling/mcp-server-docker`)
- Google Drive (`@modelcontextprotocol/server-google-drive`)
- Terraform (`@modelcontextprotocol/server-terraform` or community equivalent)
- Fetch (`@modelcontextprotocol/server-fetch`)
- PostgreSQL (`@modelcontextprotocol/server-postgres`)
- Aider (`@modelcontextprotocol/server-aider` or community equivalent — confirm during planning)
- OpenAdapt (confirm package name during planning)
- Firecrawl (has both npx `firecrawl-mcp` and uvx forms — use npx here)

**uvx-based (Python):**

- crawl4ai
- Crawlee (`crawlee-python` — confirm during planning)
- scrapegraphAI
- Open Interpreter
- OpenHands
- Exa-map
- Rivalsearch-mcp
- searxng (`searxng-mcp` or equivalent — confirm package name)

**HTTP/SSE endpoints (if any):** Some MCPs publish as hosted HTTP endpoints rather than spawnable subprocesses (Firecrawl hosted, Exa hosted). Research during planning phase will determine per-server whether `type: "http"` with a URL is more appropriate than `type: "stdio"` with a command.

**For each entry, the implementation plan will specify:**

- Exact `command` array (npx/uvx with correct package name and flags)
- Required `env` vars (with `${VAR}` placeholders)
- `enabled` default per the policy in Section 1 (zero-config = `true`, else `false`)
- `category` (e.g. `vcs`, `search`, `data`, `automation`, `browser`, `infra`)
- `description` (short, lowercase per CLAUDE.md)
- One-line install prerequisite in the description or a comment (e.g. `"requires docker daemon"`, `"requires uv installed"`)

Non-installable-on-this-machine entries are included but `enabled: false` with description noting the prereq.

## Section 9: Testing Strategy

**Unit tests** (`internal/tools/mcp/manager_stats_test.go` — new file):

- `TestSetConfigs_PopulatesConfigsMap` — disabled + enabled mix
- `TestAllServerStatuses_IncludesDisabledServers` — disabled servers appear with state=`disabled`
- `TestAllServerStatuses_FallbackWithoutSetConfigs` — empty `configs` falls back to listing `clients`
- `TestCallTool_IncrementsRequestsAndErrors` — mock client, simulate success + failure
- `TestStartServer_FailureSetsErrorState` — bad command (`false`), verify state=`error` + `LastError` populated
- `TestReload_DisabledServersSkippedFromStart` — disabled entries not in phase-2 start list
- `TestReload_PreservesStatsForDisabledServers` — stats entry remains with state=`disabled`

**Integration test** (`tests/integration/mcp_toggle_test.go` — new file):

- Use stub server command (`sleep 3600`) for enabled/active.
- Use guaranteed-fail command (`false`) for error state.
- Toggle flow: `mcp.set_enabled` → verify `mcp_servers.json5` updated on disk → verify `Manager.Reload` ran → verify `mcp.list` returns updated state.

**Pre-commit hooks:** Existing chain applies. New files in `internal/tools/mcp/`, `internal/sharedclient/menus/`, `menubar/` — no special additions beyond what's already enforced (mutexio, setters nil-guard, staticcheck, etc.).

## Section 10: Documentation Updates

Per CLAUDE.md documentation requirements:

| File | Update |
|------|--------|
| `docs/workflows/tool-routing.md` | Add MCP default catalog section; document per-server enable/disable |
| `docs/configuration/llm-lifecycle.md` (or sibling) | Cross-link MCP server config; document `${VAR}` env expansion |
| `docs/reference/http-api.md` | Add `GET /api/v1/mcp/servers` and `PUT /api/v1/mcp/servers/{name}/enabled` |
| `docs/reference/http-api/openapi.yaml` | Add the two new endpoints formally |
| `docs/reference/cli.md` | Document `/mcp` slash command in TUI |
| `CLAUDE.md` | Update "Configuration" section to mention default MCP catalog; note `enabled` field |
| `README.md` | If MCP section exists, mention default catalog briefly |

`make docs-generate` should be run if any `internal/config/schema.go` struct changes (none expected — `ServerConfig` lives in `internal/tools/mcp/manager.go`).

## Scope Summary

- ~6 Go files modified, 3 new Go files (menus/mcp.go, manager_stats_test.go, mcp_toggle_test.go)
- 1 config template (`config/mcp_servers.json5` — expanded from current)
- 3 new SwiftUI files (MCPServersView.swift, MCPViewModel.swift, MCPServer.swift)
- 1 SwiftUI file modified (SettingsWindow.swift — add tools tab)
- 2 new RPC methods
- 2 new HTTP endpoints

## Implementation Order (high-level; detailed in plan)

1. Schema: add `Enabled`/`Description`/`Category` to `ServerConfig`; add `IsEnabled()` helper.
2. Stats: add `ServerState`/`ServerStats`/`ServerStatusEntry` types; add `stats`/`configs` maps to Manager; add `SetConfigs`/`ServerStatus`/`AllServerStatuses`/`StartHealthMonitor`.
3. Wire stats into `StartServer`/`StopServer`/`CallTool`/`Reload`.
4. Daemon: call `SetConfigs` + `StartHealthMonitor` on startup; skip disabled in startup loop.
5. Persistence: `SaveMCPConfig` helper in `internal/config/mcp.go`.
6. RPC: add `mcp.list` and `mcp.set_enabled`.
7. HTTP: add `GET /api/v1/mcp/servers` and `PUT /api/v1/mcp/servers/{name}/enabled`.
8. Default catalog: research + populate `config/mcp_servers.json5`.
9. TUI: new `menus/mcp.go`; bind `ctl-x o`; add `/mcp` slash command.
10. Menubar: new SwiftUI view + view model + model; add "tools" tab to Settings.
11. Tests: unit + integration.
12. Docs: update all files listed in Section 10.
