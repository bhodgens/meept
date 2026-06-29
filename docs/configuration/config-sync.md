# Config Sync Configuration

Cluster-wide configuration distribution via git. Config sync periodically pulls a shared configuration repository, deep-merges per-node overrides on top of cluster-wide defaults, writes the result to `~/.meept/`, and triggers hot-reload hooks for supported components.

## Overview

Config sync solves the problem of keeping configuration consistent across multiple meept nodes. Instead of manually editing `meept.json5` on every machine, you maintain a single git repository with:

- **Shared configs** — cluster-wide defaults applied to every node
- **Per-node overrides** — node-specific settings deep-merged on top

The syncer runs on each node, pulls the repo on a schedule (default: every 5 minutes), and applies changes locally. For files where hot-reload is supported, changes take effect without a daemon restart.

### How it differs from backup and peer sync

| Feature | What it syncs | Source | Target |
|---------|--------------|--------|--------|
| **Backup** | SQLite databases | `local.db` | git repo |
| **Peer sync** | Sessions, turns, memories | Peer's backup in git | `sync-gossip.db` |
| **Config sync** | Configuration files | Dedicated config repo | `~/.meept/*.json5` |

Config sync uses its own repository, separate from the backup repository.

## Configuration Reference

Config sync is configured under the `config_sync` key in `~/.meept/meept.json5`:

```json5
// ~/.meept/meept.json5
{
  config_sync: {
    // Master switch. When false, no config sync runs.
    enabled: true,

    // Git repository URL containing shared and per-node configs.
    // Required when enabled.
    repo_url: "git@github.com:caimlas/meept-config.git",

    // Interval between automatic pulls. Go duration string.
    // Default: 5m (every 5 minutes).
    pull_schedule: "5m",

    // Conflict resolution mode. Determines how merge conflicts
    // in the git working tree are handled.
    // One of: "local-wins", "remote-wins", "manual".
    // Default: "local-wins".
    conflict_mode: "local-wins",
  },
}
```

### Field Reference

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `enabled` | bool | `false` | No | Enable config syncer |
| `repo_url` | string | — | Yes (if enabled) | Config repository URL |
| `pull_schedule` | duration | `5m` | No | Interval between config pulls |
| `conflict_mode` | string | `"local-wins"` | No | Conflict resolution strategy |

### Validation

Config validation fails if:
- `enabled: true` but `repo_url` is empty
- `pull_schedule` is zero or negative

### Node Identity

Config sync uses `backup.node_id` to determine which per-node override directory to apply. Ensure `backup.node_id` is set explicitly when using config sync:

```json5
{
  backup: {
    node_id: "node-a",  // Used by config sync for per-node overrides
  },
  config_sync: {
    enabled: true,
    repo_url: "git@github.com:caimlas/meept-config.git",
  },
}
```

## Config Repository Structure

```
config-sync-repo/
├── config/
│   ├── shared/                 # Cluster-wide configs (applied to all nodes)
│   │   ├── meept.json5         # Main daemon config
│   │   ├── models.json5        # LLM model definitions
│   │   └── mcp_servers.json5   # MCP server catalog
│   └── nodes/
│       ├── node-a/             # Per-node overrides for node-a
│       │   └── meept.json5
│       ├── node-b/             # Per-node overrides for node-b
│       │   └── meept.json5
│       └── node-c/
│           └── meept.json5
└── README.md
```

### Shared Configs (`config/shared/`)

Files in `config/shared/` are copied **wholesale** to `~/.meept/`. They represent the baseline configuration that every node in the cluster should have.

Supported file extensions: `.json5`, `.toml`.

### Per-Node Overrides (`config/nodes/<node_id>/`)

Files in `config/nodes/<node_id>/` are **deep-merged** on top of the corresponding file in `~/.meept/`. This lets you override individual nested fields without republishing the entire config.

## Deep-Merge Semantics

Deep-merge applies to `.json5` files only. TOML files use wholesale replacement.

### How it works

For each `.json5` file in `config/nodes/<node_id>/`:

1. Parse the existing file at `~/.meept/<filename>` (written by the shared pass earlier in the same cycle, or carried over from a previous cycle)
2. Parse the node override file
3. Recursively merge: for each key in the override, if both values are objects, merge recursively; otherwise, the override value wins
4. Write the merged result back to `~/.meept/<filename>`

### Example

**Shared** (`config/shared/meept.json5`):
```json5
{
  daemon: {
    data_dir: "~/.meept",
    log_level: "info",
  },
  llm: {
    default_model: "gpt-4o",
    timeout: "30s",
  },
  backup: {
    enabled: true,
    repo_url: "git@github.com:caimlas/meept-backups.git",
    schedule: "24h",
  },
}
```

**Node override** (`config/nodes/node-a/meept.json5`):
```json5
{
  daemon: {
    log_level: "debug",  // override just this field
  },
  llm: {
    timeout: "60s",  // override just this field
  },
}
```

**Result** (`~/.meept/meept.json5` after merge):
```json5
{
  daemon: {
    data_dir: "~/.meept",      // from shared
    log_level: "debug",         // from node override
  },
  llm: {
    default_model: "gpt-4o",   // from shared
    timeout: "60s",             // from node override
  },
  backup: {
    enabled: true,              // from shared
    repo_url: "...",            // from shared
    schedule: "24h",            // from shared
  },
}
```

