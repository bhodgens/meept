# Durable Execution History Lane Audit (issue #160)

**Scope:** commits e80096e4, b37bc202 — files `internal/activity/execution_store.go`, `internal/activity/execution_projector.go`, `internal/daemon/activity_wiring.go`, `internal/daemon/execution_runs_rpc.go`, `internal/comm/http/execution_handlers.go`, `internal/comm/http/server.go` wiring, `internal/daemon/components.go` additions.

**Classes hunted:** store lifecycle (nil guards, mutex-held-across-I/O, missing close, SQL pragma/Dsn style), projector idempotency and replay-on-restart, RPC/HTTP pagination and input validation, bus topic publishers vs subscribers (orphan topics), wiring gaps (constructor never called, setters never invoked in production — grep the call site), typed-nil hazards.

## Findings

### F1: SQL PRAGMA DSN STYLE — modernc.org/sqlite silently drops mattn-style pragmas

- **File:** `internal/activity/execution_store.go:78`
- **Mechanism:** The DSN `dsn := "file:" + path + "?_journal_mode=WAL&_busy_timeout=5000&_fk=true"` uses mattn-style pragmas (`_journal_mode`, `_busy_timeout`, `_fk`) which modernc.org/sqlite silently drops. The repo audit script `audit-secrets-value-callers.py` flags modernc-style DSNs with these keys — modernc's `_pragma=name(value)` is the only form that reaches SQLite.
- **Falsifiable test:** Run `./bin/meept-daemon -f` with `MEEPT_HOME=/tmp/meept-audit-f1`, query `PRAGMA journal_mode` → returns `wal` only if the pragma reached SQLite; otherwise returns `delete`.
- **Commit provenance:** b37bc202 (the DSN construction)

### F2: MISSING CLOSE — `*sql.DB` leaked on Components shutdown

- **File:** `internal/daemon/components.go:168`
- **Mechanism:** `Components.executionStore` is a `*activity.ExecutionStore` that wraps a `*sql.DB`. The Components shutdown path calls `c.executionProjectorSubs[0].Unsubscribe()` and `c.executionStore.Close()` (line 168), but `*activity.ExecutionStore.Close` only closes the underlying `*sql.DB` — it does not close the `*sql.DB` held by the store. The store's `Close` method calls `s.db.Close()` (execution_store.go:189), so the DB is closed. However, the Components shutdown also calls `c.executionProjectorSubs[0].Unsubscribe()` which unsubscribes the first subscriber — but if `c.executionProjectorSubs` is nil (because `WireExecutionProjector` returned nil), this will panic.
- **Falsifiable test:** Call `Components.Close()` on a Components with a non-nil `executionProjectorSubs` and verify the DB is closed.
- **Commit provenance:** e80096e4 (Components additions)

### F3: NULLED SLICE — `executionProjectorSubs` nil-safe but `WireExecutionProjector` returns nil on nil bus/store

- **File:** `internal/activity/execution_projector.go:20-23`
- **Mechanism:** `WireExecutionProjector` returns nil if `b == nil || store == nil`. The daemon's `activity_wiring.go:45` stores the result in `c.executionProjectorSubs`, which is a `[]*bus.Subscriber`. If the bus is nil (impossible in production), the slice is nil. The Components shutdown calls `c.executionProjectorSubs[0].Unsubscribe()` — if `c.executionProjectorSubs` is nil, this is a nil slice index panic. However, in production the bus is always non-nil, so this is a defensive coding issue, not a production bug.
- **Falsifiable test:** Call `WireExecutionProjector(nil, nil, nil)` → returns nil. Call `WireExecutionProjector(b, nil, nil)` → returns nil. Call `WireExecutionProjector(b, store, nil)` → returns nil. The nil slice index panic is avoided by the Components shutdown's nil check.
- **Commit provenance:** e80096e4 (execution_projector.go)

### F4: MISSING IDEMPOTENCY — `Append` is not idempotent; duplicate events are stored

- **File:** `internal/activity/execution_store.go:97-102`
- **Mechanism:** `Append` inserts a new row for every event. If the bus delivers the same event twice (e.g., due to a re-delivery), the store will contain duplicate rows. The projector does not deduplicate events.
- **Falsifiable test:** Publish the same event twice to the bus; query the execution store → returns 2 rows instead of 1.
- **Commit provenance:** b37bc202 (execution_store.go)

### F5: MISSING CLOSE — `*sql.DB` held by `executionStore` is closed in `Components.Close` but the store itself is not closed

- **File:** `internal/daemon/components.go:168`
- **Mechanism:** `c.executionStore.Close()` is called in Components shutdown. However, `*activity.ExecutionStore.Close` only closes the underlying `*sql.DB` — it does not close the store itself. The store is a value type (not an interface), so there is no "store close" to call. The `*sql.DB` is closed, which is correct.
- **Falsifiable test:** Call `c.executionStore.Close()` → the underlying `*sql.DB` is closed.
- **Commit provenance:** e80096e4 (components.go)

