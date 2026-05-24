# meept config — Interactive Configuration CLI

**Date:** 2026-05-23
**Status:** Approved

## Overview

A new `meept config` command providing an interactive TUI for editing all meept configuration files. Uses bubbletea v2 with flat section menus and drill-down sub-screens for nested structures. The existing `meept models` command is removed and replaced by `meept config models`.

## Entry Point

`meept config` opens the interactive TUI at the main menu. Subcommands jump directly to a section or perform non-interactive operations.

```
meept config                    # interactive TUI — main menu
meept config <section>          # interactive TUI — jump to section
meept config list               # non-interactive: print config file paths and status
meept config get <keypath>      # non-interactive: get a single value
meept config set <keypath> <v>  # non-interactive: set a single value
```

`meept models` is removed entirely.

## Architecture

### Approach: Flat Menus + Drill-Down Hybrid

- Main menu: bubbletea list picking a section
- Flat sections: scrollable field list for scalar fields
- Drill-down: sub-screens for list-of-struct values (providers, agents, mcp servers)

### Package Structure

```
cmd/meept/config.go              # cobra command wiring
internal/configui/
  app.go                         # root bubbletea model, main menu
  section.go                     # SectionModel — shared field-list model
  drilldown.go                   # DrilldownModel — sub-screen for nested structs
  fields.go                      # field types (Toggle, Select, MultiSelect, Text, Masked, Number)
  editor.go                      # inline field editors
  writers.go                     # config file writing (atomic, JSON5)
  keypath.go                     # dot-notation keypath resolver for get/set
  sections/
    daemon.go                    # each file defines one section's fields
    transport.go
    llm.go
    models.go
    presets.go
    agents.go
    memory.go
    security.go
    mcp.go
    client.go
    scheduler.go
    queue.go
    workers.go
    isolation.go
    workspace.go
    skills.go
    orchestrator.go
    compaction.go
    session.go
    code_intel.go
    telegram.go
    web.go
    mcp_toggle.go
    plugins.go
    selfimprove.go
    shadow.go
    distributed_memory.go
    q_agent.go
    tooling.go
    calendar.go
    memvid.go
    multiagent.go
    agent_loop.go
```

### UI Components

All built on bubbletea v2 + lipgloss v2 (already in go.mod).

**Main Menu (App):** bubbletea list with 10 primary sections + "show advanced" toggle. Arrow keys navigate, enter selects, `q` quits.

**Section Screen (SectionModel):** scrollable field list. Each row: label (left), current value (right, styled by type), `*` marker if dirty. Keybindings:
- `↑/↓` navigate
- `enter` edit field (opens inline editor)
- `d` reset to default
- `?` field help
- `esc`/`q` back to main menu (triggers save if dirty)

**Inline Editor (EditorModel):** activated on `enter`, varies by field type:
- **ToggleField:** `[*] enabled` / `[ ] disabled`, space to flip
- **SelectField:** vertical option list, arrow to pick, enter to confirm
- **MultiSelectField:** vertical list with `[*]`/`[ ]` checkboxes, space to toggle, enter to confirm
- **TextField:** standard text input with cursor
- **MaskedField:** text input showing `••••••` for typed characters (used for API keys)
- **NumberField:** text input rejecting non-numeric characters

All editors show the field label and a hint line at the bottom.

**Drilldown Screen (DrilldownModel):** list-of-struct sub-screen. Shows item list, enter opens a SectionModel for that item. Supports add (`a`) and delete (`x`) from list view.

Example — models > providers:
```
┌─ models > providers ──────────────────────┐
│  > zai                   [2 models]      │
│    openai                [3 models]      │
│    anthropic             [1 model]       │
│                                           │
│  [a] add provider  [x] remove selected   │
└───────────────────────────────────────────┘
```

### Save Flow

When leaving a section (esc/q) with dirty fields:
1. Prompt: `save changes? [y/n]`
2. `y` — write config file atomically (temp file + rename), show confirmation toast
3. `n` — discard, return to main menu

Comments in JSON5 are not preserved on save (same behavior as current `models` command). The writer uses `json.MarshalIndent` with 2-space indent, matching the existing pattern.

### Config File Mapping

A single in-memory `config.Config` struct is loaded at startup for all `meept.json5` sections. Separate files load their own structs:

