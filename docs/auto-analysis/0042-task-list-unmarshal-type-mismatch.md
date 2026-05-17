# task list Command Fails with JSON Unmarshal Error

**Date**: 2026-05-15
**Phase**: 1
**Severity**: medium
**Status**: FIXED (2026-05-16)
**Component**: internal/task/registry.go, internal/tui/types/types.go
**Evaluation Dimension**: correctness

## Resolution

**Root cause**: The `handleList` method in `internal/task/registry.go` returned the raw slice from `h.registry.List()` (`[]*Task`), which serializes to a JSON array like `[{...}, {...}]`. But the client in `internal/tui/rpc.go` expects `TaskListResponse{Tasks: []Task}` which requires a JSON object like `{"tasks": [{...}, {...}]}`.

**Fix**: Wrapped the task list return value in a `map[string]any{"tasks": tasks}` in `handleList()` at `internal/task/registry.go:553-557`, matching the established response pattern used by other endpoints (e.g., queue.list already returns `{"jobs": jobs}`).

**Changes**:
- `internal/task/registry.go`: `handleList()` now returns `map[string]any{"tasks": tasks}` instead of bare `[]*Task`
- `internal/task/registry_test.go`: Updated `TestHandler_ListViaBus` to parse the wrapper response format
