# 0055 - ClawSkills system is not implemented

- **Phase**: 8 (Skills System)
- **Severity**: High
- **Status**: Confirmed gap
- **Tested**: 2026-05-16

## Symptom

- `meept clawskills list` → `Error: accepts at most 1 arg(s), received 2`
- `meept clawskills search "test"` → `Error: accepts at most 1 arg(s), received 3`
- `meept clawskills --help` → Falls through to main help, no clawskills subcommand.
- No `clawskills` subcommand exists in CLI.

## Investigation

Searched entire codebase for clawskills implementation:

| Location | Status |
|----------|--------|
| `cmd/meept/*.go` | No clawskills subcommand defined |
| `internal/rpc/*.go` | No clawskills RPC handlers |
| `internal/daemon/daemon.go` | No clawskills wiring |
| `internal/config/schema.go` | No `ClawSkillsConfig` struct |
| `internal/` (any Go package) | No clawskills package |
| Go module imports | No clawskills import |

## What exists

| Artifact | Present? |
|----------|----------|
| Docs (`docs/reference/cli.md`) | Documents `clawskills search/install/list/update` commands |
| Config template (`config/meept.toml`) | Has `[clawskills]` section with registry_url, install_dir, etc. |
| User config (`~/.meept/meept.toml`) | Has `[clawskills]` section |
| `~/.meept/clawskills/` directory | Exists but is empty |
| Go source code | **Completely absent** |

## Gap

`clawskills` is fully documented and configured but has zero implementation in Go. The entire third-party skill marketplace feature (search, install, update, manage) needs to be built from scratch.

At minimum, the docs are misleading -- they document commands that don't exist. The `[clawskills]` config section is silently ignored because there is no `ClawSkillsConfig` in the schema.
