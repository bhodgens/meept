# Fix Report: memory backend default case & effects key convention

## Finding 1: Memory backend no default case
**File:** `internal/memory/manager.go:222`
**Change:** Added a `default:` case to the `switch m.config.Backend` statement that logs a `slog.Warn` with the unknown backend value and sets `wantSQLite = true, wantMemvid = true` to match the empty-case fallback behavior.
**Regression test:** `TestManager_BackendDefaultCase` in `internal/memory/manager_test.go` sets `Backend: "sqlie"`, runs `Initialize`, and asserts a warning is logged containing "unknown memory backend value" and "sqlie", and that SQLite backends were initialized as fallback.

## Finding 2: Effects key convention unenforced
**File:** `internal/effects/key.go`
**Change:** Added named constructors `GitPushKey(remotePath, headSHA string)` and `PushNotifyKey(sessionsJSON, source, typ, content string)` that wrap `EffectKey` with the documented argument order. Added a doc comment stating new effect types must add a named constructor.
**Migration:** 
- `internal/backup/git_ops.go:270` already calls `effects.GitPushKey(remotePath, headSHA)` -- no change needed.
- `internal/services/push_service.go:168` already calls `effects.PushNotifyKey(...)` -- no change needed.
**Regression test:** `TestNamedConstructorsMatchEffectKey` in `internal/effects/key_test.go` pins both the old inline `EffectKey` composition and the new named constructors, asserting they produce identical key strings.

## Gates
- `go build ./internal/memory/... ./internal/effects/... ./internal/backup/... ./internal/services/...` -- PASS
- `go vet ./internal/memory/... ./internal/effects/... ./internal/backup/... ./internal/services/...` -- PASS
- `go test -p 2 -run 'Backend|Key|Effects' ./internal/memory/... ./internal/effects/... ./internal/backup/... ./internal/services/...` -- PASS

## Observations
- No observations made during this fix wave.

gates_run: go build ./internal/memory/... ./internal/effects/... ./internal/backup/... ./internal/services/... (PASS); go vet ./internal/memory/... ./internal/effects/... ./internal/backup/... ./internal/services/... (PASS); go test -p 2 -run 'Backend|Key|Effects' ./internal/memory/... ./internal/effects/... ./internal/backup/... ./internal/services/... (PASS)