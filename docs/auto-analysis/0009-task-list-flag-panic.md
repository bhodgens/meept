# `meept task list` and `meept queue list` Panic on Flag Conflict

**Date**: 2026-05-15
**Phase**: 12 (CLI/TUI & session management)
**Severity**: high
**Status**: FIXED (already fixed - see below)

## Resolution

The flag conflict described in this issue has **already been resolved**. The current source code does not use `-s` as the shorthand for the `--state` flag:

- `cmd/meept/task.go:87`: `cmd.Flags().StringVar(&state, "state", "", ...)` (no shorthand)
- `cmd/meept/queue.go:150`: `cmd.Flags().StringVarP(&state, "state", "", "pending", ...)` (no shorthand)

The `-s` shorthand remains only on the global `--socket` flag. The `--state` flags use no shorthand in both files, so there is no conflict with the global `-s` shorthand.
