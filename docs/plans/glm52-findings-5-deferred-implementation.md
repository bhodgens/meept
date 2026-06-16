# GLM-5.2 Round 5 Findings — Deferred Implementation Plan (All Resolved)

## Overview

This document tracks the resolution of all initially-deferred findings (now all resolved).
from `docs/plans/glm52-findings-5.md`. All items were resolved in a
follow-up `oneshot-yeet` pass that dispatched 6 parallel fixer subagents
(one per cluster: S1, S2, S4, S5, S6, S7), plus a final commit for
S2-7 and S4-10.

**Total deferred findings: 68 (all resolved)**
**Resolved: 68/68 (100%)**
**Remaining: 0**

## Resolution Summary by Cluster

### S1 — Session/State (6 findings, all resolved)

| ID | Title | Resolution |
|----|-------|-----------|
| S1-5 | GetSession returns mutable pointer | Returns deep copy via `cloneTrackerSessionState` |
| S1-7 | classifyIntent error not logged | Warn-level log added at both call sites in `dispatcher.go` |
| S1-8 | EscalationLevel no monotonic seq | `Seq uint64` field + `atomic.Uint64` counter added |
| S1-9 | GetIdleSessions returns mutable slice | Returns deep copies (same helper as S1-5) |
| S1-10 | TeamSession.Status exposes maps | `TeamSessionStateSnapshot` type; copies under RLock |
| S1-11 | generateMessageID predictable | Fallback calls `id.Generate("msg")`; primary bumped 4→16 bytes |

### S2 — Plan/Graph/Parse (15 findings, all resolved)

| ID | Title | Resolution |
|----|-------|-----------|
| S2-6 | Plan parser scanner buffer too small | Buffer raised to 1MB |
| S2-7 | UpdatePlanStatus heuristic downgrades completed steps | Preserves existing `StepStatusCompleted`; regression test added |
| S2-9 | weightedLine edge ID collision | Uses `atomic.Int64` counter instead of ID packing |
| S2-10 | Applier patches first textual match | Returns error on ambiguous (multi-occurrence) snippets |
| S2-11 | Two RLocks around personalization loops | Single RLock wraps both loops |
| S2-12 | ProposedEdit lacks byte offsets | `StartByte`/`EndByte` fields; `ApplyEdits` uses them |
| S2-13 | Dead Warn block | Filled with real `slog.Warn` |
| S2-14 | Regex compiled per-call | Hoisted to package-level `var` |
| S2-15 | ConfirmPlan rejects valid statuses | Accepts `completed`, `failed`, `cancelled` |
| S2-16 | Plan handler no SQLite busy retry | 3 retries with 50/100/200ms backoff |
| S2-17 | Map delete before Close calls | Reordered: delete after Close |
| S2-18 | Hand-rolled parseCompositeDuration | Replaced with `time.ParseDuration` + preprocessor |
| S2-19 | Predictable IDs in plan package | All three call `id.Generate` |
| S2-20 | positionToByte no error return | Returns `(int, error)`; `ApplyEdits` skips with Warn |
| S2-21 | GoLinter hides raw stderr | Surfaces raw stderr as fallback `LinterResult` |

### S4 — Tools/MCP (10 findings, all resolved)

| ID | Title | Resolution |
|----|-------|-----------|
| S4-7 | Predictable IDs across tool package | 7 sites converted to `id.Generate(...)` |
| S4-9 | MCP tool error missing isError flag | `"isError": true` set on error |
| S4-10 | AskTool.TerminateHint docstring contradicts code | Docstring corrected; `false` is intentional |
| S4-11 | SSE parser tiny buffer / missing colonless prefix | 10MB buffer, both `data: `/`data:` prefixes |
| S4-12 | Client.Connect races with tool registration | Snapshots `len(c.tools)` under RLock |
| S4-13 | Bubble sort for role IDs | Replaced with `sort.Ints` |
| S4-14 | rawToMap silently drops errors | `slog.Debug` on unmarshal failure |
| S4-15 | MCP version string duplication | `const Version` in `internal/mcp`; client references it |
| S4-16 | StdioTransport.Close doesn't signal drain goroutine | Signals `stderrDone` via `sync.Once` |
| S4-17 | orderedMap loses insertion order | Struct with `[]string` keys + backing map |
| S4-18 | ExtractJSONFromText stops on first strategy | Tries each in turn; stops only on success |