| Section | Config File | Go Struct |
|---------|-------------|-----------|
| daemon | `~/.meept/meept.json5` | `config.Config` (daemon subtree) |
| transport | `~/.meept/meept.json5` | `config.Config` (transport subtree) |
| llm | `~/.meept/meept.json5` | `config.Config` (llm subtree) |
| models | `~/.meept/models.json5` | `llm.ProvidersConfig` |
| presets | `~/.meept/presets.json5` | `config.PresetConfig` |
| agents | `~/.meept/agents.json5` | `config.AgentsFileJSON5` |
| memory | `~/.meept/meept.json5` | `config.Config` (memory subtree) |
| security | `~/.meept/meept.json5` | `config.Config` (security subtree) |
| mcp servers | `~/.meept/mcp_servers.json5` | `config.MCPServersConfig` |
| client/tui | `~/.meept/client.json5` | `tui.ClientConfig` |
| all other meept.json5 sections | `~/.meept/meept.json5` | `config.Config` (respective subtree) |

Config priority for loading (existing behavior preserved):
- Main config: `~/.meept/meept.json5` > `~/.meept/meept.toml`
- Models: `~/.meept/models.json5` > `config/models.json5`
- Client: `.meept/client.json5` > `~/.meept/client.json5`

## Section Menu Layout

### Primary Menu (shown by default)

| Menu Item | Config File | Description |
|-----------|-------------|-------------|
| daemon | `meept.json5` | Socket path, PID file, log level, data dir |
| transport | `meept.json5` | RPC/HTTP toggles, addresses, endpoint flags |
| llm | `meept.json5` | Budget, broker, adaptive timeout, context firewall, cache |
| models | `models.json5` | Default model, providers, models, credentials, runtime lifecycle |
| agents | `agents.json5` | Agent definitions, tools, prompts |
| memory | `meept.json5` | Backend, episodic/task/personality, embeddings, limits, expiration |
| security | `meept.json5` | Sanitization, path restrictions, tirith, audit |
| mcp servers | `mcp_servers.json5` | MCP server definitions (stdio/http) |
| client/tui | `client.json5` | Connection, keybindings, vim, rendering, chat |
| scheduler | `meept.json5` | Timezone |

### Advanced Menu (toggled via "show advanced")

| Menu Item | Config File | Description |
|-----------|-------------|-------------|
| multiagent | `meept.json5` | Dispatcher/classifier models, memory refs |
| agent loop | `meept.json5` | Progress, cache, errors, review, validation, watchdog, queues |
| queue | `meept.json5` | DB path, max retries |
| workers | `meept.json5` | Pool size, idle timeout, capabilities |
| isolation | `meept.json5` | Sandbox dir, auto git init, auto test |
| workspace | `meept.json5` | Base dir, auto commit settings |
| skills | `meept.json5` | Search paths, auto reload, cache |
| orchestrator | `meept.json5` | Max plan steps, timeouts |
| compaction | `meept.json5` | Context compaction model, tokens, ratios |
| session | `meept.json5` | Persistence, branching, compaction, auto fork |
| code intel | `meept.json5` | AST cache, LSP servers |
| telegram | `meept.json5` | Bot token, allowed users, poll timeout |
| web | `meept.json5` | Host, port, secret key |
| mcp toggle | `meept.json5` | MCP enabled, config file path |
| plugins | `meept.json5` | Enabled, directory |
| self-improve | `meept.json5` | AI infra, sandbox, safety, detection |
| shadow | `meept.json5` | Shadowing, teacher, quality, adapters |
| distributed memory | `meept.json5` | Mode, sync, distillation |
| q agent | `meept.json5` | Thresholds, notifications, analysis |
| tooling | `meept.json5` | Sidecar agent config |
| calendar | `meept.json5` | Google OAuth, reminders |
| memvid | `meept.json5` | Endpoint, data dir, timeout |
| presets | `presets.json5` | Temperature/preset profiles |

## Section Field Maps

### daemon

| Field | Type | Key Path |
|-------|------|----------|
| log level | Select (debug/info/warn/error) | `daemon.log_level` |
| data dir | Text | `daemon.data_dir` |
| socket path | Text | `daemon.socket_path` |
| pid file | Text | `daemon.pid_file` |

### transport

