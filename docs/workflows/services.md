# Services

## Overview

Service layer (`internal/services/`) that shares business logic between RPC and HTTP transports.

## Problem

Without a shared service layer, business logic would be duplicated between transport handlers. The service layer provides a single source of truth for operations like chat, memory, tasks, queue management, sessions, workers, skills, self-improvement, cache, security, scheduler, bus, search, and plans.

## Behavior

- `ServiceRegistry` holds all service instances, wired via `NewRegistry(services.Config{...})` in the daemon.
- HTTP handlers in `internal/comm/http/api_handlers.go` call into services.
- RPC handlers (some via bus proxy, some direct) also call into services.
- **Search service** (`SearchService`): both keyword (`Search`) and semantic (`SearchSemantic`) search across sessions/tasks/memories/plans. Semantic uses embeddings when available, otherwise falls back to keyword.

## Configuration

Services read their dependencies (session store, task registry, memory manager, plan store) via `services.Config` at construction time. Nil deps are allowed; the corresponding scope returns no results.

## Edge Cases

- Nil dependencies: services gracefully return empty results rather than panicking.
- Import cycles: handler registration that would create a cycle (`rpc → services → scheduler → rpc`) lives in the `daemon` package instead (see `internal/daemon/search_rpc.go`).

---

*Updated with Global Semantic Search spec.*
