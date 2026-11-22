# Agent Reference: Configuration

Moved from the root `AGENTS.md` to keep the root under the size cap. Update the
root pointer and this file in the same commit.

## Configuration

All config uses **JSON5** format. Templates in `config/`, copied on
`make install`.

- **Main**: `~/.meept/meept.json5`
- **Kanban**: the `[kanban]` block in `config/meept.json5` ships uncommented with
  `"enabled": true` and `"sidecar_enabled": true` (board + loopback sidecar
  default-on; set either to `false` to opt out). Installed homes older than this
  block lack it and fall back to the same package defaults — see the
  `// drift:` annotation on the template block and `config-drift-check.py`.
- **Models**: `config/models.json5` (capability-based resolution)
- **MCP servers**: `~/.meept/mcp_servers.json5` (22 preconfigured, 8 enabled
  by default — incl. `obscura` browser MCP, enabled; `excel` xlsx fallback,
  disabled). Every stdio entry carries `install_hint`; `meept doctor`
  surfaces it and `meept doctor --fix --install-missing` runs hints
  opt-in (consent per command). Daemon/launchd/menubar launches augment
  PATH via `internal/daemon/daemonpath.go` `DaemonPath()` (menubar keeps
  a synced Swift copy) — keep new guaranteed dirs in both.
- **Skills**: `config/skills/` ships via `make install` / `make sync-config`
  into `~/.meept/skills/` (merge, no-clobber: user-modified files are kept,
  new defaults land as `<file>.new` — see `scripts/install-sync.py`).
  Frontmatter `requires-tools:` gates
  execution on tool availability (see docs/workflows/skills.md).
- **Home dir**: everything meept reads/writes lives under one home —
  `$MEEPT_HOME` when the env var is set, else `~/.meept`
  (`config.MeeptHome()` / `MeeptPath()` in internal/config/home.go are THE
  resolution points; config paths carrying the `~/.meept` prefix are
  redirected through it too). Set `MEEPT_HOME` consistently for daemon,
  CLI, and `make sync-config` — they all honor it.
- **ACP agents**: `~/.meept/acp_agents.json5` (catalog of external ACP
  agents; `[acp] enabled` defaults false — no subprocesses until opted in)
- **UI theme** (TUI + GUI): shared tokens in `theme/tokens.json5`; select per
  client via `rendering.ui_theme` — see `docs/configuration/theming.md`.
- **Client**: `~/.meept/client.json5` (TUI keybindings)
- **Log level**: `log_level` field in `~/.meept/meept.json5` (NOT env vars)
- **Log size cap**: `daemon.log_max_bytes` bounds `~/.meept/meept.log` (default
  256 MiB, 3 retained generations, worst case 3x). Enforced in-process by
  `internal/logrotate` — a spawner holds the log fd open for the daemon's whole
  life, so a parent-side rotate leaves the daemon appending to an unlinked file
  and the cap stops binding.

See `docs/configuration/` for full reference.