| Field | Type | Key Path |
|-------|------|----------|
| rpc enabled | Toggle | `transport.rpc.enabled` |
| rpc socket path | Text | `transport.rpc.socket_path` |
| http enabled | Toggle | `transport.http.enabled` |
| http addr | Text | `transport.http.addr` |
| require auth | Toggle | `transport.http.require_auth` |
| api keys | Drilldown (multi-text) | `transport.http.api_keys` |
| rest | Toggle | `transport.http.rest` |
| websocket | Toggle | `transport.http.websocket` |
| ws path | Text | `transport.http.ws_path` |
| mcp | Toggle | `transport.http.mcp` |
| mcp path | Text | `transport.http.mcp_path` |

### llm

| Field | Type | Key Path |
|-------|------|----------|
| hourly token limit | Number | `llm.budget.hourly_token_limit` |
| daily token limit | Number | `llm.budget.daily_token_limit` |
| rate limit rpm | Number | `llm.budget.rate_limit_rpm` |
| budget aggressiveness | Number (0.0-1.0) | `llm.budget.aggressiveness` |
| broker max error rate | Number (0.0-1.0) | `llm.broker.max_error_rate` |
| broker max p95 latency | Number | `llm.broker.max_p95_latency_ms` |
| broker fallback | Toggle | `llm.broker.fallback_enabled` |
| adaptive timeout | Toggle | `llm.adaptive_timeout.enabled` |
| adaptive min timeout | Number | `llm.adaptive_timeout.min_timeout_seconds` |
| adaptive max timeout | Number | `llm.adaptive_timeout.max_timeout_seconds` |
| context firewall | Toggle | `llm.context_firewall.enabled` |
| summarize history | Toggle | `llm.context_firewall.summarize_history` |
| wrap-up threshold | Number (0.0-1.0) | `llm.context_firewall.wrap_up_threshold` |
| hard limit | Number (0.0-1.0) | `llm.context_firewall.hard_limit` |
| drop on hard limit | Toggle | `llm.context_firewall.drop_context_on_hard_limit` |
| proactive compression | Toggle | `llm.context_firewall.proactive_compression` |
| metrics | Toggle | `llm.metrics.enabled` |
| metrics db path | Text | `llm.metrics.db_path` |
| metrics retention days | Number | `llm.metrics.retention_days` |
| cache | Toggle | `llm.cache.enabled` |
| cache l1 max entries | Number | `llm.cache.l1_max_entries` |
| cache l2 enabled | Toggle | `llm.cache.l2_enabled` |

### models

| Field | Type | Key Path |
|-------|------|----------|
| default model | Text | `model` |
| small model | Text | `small_model` |
| disabled providers | Multi-select | `disabled_providers` |
| providers | Drilldown | `providers` |
| credentials | Drilldown (OS keychain) | stored via OS keychain, not in config file |

Provider drilldown: api type (select: openai), base_url (text), api_key (masked), timeout (number). Models sub-drilldown: capabilities (multi-select from [code, reasoning, tool_use, vision]), context_length (number).

### presets

| Field | Type | Key Path |
|-------|------|----------|
| default preset | Select (from existing) | `default` |
| presets | Drilldown | `presets` |

Preset drilldown: label (text), description (text), temperature (number), top_p (number), max_tokens (number).

### agents

| Field | Type | Key Path |
|-------|------|----------|
| agents | Drilldown | agent list |

Agent drilldown: role (text), description (text), tools (multi-select from registered tools), system_prompt (text), constraints (multi-text).

### memory

| Field | Type | Key Path |
|-------|------|----------|
| backend | Select (memvid/sqlite) | `memory.backend` |
| data dir | Text | `memory.data_dir` |
| consolidation interval | Number | `memory.consolidation_interval_hours` |
| episodic enabled | Toggle | `memory.episodic.enabled` |
| episodic max context items | Number | `memory.episodic.max_context_items` |
| task enabled | Toggle | `memory.task.enabled` |
| task domains | Multi-select | `memory.task.domains` |
| personality enabled | Toggle | `memory.personality.enabled` |
| personality update interval | Number | `memory.personality.update_interval_conversations` |
| embeddings enabled | Toggle | `memory.embeddings.enabled` |
| embeddings provider | Select (openai/ollama) | `memory.embeddings.provider` |
| embeddings api key | Masked | `memory.embeddings.api_key` |
| embeddings base url | Text | `memory.embeddings.base_url` |
| embeddings model | Text | `memory.embeddings.model` |
| security enabled | Toggle | `memory.security.enabled` |
| security fail closed | Toggle | `memory.security.fail_closed` |
| caching enabled | Toggle | `memory.caching.enabled` |
| expiration enabled | Toggle | `memory.expiration.enabled` |
| expiration days | Number | `memory.expiration.access_expiration_days` |
| versioning enabled | Toggle | `memory.versioning.enabled` |
| versioning max versions | Number | `memory.versioning.max_versions` |

