# Services

## Overview

Service layer (`internal/services/`) that shares business logic between RPC and HTTP transports.

## Problem

Without a shared service layer, business logic would be duplicated between transport handlers. The service layer provides a single source of truth for operations like chat, memory, tasks, queue management, sessions, workers, skills, self-improvement, cache, security, scheduler, bus, search, and plans.

## Behavior

- `ServiceRegistry` holds all service instances, wired via `NewRegistry(services.Config{...})` in the platform.
- HTTP handlers in `internal/comm/http/api_handlers.go` call into services.
- RPC handlers (some via bus proxy, some direct) also call into services.
- **Search service** (`SearchService`): both keyword (`Search`) and semantic (`SearchSemantic`) search across sessions/tasks/memories/plans. Semantic uses embeddings when available, otherwise falls back to keyword.
- **Prompt service** (`PromptService`): the 4-tier prompts hierarchy exposed over HTTP/RPC at `/api/v1/prompts`. See [prompt directory resolution](#prompt-directory-resolution).

## Prompt directory resolution

The prompt service walks four tiers, highest priority first:

| tier | directory | resolved by |
|------|-----------|-------------|
| project | `<active project>/.meept/prompts` | the active project (`ProjectManager.GetActive`), never the daemon CWD |
| user | `$MEEPT_HOME/prompts` (default `~/.meept/prompts`) | `config.MeeptPath`, so it honors `$MEEPT_HOME` |
| system | `~/.config/meept/prompts` | the user config dir |
| bundled | shipped `config/prompts` | the daemon, from the repo/executable layout (not the CWD) |

The daemon resolves all four directories at startup and passes them to
`services.NewRegistry` via `services.Config.PromptDirs`
(`internal/daemon/prompt_dirs.go`). The service is constructed with
`services.NewPromptServiceFromDirs`; an installed daemon launched from any
working directory therefore returns the same prompt list as one launched from
the repo root.

Two invariants keep the list honest:

- Empty tier directories are dropped, so a `filepath.Join("", name)` lookup can
  never silently read from the process CWD.
- When the project and user directories resolve to the same path (for example a
  caller that derived the project dir from a CWD equal to the meept home), the
  project tier is dropped and entries are labelled `user`, not `project`.

The list response shape is unchanged: `GET /api/v1/prompts` returns
`{"prompts": [...]}` and each entry carries a `tier` label.

## Configuration

Services read their dependencies (session store, task registry, memory manager, plan store) via `services.Config` at construction time. Nil deps are allowed; the corresponding scope returns no results.

## Edge Cases

- Nil dependencies: services gracefully return empty results rather than panicking.
- Import cycles: handler registration that would create a cycle (`rpc → services → scheduler → rpc`) lives in the `daemon` package instead (see `internal/daemon/search_rpc.go`).

---

*Updated with Global Semantic Search spec.*
