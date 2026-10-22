# handleChatStream synthCh duplicate fix -- bughunt 2026-10-16

## Finding

**File:** `internal/comm/http/api_handlers.go`
**Location:** `handleChatStream` function, select statement
**Issue:** Duplicate `case msg, ok := <-synthCh:` arms in the same select. The first arm (line 369) applies `sseSessionMatches(payload, filter)` session filter; the second arm (line 385) does NOT, causing synthesized progress from other sessions to leak into this SSE stream (Go picks randomly among ready duplicate cases).
**Severity:** HIGH
**Mechanism:** In Go's `select`, when multiple cases are ready, one is chosen pseudo-randomly. A duplicate channel receive case means synthesized events for unrelated sessions can be delivered to an SSE stream that should only receive filtered events.

## Fix Applied

The second `case msg, ok := <-synthCh:` arm (lines 385-396, the one WITHOUT the `sseSessionMatches` filter) was deleted, keeping only the filtered version.

**Before:** Two `case msg, ok := <-synthCh:` arms in the select.
**After:** One `case msg, ok := <-synthCh:` arm with `sseSessionMatches(payload, filter)` check.

## Verification

```bash
go build ./internal/comm/...      # PASS
go vet ./internal/comm/...        # PASS
grep -c 'case msg, ok := <-synthCh:' internal/comm/http/api_handlers.go  # 1
```

## Regression Test

No suitable existing test harness was found in `internal/comm/http/` for asserting synthesized SSE event filtering by session. The existing tests (`chat_submit_http_test.go`, `server_test.go`, `server_ws_*.go`) cover WS relay and chat submit functionality but not the SSE stream session-filtering behavior for synthesized events. A regression test would require adding a new test that:
1. Creates an SSE stream with a `session_id` filter
2. Publishes a synthesized progress event with a different `session_id`
3. Asserts the event is NOT received on the filtered stream

This was not implemented as per the instruction to state this in the report rather than building new e2e scaffolding.

## Checked and Clean

- `internal/comm/http/api_handlers.go` -- verified only one `synthCh` case remains
- Build and vet pass for `./internal/comm/...`

## Residual / Not Covered

- No regression test added (no suitable existing harness)

gates_run: go build ./internal/comm/... (PASS), go vet ./internal/comm/... (PASS), grep -c 'case msg, ok := <-synthCh:' internal/comm/http/api_handlers.go (1)
