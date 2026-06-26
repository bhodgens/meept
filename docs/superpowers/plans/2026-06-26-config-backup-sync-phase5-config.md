# Phase 5: Config Sync - Implementation Plan

**Spec:** `docs/superpowers/specs/2026-06-26-config-backup-sync-design.md`
**Date:** 2026-06-26
**Status:** Ready for implementation
**Prerequisites:** Phase 1 (Git Backup Scheduler) complete

---

## Overview

This plan implements configuration synchronization via git, enabling shared cluster-wide config and per-node overrides. Provides hot-reload of config changes without daemon restart.

### Scope

| In Scope | Out of Scope |
|----------|--------------|
| Config repo pull (shallow clone) | Backup scheduler (Phase 1) |
| Shared + per-node config merge | Gossip events (Phase 4) |
| Hot-reload hooks | Peer sync (Phase 2) |
| CLI: `config sync status`, `config sync pull` | |

---

## Phase 5 Checklist

### Task 5.1: Config Sync Scheduler

**File:** `internal/config/sync.go` (new)

```go
package config

type ConfigSyncer struct {
    repoURL       string
    checkoutDir   string
    nodeID        string
    pullSchedule  time.Duration
    logger        *slog.Logger

    // Callbacks for hot-reload
    reloadHooks map[string]ReloadFunc

    lastCommitHash string
    mu             sync.Mutex
}

// ReloadFunc is called when config changes
type ReloadFunc func(oldCfg, newCfg *Config) error

func NewConfigSyncer(cfg ConfigSyncConfig, nodeID string, logger *slog.Logger) (*ConfigSyncer, error)
func (s *ConfigSyncer) Start(ctx context.Context)
func (s *ConfigSyncer) pullOnce(ctx context.Context) error
func (s *ConfigSyncer) mergeConfigs(ctx context.Context) error
func (s *ConfigSyncer) RegisterReloadHook(path string, fn ReloadFunc)
```

**Implementation steps:**
1. Create `internal/config/sync.go`
2. Implement `Start()` - ticker loop for scheduled pulls
3. Implement `pullOnce()`:
   - Git pull (shallow clone for efficiency)
   - Compare commit hash with last known
   - If changed, call `mergeConfigs()`
4. Implement `mergeConfigs()`:
   - Copy `config/shared/*.json5` to `~/.meept/`
   - Copy `config/nodes/<node_id>/*.json5` (overrides)
   - Trigger reload hooks
5. Implement `RegisterReloadHook` for components to register callbacks

**Tests:** `internal/config/sync_test.go`

---

### Task 5.2: Config Sync Schema

**File:** `internal/config/sync_config.go` (update)

```go
type ConfigSyncConfig struct {
    Enabled      bool          `json:"enabled"`
    RepoURL      string        `json:"repo_url"`
    PullSchedule time.Duration `json:"pull_schedule"`  // "5m"
}

func DefaultConfigSyncConfig() ConfigSyncConfig {
    return ConfigSyncConfig{
        Enabled:      false,
        PullSchedule: 5 * time.Minute,
    }
}

func (c *ConfigSyncConfig) Validate() error {
    if c.Enabled && c.RepoURL == "" {
        return errors.New("config_sync: repo_url required when enabled")
    }
    return nil
}
```

**Update:** `internal/config/schema.go` - add `ConfigSync` field

**Tests:** `internal/config/sync_config_test.go`

---

### Task 5.3: Config Merge Logic

**File:** `internal/config/merger.go` (new)

```go
package config

// Merger handles config file merging
type Merger struct {
    baseDir    string  // ~/.meept/
    checkoutDir string // Git checkout path
    nodeID     string
    logger     *slog.Logger
}

func NewMerger(baseDir, checkoutDir, nodeID string, logger *slog.Logger) *Merger

// Merge applies shared + per-node configs
func (m *Merger) Merge() (*MergeResult, error)

type MergeResult struct {
    FilesApplied []string
    FilesSkipped []string
    Errors       []error
}

func (m *Merger) mergeSharedConfigs() error
func (m *Merger) mergeNodeOverrides() error
func (m *Merger) applyConfigFile(src, dst string) error
```

**Merge order:**
1. Shared configs: `config/shared/*.json5` → `~/.meept/`
2. Node overrides: `config/nodes/<node_id>/*.json5` → `~/.meept/` (overwrites)