### S5 — HTTP/Comm (11 findings, all resolved)

| ID | Title | Resolution |
|----|-------|-----------|
| S5-2 | CORS allows `*` | `isLocalOrigin` echo check |
| S5-3 | NotificationHandler dead token extraction | Deleted; relies on middleware |
| S5-6 | BusService.Stats missing presence checks | Uses `ok` checks |
| S5-8 | handleChatStream missing synthesized topic | Subscribes to both agent.progress topics |
| S5-9 | Dead method check in handleChatQueueStatus | Check + TrimPrefix fallback removed |
| S5-10 | http.Error instead of writeError | All replaced with `s.writeError` |
| S5-11 | handleReload swallows error | Returns error to dispatch |
| S5-12 | acceptLoop busy-loops on ErrClosed | Returns on ErrClosed, sleeps 50ms on transient |
| S5-13 | handleBusUnsubscribe async delete | Deletes synchronously before returning |
| S5-14 | MCP handlers ignore request context | Threads `r.Context()` through |
| S5-15 | Predictable event IDs | Already migrated (verified) |

### S6 — Queue/Runtime/PTY (9 findings, all resolved)

| ID | Title | Resolution |
|----|-------|-----------|
| S6-7 | Claim returns raw already-claimed error | Translates to `ErrNoJobAvailable` |
| S6-8 | CheckNodeReachability no context | Takes `ctx`, uses `QueryRowContext` |
| S6-9 | PTY Manager.Close holds lock during destroy | Snapshots IDs under lock, releases, then destroys |
| S6-10 | Debug Client.Close doesn't unblock readLoop | Stores `cancelFunc`; cancels before kill |
| S6-11 | Predictable debug session IDs | Already migrated (verified) |
| S6-13 | sentEvents unbounded growth | `map[string]time.Time`; LRU prunes 500 oldest |
| S6-14 | Pool.Scale holds lock during wait | RLock for count; documented pattern |
| S6-15 | Empty if body in parseGDBVariable | Deleted |
| S6-16 | scanClusterMember no key length validation | Validates `ed25519.PublicKeySize` |

### S7 — CLI/UI/Swift (15 findings, all resolved)

| ID | Title | Resolution |
|----|-------|-----------|
| S7-2 | saveConfig shadows filePath | Parameter renamed |
| S7-3 | Analytics shows 0.000000 cost | Prints `"n/a"` when `AvgCost == 0` |
| S7-4 | runTUI silently ignores http transport | Returns error on `--transport=http` |
| S7-5 | Unwired startAtLogin/showInMenuBar | Deleted per CLAUDE.md "no stub code" rule |
| S7-6/7/8/19 | Capitalized UI text | Lowercase sweep across Go + Swift files |
| S7-9 | Single isRefreshing conflates states | Split into `isRefreshingStatus` + `isControllingDaemon` |
| S7-10 | HistoricalReportView non-Identifiable items | `List` + `MetricPoint: Identifiable` |
| S7-13 | pluralize name collision | Renamed to `pluralizeEntry` |
| S7-14 | NotificationManager missing deinit | `deinit`, `disconnect`, `reconnect`; `print()` → `Logger` |
| S7-15 | WebSocketManager race on urlSession | Guards before flag flip; `print()` → `Logger` |
| S7-16 | MenubarConfigService uses print | `Logger.error` |
| S7-17 | /health requires auth | `makeRequest(requiresAuth:)`; `/health` passes `false` |
| S7-18 | Transport flag asymmetry undocumented | Documented in `cmd/meept/main.go` |
| S7-20 | LoadClientConfig silent on bad values | Warns + applies explicit defaults |

### Test Flakes (2, both resolved)

| Test | Root Cause | Resolution |
|------|-----------|-----------|
| TestAgentHandler_SessionPersistence | Background write races with test read | `writeAtomic` (temp + rename) helper; synchronous persistence |
| TestApp_SessionsKeyNavigatesToView | Modal.HandleKey case-sensitive for single chars | Case-insensitive matching for single-char keys |

## Verification

All fixes verified with:
- `go build ./...` — clean
- `go vet ./...` — clean
- `go test -count=1 -timeout 300s ./...` — all packages pass
- `swift build` in `menubar/` — 0 errors / 0 warnings