### Fallback behavior

If the node override file is not a JSON object (e.g., a top-level array), or if the destination file does not exist or is unparseable, the override is applied wholesale (full file replacement) instead of deep-merged.

## Hot-Reload

After a successful merge, the config syncer triggers reload hooks for each applied file. Not all components support hot-reload:

| File | Hot-reload | Behavior on change |
|------|-----------|-------------------|
| `mcp_servers.json5` | Yes | MCP server catalog re-read from disk; `MCPManager.Reload` called. Running servers are stopped/restarted as needed. |
| `meept.json5` | No (restart required) | Warning logged. Daemon restart needed for changes to take effect. |
| `models.json5` | No (restart required) | Warning logged. LLM resolver has no reload method. |
| `backup.json5` | No (restart required) | Warning logged. Backup scheduler reads config at construction time. |

For files that require a restart, the daemon log will show:
```
config sync: meept.json5 changed on disk; daemon restart required for full effect
```

## CLI Commands

Config sync commands live under `meept config sync`:

### `meept config sync status`

Show current config sync state.

```bash
meept config sync status
```

Output:
```
Key                             Value
---                             ---
Enabled                         true
Repo                            git@github.com:caimlas/meept-config.git
Node                            node-a
Pull rate                       5m0s
Checkout                        /home/user/.meept/.config-sync/meept-config

Last commit                     abc1234 (2026-06-26T12:00:00Z)
```

The "Last commit" line appears only when the daemon is reachable via RPC and a pull has completed.

### `meept config sync pull`

Force an immediate config pull and merge, bypassing the schedule.

```bash
meept config sync pull
```

This dispatches via RPC (`config_sync.pull`) to the running daemon. The daemon performs a shallow git pull, runs the merger, and triggers reload hooks.

Output is the JSON result from the daemon, typically:
```json
{"status":"ok","commit":"abc1234","files_applied":["meept.json5","mcp_servers.json5"]}
```

### `meept config sync push`

Commit local configuration changes and push them to the shared config repository.

```bash
meept config sync push
meept config sync push -m "add node-c overrides"
```

Flags:
- `-m, --message` — commit message override. When empty, the daemon generates a default message with timestamp.

This dispatches via RPC (`config_sync.push`) to the running daemon. The daemon:
1. Stages all changes in the config-sync checkout
2. Commits with the provided or default message
3. Pushes to the remote repository

Other nodes will pick up the changes on their next pull cycle (or immediately via `meept config sync pull`).

## Troubleshooting

### Git conflict (dirty working tree)

**Symptoms**: Pull fails with "checkout is dirty" or merge errors.

**Cause**: The local config-sync checkout has uncommitted changes, preventing a clean pull. This can happen if configs were edited directly in the checkout directory, or if a previous merge wrote partial files.

**Fix**: Conflicts require manual resolution. The config syncer does not auto-resolve git conflicts.

```bash
# Inspect the checkout
cd ~/.meept/.config-sync/<repo-name>
git status

# Option 1: Discard local changes and accept remote
git checkout -- .
git pull

# Option 2: Stash local changes, pull, then re-apply
git stash
git pull
git stash pop

# Option 3: Reset to remote entirely (loses local edits)
git fetch origin
git reset --hard origin/main
```

After resolving, trigger a fresh pull:
```bash
meept config sync pull
```

### Invalid config skipped

**Symptoms**: Daemon logs show "config sync: merge error" and some files are listed in `FilesSkipped`.

**Cause**: A config file in the repo failed to parse (invalid JSON5, missing required fields, etc.). The syncer skips invalid files and continues with valid ones.

**Diagnosis**: Check daemon logs for the specific parse error. The error includes the file path and reason.

**Fix**: Correct the invalid file in the config repo and push. The next pull cycle will apply it.

### Reload hook failure

**Symptoms**: Config was pulled and merged successfully, but runtime behavior did not change.

**Cause**: A reload hook returned an error. For `mcp_servers.json5`, this usually means the MCP config is valid JSON5 but references an unreachable server, or the manager failed to restart a server.

**Diagnosis**:
```bash
# Check daemon logs for hook errors
journalctl -u meept -g "config sync"

# Verify MCP server status
meept mcp status
```

**Fix**: Correct the underlying issue (e.g., fix the MCP server URL) and trigger another pull. For files that require restart (`meept.json5`, `models.json5`, `backup.json5`), restart the daemon:

```bash
meept daemon restart
```

### Clone fails on first run

**Symptoms**: Daemon logs show "config sync: failed to clone" on startup.

**Cause**: The repository URL is wrong, or the daemon's SSH key does not have access.

**Fix**:
```bash
# Verify access
git ls-remote git@github.com:caimlas/meept-config.git

# Verify SSH key
ssh -T git@github.com
```

If the URL changed, clear the stale checkout:
```bash
rm -rf ~/.meept/.config-sync
meept daemon restart
```

### Changes pushed but not appearing on other nodes

**Cause**: Other nodes have not pulled yet (within their `pull_schedule` window), or their config sync is disabled.

**Fix**: On the other node, trigger an immediate pull:
```bash
meept config sync pull
```

Or wait for the next scheduled pull (default: 5 minutes).