**Deep merge for JSON5:**
```go
// For nested configs, merge deeply (not shallow overwrite)
func deepMerge(base, override json5.Object) json5.Object {
    result := base.Clone()
    for key, val := range override {
        if existing, ok := result[key]; ok {
            if existingObj, ok := existing.(json5.Object); ok {
                if overrideObj, ok := val.(json5.Object); ok {
                    result[key] = deepMerge(existingObj, overrideObj)
                    continue
                }
            }
        }
        result[key] = val
    }
    return result
}
```

**Tests:** `internal/config/merger_test.go`

---

### Task 5.4: Hot-Reload Hooks

**File:** `internal/config/reload_hooks.go` (new)

```go
package config

// ReloadRegistry manages reload callbacks
type ReloadRegistry struct {
    hooks map[string][]ReloadFunc
    mu    sync.RWMutex
}

func NewReloadRegistry() *ReloadRegistry

// Register adds a reload hook for a config path
func (r *ReloadRegistry) Register(path string, fn ReloadFunc)

// Trigger calls all hooks for a config path
func (r *ReloadRegistry) Trigger(path string, oldCfg, newCfg *Config) error

// Example reload hook registration
func (r *ReloadRegistry) RegisterLLMReload(fn func(old, new *LLMConfig) error) {
    r.Register("models.json5", func(oldCfg, newCfg *Config) error {
        return fn(oldCfg.LLM, newCfg.LLM)
    })
}

func (r *ReloadRegistry) RegisterBackupReload(fn func(old, new *BackupConfig) error) {
    r.Register("meept.json5", func(oldCfg, newCfg *Config) error {
        return fn(oldCfg.Backup, newCfg.Backup)
    })
}
```

**Integration points:**
- LLM client: Reload model config without restart
- Backup scheduler: Update backup config on change
- Cluster engine: Update cluster config on change

**Tests:** `internal/config/reload_hooks_test.go`

---

### Task 5.5: Git Operations

**File:** `internal/config/git_checkout.go` (new)

```go
package config

// GitCheckout manages the config repo checkout
type GitCheckout struct {
    repoURL   string
    checkoutDir string
    logger    *slog.Logger
}

func NewGitCheckout(repoURL, checkoutDir string, logger *slog.Logger) (*GitCheckout, error)

// Pull performs git pull (shallow clone if first time)
func (g *GitCheckout) Pull(ctx context.Context) (commitHash string, changed bool, err error)

// GetLatestCommit returns the latest commit hash
func (g *GitCheckout) GetLatestCommit() (string, error)

// IsDirty returns true if working tree has uncommitted changes
func (g *GitCheckout) IsDirty() bool
```

**Shallow clone strategy:**
```bash
# First pull
git clone --depth 1 <repo_url> <checkout_dir>

# Subsequent pulls
cd <checkout_dir> && git pull --depth 1
```

**Tests:** `internal/config/git_checkout_test.go`

---

### Task 5.6: CLI Commands

**File:** `cmd/meept/config_sync_cmd.go` (new)

```go
type ConfigSyncCommand struct {
    Status *ConfigSyncStatusCmd `cmd:"" help:"Show config sync status"`
    Pull   *ConfigSyncPullCmd   `cmd:"" help:"Force config refresh"`
}

type ConfigSyncStatusCmd struct{}
func (c *ConfigSyncStatusCmd) Run(cfg *Config) error

type ConfigSyncPullCmd struct{}
func (c *ConfigSyncPullCmd) Run(cfg *Config) error
```

**Output format for `config sync status`:**
```
Config Sync Status
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Repo: git@github.com:caimlas/meept-config.git
Last pull: 2 minutes ago
Last commit: abc123 (2026-06-25)

Shared configs applied:
  ✅ meept.json5
  ✅ models.json5
  ✅ mcp_servers.json5

Node overrides (machine-a):
  ✅ meept.json5 (overridden)
  ❌ models.json5 (not present)

Pending local changes: none
```

**Tests:** `cmd/meept/config_sync_cmd_test.go`

---

### Task 5.7: Daemon Wiring

**File:** `internal/daemon/daemon.go`

