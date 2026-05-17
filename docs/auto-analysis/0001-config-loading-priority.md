# Config Loading Priority Shadows User Config

**Date**: 2026-05-15
**Phase**: 0 (prerequisite setup)
**Severity**: high
**Component**: `internal/daemon/components.go`, `internal/config/config.go`, `internal/llm/providers.go`

## Description

When running the meept daemon from the project directory (`~/git/meept/`), the models configuration is loaded from `config/models.json5` (project-local, CWD-relative) **before** `~/.meept/models.json5` (user config). This means:

1. Changes to `~/.meept/models.json5` are silently ignored when running from the project directory
2. `make install` copies templates to `~/.meept/models.json5` but the daemon doesn't use them
3. Users who edit their home config won't see changes take effect if they happen to `cd` into the source tree

The same priority exists in at least 3 places:
- `internal/daemon/components.go:loadModelsConfigWithPath()` (lines 1462-1492)
- `internal/config/config.go:LoadModelsConfigDefault()` (lines 240-253)
- `internal/llm/providers.go:LoadProvidersConfigDefault()` (lines 82-96)

## Reproduction

1. Edit `~/.meept/models.json5` to add a new provider
2. `cd ~/git/meept && ./bin/meept-daemon -f`
3. Observe logs: `Loaded models configuration path=config/models.json5` — loads from project-local
4. User config is ignored despite existing at `~/.meept/models.json5`

## Evidence

From daemon debug logs:
```
level=DEBUG msg="Found models config" path=config/models.json5
level=INFO msg="Loaded models configuration" path=config/models.json5 default_model=zai/glm-4.7 small_model=zai/glm-4.5-air
```

Meanwhile `~/.meept/models.json5` has `small_model=local/lfm-code` which is never loaded.

## Root Cause

The config loading functions check `config/models.json5` (relative to CWD) first. When the daemon is launched from the project directory, this file always exists and takes priority. The `~/.meept/` path is only tried as a fallback.

This is a **priority inversion**: the project-local config is meant for development defaults, but it overrides user customizations in production use.

## Proposed Fix

Reverse the priority order: `~/.meept/models.json5` should take precedence over `config/models.json5`. This matches the behavior of other config files (agents, skills) where user-level config shadows project-level.

Alternative: add a `--models-config` CLI flag to explicitly specify the path, with default priority being `~/.meept/` > `config/`.

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both