### security

| Field | Type | Key Path |
|-------|------|----------|
| sanitize inputs | Toggle | `security.sanitize_inputs` |
| sanitize strictness | Select (permissive/standard/strict) | `security.sanitize_strictness` |
| monitor output | Toggle | `security.monitor_output` |
| redact output | Toggle | `security.redact_output` |
| scan shell commands | Toggle | `security.scan_shell_commands` |
| tirith binary | Text | `security.tirith_binary` |
| require confirmation high | Toggle | `security.require_confirmation_high` |
| require confirmation critical | Toggle | `security.require_confirmation_critical` |
| block financial | Toggle | `security.block_financial` |
| allowed paths | Drilldown (multi-text) | `security.allowed_paths` |
| blocked paths | Drilldown (multi-text) | `security.blocked_paths` |
| enable audit log | Toggle | `security.enable_audit_log` |
| audit db path | Text | `security.audit_db_path` |

### mcp servers

| Field | Type | Key Path |
|-------|------|----------|
| servers | Drilldown | server list |

Server drilldown: name (text), type (select: stdio/http), command (text), args (multi-text), env (key-value), headers (key-value).

### client/tui

| Field | Type | Key Path |
|-------|------|----------|
| transport | Select (rpc/http/auto) | `connection.transport` |
| address | Text | `connection.address` |
| connection timeout | Text | `connection.timeout` |
| retry attempts | Number | `connection.retry.attempts` |
| retry delay | Text | `connection.retry.delay` |
| keybinding command mode | Text | `keybindings.command_mode` |
| keybinding quit | Text | `keybindings.quit` |
| escape behavior | Select (once/twice/off) | `keybindings.escape_behavior` |
| session auto resume | Toggle | `session.auto_resume` |
| session default name | Text | `session.default_name` |
| vim enabled | Toggle | `vim.enabled` |
| vim escape insert | Text | `vim.escape_insert` |
| vim leader | Text | `vim.leader` |
| markdown rendering | Toggle | `rendering.markdown` |
| syntax highlighting | Toggle | `rendering.syntax_highlighting` |
| theme | Text | `rendering.theme` |
| word wrap | Toggle | `rendering.word_wrap` |
| show header | Toggle | `rendering.show_header` |
| sidebar animation | Toggle | `rendering.sidebar_animation` |
| sidebar show metrics | Toggle | `rendering.sidebar.show_metrics` |
| sidebar show activity | Toggle | `rendering.sidebar.show_activity_feed` |
| sidebar default panel | Number | `rendering.sidebar.default_panel` |
| sidebar metrics history | Number | `rendering.sidebar.metrics_history` |
| sidebar activity feed size | Number | `rendering.sidebar.activity_feed_size` |
| auto copy on release | Toggle | `chat.auto_copy_on_release` |
| scroll speed | Number | `chat.scroll_speed` |
| verbosity | Select (quiet/normal/verbose) | `chat.verbosity` |

### scheduler

| Field | Type | Key Path |
|-------|------|----------|
| enabled | Toggle | `scheduler.enabled` |
| timezone | Text | `scheduler.timezone` |

### Advanced Sections

All advanced sections follow the same pattern — flat field lists using the standard field types (Toggle, Select, MultiSelect, Text, Masked, Number) with key paths into their respective `meept.json5` subtrees. Drilldowns used for map-valued and slice-of-struct fields.

## Testing

- **Unit tests per section:** table-driven tests covering field loading, display rendering, editing (each field type), and save round-trip
- **Writer tests:** atomic write, no-change skip, multi-section shared-file save
- **Keypath tests:** get/set with dot notation for all supported key paths
- **Integration test:** launch configui app, verify main menu renders, navigate to a section, toggle a field, save, verify config file changed