Add to daemon startup:
```go
// In wireConfigSync()
if cfg.ConfigSync.Enabled {
    configSyncer, err := config.NewConfigSyncer(cfg.ConfigSync, cfg.NodeID, logger)
    if err != nil {
        return fmt.Errorf("failed to create config syncer: %w", err)
    }

    // Register reload hooks for components
    configSyncer.RegisterReloadHook("meept.json5", func(oldCfg, newCfg *config.Config) error {
        // Reload backup scheduler config
        d.backupScheduler.UpdateConfig(newCfg.Backup)
        return nil
    })

    configSyncer.RegisterReloadHook("models.json5", func(oldCfg, newCfg *config.Config) error {
        // Reload LLM resolver
        return d.llmResolver.Reload(newCfg.LLM)
    })

    d.configSyncer = configSyncer
    go configSyncer.Start(d.ctx)
}
```

**Tests:** `internal/daemon/config_sync_wiring_test.go`

---

### Task 5.8: Local Config Push (Optional)

**File:** `internal/config/push.go` (new)

```go
// PushLocalChanges commits and pushes local config edits
func (s *ConfigSyncer) PushLocalChanges(ctx context.Context, message string) error

// Steps:
// 1. Check if config files modified in checkout dir
// 2. Git add, commit with message
// 3. Git push with rebase
// 4. Return error on conflict (user must resolve manually)
```

**Usage:**
```bash
# User edits shared config via CLI
meept config set backup.retention_days 30

# Push changes to repo
meept config sync push -m "Update retention to 30 days"
```

---

### Task 5.9: Error Handling

**File:** `internal/config/sync_errors.go` (new)

```go
type ConfigSyncError struct {
    Op    string  // "pull", "merge", "reload"
    Path  string  // Config file path
    Err   error
}

func (e *ConfigSyncError) Error() string

var (
    ErrGitConflict      = errors.New("config: git conflict (manual resolution required)")
    ErrConfigInvalid    = errors.New("config: invalid JSON5 syntax")
    ErrReloadFailed     = errors.New("config: hot-reload failed")
    ErrRepoUnreachable  = errors.New("config: git repo unreachable")
)
```

**Recovery:**
- Git conflict: Log, alert user, skip until resolved
- Invalid config: Skip file, use previous valid config
- Reload failed: Log, continue (config still valid, just not hot-reloaded)

---

### Task 5.10: Unit Tests

**Files:**
- `internal/config/sync_test.go`
- `internal/config/merger_test.go`
- `internal/config/reload_hooks_test.go`
- `internal/config/git_checkout_test.go`

**Coverage targets:**
- Merge logic: 95%+
- Reload hooks: 90%+
- Git operations: 90%+

---

## Acceptance Criteria

- [ ] `meept config sync status` shows sync state
- [ ] `meept config sync pull` triggers immediate refresh
- [ ] 5-minute scheduled pulls run automatically
- [ ] Shared configs applied to all nodes
- [ ] Per-node overrides work correctly
- [ ] Hot-reload hooks trigger on config change
- [ ] Git conflicts handled gracefully
- [ ] Invalid configs skipped (previous version retained)
- [ ] All unit tests pass with >90% coverage
- [ ] Documentation updated in `docs/configuration/config-sync.md`

---

## Configuration Example

```json5
// ~/.meept/meept.json5
{
  config_sync: {
    enabled: true,
    repo_url: "git@github.com:caimlas/meept-config.git",
    pull_schedule: "5m",
  }
}
```

---

## Config Repo Structure

```
meept-config-repo/
├── config/
│   ├── shared/
│   │   ├── meept.json5       # Cluster-wide main config
│   │   ├── models.json5      # LLM model resolution
│   │   └── mcp_servers.json5 # MCP server catalog
│   └── nodes/
│       ├── machine-a/
│       │   └── meept.json5   # Node A overrides
│       └── machine-b/
│           └── meept.json5   # Node B overrides
└── README.md
```

---

## Dependencies

| Package | Purpose |
|---------|---------|
| `gopkg.in/src-d/go-git.v4` | Git operations |
| `github.com/pelletier/go-toml/v2` | JSON5 parsing (existing) |

---

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Config pull corrupts local config | Atomic swap: write to temp, then rename |
| Reload hook panics | Recover in hook wrapper, log error |
| Git conflict on push | Skip push, notify user to resolve manually |
| Invalid config syntax | Validate before apply, skip invalid files |

---

## Estimated Effort

**Total tasks:** 10
**Estimated time:** 8-12 hours
**Complexity:** Medium (config merging, hot-reload)

---

*This plan implements Phase 5 of 7 from the backup/sync design spec.*