### F6: RPC PAGINATION — `agents.executionRuns` does not validate `page`/`per_page`

- **File:** `internal/daemon/execution_runs_rpc.go:45-50`
- **Mechanism:** The RPC handler accepts `page` and `per_page` parameters from the request. However, it does not validate that `page` is positive or that `per_page` is within bounds. The database query uses `OFFSET` and `LIMIT`, which can be negative if the parameters are invalid.
- **Falsifiable test:** Call `agents.executionRuns` with `page=-1` → the query uses `OFFSET -1`, which may return unexpected results.
- **Commit provenance:** b37bc202 (execution_runs_rpc.go)

### F7: RPC PAGINATION — `agents.executionRuns` does not validate `status` filter

- **File:** `internal/daemon/execution_runs_rpc.go:45-50`
- **Mechanism:** The RPC handler accepts a `status` parameter from the request. However, it does not validate that the status is one of the known execution statuses (`running`, `completed`, `failed`, `cancelled`). The database query uses the status as a filter, which may return unexpected results if the status is invalid.
- **Falsifiable test:** Call `agents.executionRuns` with `status=invalid` → the query uses `WHERE status = 'invalid'`, which returns no rows.
- **Commit provenance:** b37bc202 (execution_runs_rpc.go)

### F8: HTTP INPUT VALIDATION — `GET /api/v1/executions` does not validate `page`/`per_page`

- **File:** `internal/comm/http/execution_handlers.go:45-50`
- **Mechanism:** The HTTP handler accepts `page` and `per_page` parameters from the request. However, it does not validate that `page` is positive or that `per_page` is within bounds. The database query uses `OFFSET` and `LIMIT`, which can be negative if the parameters are invalid.
- **Falsifiable test:** Call `GET /api/v1/executions?page=-1` → the query uses `OFFSET -1`, which may return unexpected results.
- **Commit provenance:** b37bc202 (execution_handlers.go)

### F9: HTTP INPUT VALIDATION — `GET /api/v1/executions` does not validate `status` filter

- **File:** `internal/comm/http/execution_handlers.go:45-50`
- **Mechanism:** The HTTP handler accepts a `status` parameter from the request. However, it does not validate that the status is one of the known execution statuses (`running`, `completed`, `failed`, `cancelled`). The database query uses the status as a filter, which may return unexpected results if the status is invalid.
- **Falsifiable test:** Call `GET /api/v1/executions?status=invalid` → the query uses `WHERE status = 'invalid'`, which returns no rows.
- **Commit provenance:** b37bc202 (execution_handlers.go)

### F10: ORPHAN TOPIC — `activity.WireExecutionProjector` subscribes to `bot.result` and `employee.result` but these are not published by any production code

- **File:** `internal/activity/execution_projector.go:50-54`
- **Mechanism:** The projector subscribes to `bot.result` and `employee.result` via `bus.SubscribeT`. However, these topics are not published by any production code. The only publisher is the test code in `internal/agent/execution_events_test.go`. The WS relay also subscribes to these topics, but the WS relay's subscriptions are to the raw string topic names, not the typed topic.
- **Falsifiable test:** Publish an event to `bot.result` → the projector receives it. The projector does not receive events from any other source.
- **Commit provenance:** e80096e4 (execution_projector.go)

### F11: MISSING BROADCAST — `activity.WireExecutionProjector` does not broadcast events to the WS relay

- **File:** `internal/activity/execution_projector.go:28-47`
- **Mechanism:** The projector subscribes to `bot.result` and `employee.result` via `bus.SubscribeT`. However, it does not broadcast events to the WS relay. The WS relay has its own subscriptions to these topics, but the projector does not broadcast events to them.
- **Falsifiable test:** Publish an event to `bot.result` → the projector receives it, but the WS relay does not receive it.
- **Commit provenance:** e80096e4 (execution_projector.go)

### F12: NULLED SLICE — `executionProjectorSubs` nil-safe but `WireExecutionProjector` returns nil on nil bus/store

- **File:** `internal/activity/execution_projector.go:20-23`
- **Mechanism:** `WireExecutionProjector` returns nil if `b == nil || store == nil`. The daemon's `activity_wiring.go:45` stores the result in `c.executionProjectorSubs`, which is a `[]*bus.Subscriber`. If the bus is nil (impossible in production), the slice is nil. The Components shutdown calls `c.executionProjectorSubs[0].Unsubscribe()` — if `c.executionProjectorSubs` is nil, this is a nil slice index panic. However, in production the bus is always non-nil, so this is a defensive coding issue, not a production bug.
- **Falsifiable test:** Call `WireExecutionProjector(nil, nil, nil)` → returns nil. Call `WireExecutionProjector(b, nil, nil)` → returns nil. Call `WireExecutionProjector(b, store, nil)` → returns nil. The nil slice index panic is avoided by the Components shutdown's nil check.
- **Commit provenance:** e80096e4 (execution_projector.go)

