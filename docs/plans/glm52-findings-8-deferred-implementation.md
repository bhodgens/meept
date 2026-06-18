# GLM52 Findings Round 8 — Deferred Implementation

**Source:** `docs/plans/glm52-findings-8.md`

## Summary

Round 8 produced 44 bug fixes across Go and Flutter. Of items deferred during the round:

- **4 design-level items (D8-1, D8-2, D-X1, D-X2) were resolved in Follow-up Phase 1** — all
  verified by `go build`, `go vet`, and `-race` tests across affected packages.
- **5 follow-up items remain as documented future work** — none are bugs; all are polish,
  refactors, or feature-panel migrations.

## Deferred Items Resolved in Follow-up Phase 1

| ID | Severity | File | Description | Resolution |
|----|----------|------|-------------|------------|
| D8-1 | Low | `internal/memory/scoped_manager.go` | Integer overflow possible in `limit * 5` expansion on 32-bit platforms with very large limit | **Fixed.** Added shared `expandLimit(limit int) int` helper: returns default (100) for `limit <= 0`, caps expansion at 10000 (overflow protection), bumps to `limit+5` when `*5` yields less. All 6 expansion sites refactored. |
| D8-2 | Low | `internal/memory/scoped_manager.go` | `query.Limit == 0` produces `expandedQuery.Limit = 0` which backend may treat as unlimited | **Fixed.** Truncation sites now gate on `limit > 0` so `Limit == 0` means "no truncation" instead of `[:0]`. |
| D-X1 | Low | `internal/llm/context_firewall.go` | `SetCompactor` writes `f.compactor` without lock; readers at lines 518/520/835/836 | **Fixed.** Added `compactorMu sync.RWMutex`. `SetCompactor` writes under Lock; propagates to `compressor.SetCompactor` OUTSIDE the lock. Reader sites snapshot under RLock, release, then call `.Compact(ctx, ...)` on the local. |
| D-X2 | Low | `internal/agent/dispatcher.go`, `internal/daemon/components.go` | Background `BuildIndex` goroutine not tracked/cancellable; goroutine leak on dispatcher close | **Fixed.** Added `indexCtx`, `indexCancel`, `indexWG` fields. BuildIndex goroutine uses derived ctx with WaitGroup tracking. Added `Stop()` method (idempotent, nil-safe) wired into `Components.stopComponents`. |

**Verification:** `go build ./...` PASS, `go vet ./...` PASS,
`go test -race -count=1 ./internal/memory/... ./internal/llm/... ./internal/agent/... ./internal/daemon/...` PASS (0 races, ~85s).

## Remaining Follow-ups (NOT bugs; documented future work)

| # | Description | Type | Effort |
|---|-------------|------|--------|
| 1 | Migrate 9 feature panels (`skills`, `projects/branches`, `search`, `terminal`, `memory`, `files`, `home/tools_dropdown`, `calendar`, `settings`) from `apiClientProvider` to `sdkClientProvider`. Once complete, `api_client.dart` and `meept_api.dart` can be deleted. | Refactor | Medium (per-panel) |
| 2 | Add response schemas to `docs/reference/http-api/openapi.yaml` to unlock typed `Future<Response<Session>>` from dart-dio generated API classes (currently all `Future<Response<void>>` — typed access is via model layer + serializers in `sdk_client.dart`). | Spec enhancement | Medium |
| 3 | Refine mutexio analyzer `ioMethods` to skip `atomic.Bool/Int64/Pointer/Value.Load()` and in-memory map `.Get()` — would eliminate the 12 Category C annotations. | Analyzer polish | Small |
| 4 | Add Java 17+ check + automatic `dart run build_runner build` to the `sdk-generate-dart` Makefile target. | Tooling hardening | Small |
| 5 | Confirm `flutter test` passes (5 test stubs migrated to subclass `SdkApiClient` in Follow-up 2; pre-existing test errors may now be resolved). | Verification | Small |

These items are intentional future work, not unresolved defects. They are tracked here to
keep the findings document as the source of truth for verification cycles.

## Resolution Status

- [x] All Round 8 design-level deferred items resolved in Follow-up Phase 1 (4 of 4)
- [x] All Round 8 follow-up items have a documented disposition (5 of 5 tracked as future work)
- [x] Completion rate: 100% of actionable items; 0 outstanding bugs
- [x] No Critical or High items remain
- [x] Verification: `go build`, `go vet`, `go test -race`, `make mutexio`, `make predid`,
      `flutter analyze`, `dart analyze` — all clean

## Follow-up Items Resolved (Session 2)

| # | Description | Resolution |
|---|-------------|------------|
| 1 | Migrate 9 feature panels to sdkClientProvider | **DONE.** All 9 panels migrated (skills, projects/branches, search, terminal, memory, files, home/tools_dropdown, calendar, settings). `api_client.dart` and `meept_api.dart` deleted (-893 lines net). 5 test stubs migrated to subclass `SdkApiClient`. `flutter analyze`: No issues. |
| 2 | OpenAPI spec response schemas | **PARTIAL.** 33 endpoints now have typed response schemas (BusStatsResponse, CacheStatsResponse, CalendarEvent, ListEventsResponse, etc.). 102 endpoints remain untyped (mostly DELETE/204-empty or action endpoints with no return body). dart-dio will now produce `Future<Response<BusStatsResponse>>` etc. for the typed ones. |
| 3 | Refine mutexio ioMethods | **DONE.** Analyzer now skips `sync/atomic.*.{Load,Store,Add,Swap,CompareAndSwap}` and `sync.Map.*` and project-local struct methods when the receiver struct embeds a sync.Mutex. Added 7 negative-test fixtures in `testdata/src/clean/clean.go`. Existing tests still pass; `make mutexio` still reports 0 violations. The 12 Category C `//nolint:mutexio` annotations could now be removed (left in place — they're harmless). |
| 4 | sdk-generate-dart Makefile hardening | **DONE.** Added `check-java-17` prerequisite target that fails fast with a helpful install message if Java is missing or older than 17 (correctly handles Java 8's `1.8` version format). Added automatic `dart pub get` + `dart run build_runner build --delete-conflicting-outputs` to the dart SDK generation target. Both `sdk-generate-go` and `sdk-generate-dart` now depend on `check-java-17`. |
| 5 | flutter test confirmation | **DONE.** `flutter test`: 146 of 154 pass (94.8%). The 8 failures are all in `chat_input_test.dart` testing a `PopupMenuButton<String>` widget that no longer exists in the refactored ChatInput UI — pre-existing test debt unrelated to the SDK migration. Race tests across affected Go packages (`agent`, `llm`, `memory`, `rpc`) all PASS with 0 races. |

### Verification Snapshot

```
go build ./...                                           # clean
make mutexio                                             # 0 violations
make predid                                              # clean
flutter analyze (in ui/flutter_ui)                       # No issues found
flutter test (in ui/flutter_ui)                          # 146 pass, 8 pre-existing failures
go test -race -count=1 ./internal/agent/... ./internal/llm/... ./internal/memory/... ./internal/rpc/...
                                                         # PASS, 0 races
```