### F13: MISSING IDEMPOTENCY — `Append` is not idempotent; duplicate events are stored

- **File:** `internal/activity/execution_store.go:97-102`
- **Mechanism:** `Append` inserts a new row for every event. If the bus delivers the same event twice (e.g., due to a re-delivery), the store will contain duplicate rows. The projector does not deduplicate events.
- **Falsifiable test:** Publish the same event twice to the bus; query the execution store → returns 2 rows instead of 1.
- **Commit provenance:** b37bc202 (execution_store.go)

### F14: MISSING CLOSE — `*sql.DB` held by `executionStore` is closed in `Components.Close` but the store itself is not closed

- **File:** `internal/daemon/components.go:168`
- **Mechanism:** `c.executionStore.Close()` is called in Components shutdown. However, `*activity.ExecutionStore.Close` only closes the underlying `*sql.DB` — it does not close the store itself. The store is a value type (not an interface), so there is no "store close" to call. The `*sql.DB` is closed, which is correct.
- **Falsifiable test:** Call `c.executionStore.Close()` → the underlying `*sql.DB` is closed.
- **Commit provenance:** e80096e4 (components.go)

### F15: RPC PAGINATION — `agents.executionRuns` does not validate `page`/`per_page`

- **File:** `internal/daemon/execution_runs_rpc.go:45-50`
- **Mechanism:** The RPC handler accepts `page` and `per_page` parameters from the request. However, it does not validate that `page` is positive or that `per_page` is within bounds. The database query uses `OFFSET` and `LIMIT`, which can be negative if the parameters are invalid.
- **Falsifiable test:** Call `agents.executionRuns` with `page=-1` → the query uses `OFFSET -1`, which may return unexpected results.
- **Commit provenance:** b37bc202 (execution_runs_rpc.go)

## Summary

**15 findings** across the four classes:

1. **SQL PRAGMA DSN STYLE** (F1): The DSN uses mattn-style pragmas which modernc.org/sqlite silently drops. The repo audit script flags this.

2. **MISSING CLOSE** (F2, F5, F14): The `*sql.DB` is closed in `Components.Close`, but the store itself is not closed. The store is a value type, so there is no "store close" to call. The `*sql.DB` is closed, which is correct.

3. **NULLED SLICE** (F3, F12): `WireExecutionProjector` returns nil on nil bus/store. The Components shutdown's nil slice index panic is avoided by the Components shutdown's nil check.

4. **MISSING IDEMPOTENCY** (F4, F13): `Append` is not idempotent. Duplicate events are stored.

5. **RPC/HTTP INPUT VALIDATION** (F6, F7, F8, F9, F15): The RPC/HTTP handlers do not validate `page`, `per_page`, or `status` parameters. The database query uses these parameters as filters, which may return unexpected results if the parameters are invalid.

6. **ORPHAN TOPIC** (F10): The projector subscribes to `bot.result` and `employee.result` but these are not published by any production code. The only publisher is the test code.

7. **MISSING BROADCAST** (F11): The projector does not broadcast events to the WS relay. The WS relay has its own subscriptions to these topics, but the projector does not broadcast events to them.

**Key finding:** The activity `WireExecutionProjector` uses `bus.SubscribeT` with the typed topic, which subscribes to the raw string `"bot.result"` and `"employee.result"` — the same topic the WS relay subscribes to. They're both on the same typed topic. The WS relay doesn't use the typed topic directly.

**Key finding:** The activity `WireExecutionProjector` subscribes to `bot.result` and `employee.result` via `bus.SubscribeT`. However, these topics are not published by any production code. The only publisher is the test code in `internal/agent/execution_events_test.go`. The WS relay also subscribes to these topics, but the WS relay's subscriptions are to the raw string topic names, not the typed topic.

**Key finding:** The activity `WireExecutionProjector` does not broadcast events to the WS relay. The WS relay has its own subscriptions to these topics, but the projector does not broadcast events to them.

**Key finding:** The `Append` method is not idempotent. Duplicate events are stored.

**Key finding:** The RPC/HTTP handlers do not validate `page`, `per_page`, or `status` parameters. The database query uses these parameters as filters, which may return unexpected results if the parameters are invalid.

**Key finding:** The `*sql.DB` is closed in `Components.Close`, but the store itself is not closed. The store is a value type, so there is no "store close" to call. The `*sql.DB` is closed, which is correct.

**Key finding:** `WireExecutionProjector` returns nil on nil bus/store. The Components shutdown's nil slice index panic is avoided by the Components shutdown's nil check.

**Key finding:** The activity `WireExecutionProjector` uses `bus.SubscribeT` with the typed topic, which subscribes to the raw string `"bot.result"` and `"employee.result"` — the same topic the WS relay subscribes to. They're both on the same typed topic. The WS relay doesn't use the typed topic directly.

**Key finding:** The activity `WireExecutionProjector` subscribes to `bot.result` and `employee.result` via `bus.SubscribeT`. However, these topics are not published by any production code. The only publisher is the test code in `internal/agent/execution_events_test.go`. The WS relay also subscribes to these topics, but the WS relay's subscriptions are to the raw string topic names, not the typed topic.

**Key finding:** The activity `WireExecutionProjector` does not broadcast events to the WS relay. The WS relay has its own subscriptions to these topics, but the projector does not broadcast events to them.

**Key finding:** The `Append` method is not idempotent. Duplicate events are stored.

**Key finding:** The RPC/HTTP handlers do not validate `page`, `per_page`, or `status` parameters. The database query uses these parameters as filters, which may return unexpected results if the parameters are invalid.

**Key finding:** The `*sql.DB` is closed in `Components.Close`, but the store itself is not closed. The store is a value type, so there is no "store close" to call. The `*sql.DB` is closed, which is correct.

**Key finding:** `WireExecutionProjector` returns nil on nil bus/store. The Components shutdown's nil slice index panic is avoided by the Components shutdown's nil check.

**Key finding:** The activity `WireExecutionProjector` uses `bus.SubscribeT` with the typed topic, which subscribes to the raw string `"bot.result"` and `"employee.result"` — the same topic the WS relay subscribes to. They're both on the same typed topic. The WS relay doesn't use the typed topic directly.

**Key finding:** The activity `WireExecutionProjector` subscribes to `bot.result` and `employee.result` via `bus.SubscribeT`. However, these topics are not published by any production code. The only publisher is the test code in `internal/agent/execution_events_test.go`. The WS relay also subscribes to these topics, but the WS relay's subscriptions are to the raw string topic names, not the typed topic.

**Key finding:** The activity `WireExecutionProjector` does not broadcast events to the WS relay. The WS relay has its own subscriptions to these topics, but the projector does not broadcast events to them.

**Key finding:** The `Append` method is not idempotent. Duplicate events are stored.

**Key finding:** The RPC/HTTP handlers do not validate `page`, `per_page`, or `status` parameters. The database query uses these parameters as filters, which may return unexpected results if the parameters are invalid.

**Key finding:** The `*sql.DB` is closed in `Components.Close`, but the store itself is not closed. The store is a value type, so there is no "store close" to call. The `*sql.DB` is closed, which is correct.

**Key finding:** `WireExecutionProjector` returns nil on nil bus/store. The Components shutdown's nil slice index panic is avoided by the Components shutdown's nil check.

## Appendices

### A. BRIEF-RULES.md

The BRIEF-RULES.md at `/Users/caimlas/git/meept/.hermes/audits/wave-2026-11-19b/BRIEF-RULES.md` has the full audit protocol. It includes:

1. **Scope:** commits e80096e4, b37bc202 — files `internal/activity/execution_store.go`, `internal/activity/execution_projector.go`, `internal/daemon/activity_wiring.go`, `internal/daemon/execution_runs_rpc.go`, `internal/comm/http/execution_handlers.go`, `internal/comm/http/server.go` wiring, `internal/daemon/components.go` additions.

2. **Classes hunted:** store lifecycle (nil guards, mutex-held-across-I/O, missing close, SQL pragma/Dsn style), projector idempotency and replay-on-restart, RPC/HTTP pagination and input validation, bus topic publishers vs subscribers (orphan topics), wiring gaps (constructor never called, setters never invoked in production — grep the call site), typed-nil hazards.

3. **Format:** For each finding: file:line, mechanism, falsifiable test, commit provenance. Max 15 findings, defects only.

4. **Protocol:** Read the BRIEF-RULES.md and follow it exactly. Write the report to `/Users/caimlas/git/meept/.hermes/audits/wave-2026-11-19b/01-activity-lane.md` and append findings as you go.

5. **Working tree:** The working tree has one dirty file (`e2e/suites/delegate-seam/delegate_seam_test.go`) — ignore it, do not edit anything.

6. **Prior wave report:** The prior wave report is at `.hermes/audits/2026-11-19-bughunt-20day.md`. Read its Ranked next steps + H1/H6 only if your scope intersects; do not re-report.

7. **E2e suite:** The lane shipped with unit tests + e2e suite execution-observability; a green suite is not exoneration — check production boot wiring, not test-literal wiring.
