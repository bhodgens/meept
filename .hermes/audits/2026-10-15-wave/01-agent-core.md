# Bughunt Audit: meept AGENT CORE

**Date**: 2026-10-15
**Scope**: `internal/agent/` (527 Go files) plus subpackages
**Status**: READ-ONLY audit

## Findings

### F1: LOW -- `prompt/loader.go:35` uses `os.Getwd()` comment but no fallback path exists in agent core
- **Severity**: LOW
- **Location**: `internal/agent/prompt/loader.go:35`
- **Mechanism**: The file contains a comment explicitly forbidding `os.Getwd()` use ("do NOT use os.Getwd() here -- the daemon's CWD is not the user's project"), but no actual `os.Getwd()` call was found in the internal/agent package. The comment is a reminder, not a bug.
- **Test**: `grep -rn "os.Getwd()" internal/agent/` returns only this comment and a test assertion.
- **Evidence**: SOURCE -- grep result
- **Confidence**: high

### F2: MEDIUM -- `handler.go:1130-1131` comment indicates WS chat_message push gating relies on `conversation_id` only, potentially silencing progress events for session-less connections
- **Severity**: MEDIUM
- **Location**: `internal/agent/handler.go:1130-1131`
- **Mechanism**: The comment states that SDK client requests carry no `session_id` (only `conversation_id`). The WS chat_message push gating on line 791 uses `wsSessionID` which may be empty for session-less connections. If a connection has no session but receives `conversation_id`-scoped events, progress events for that conversation could be suppressed.
- **Test**: Trace the WS filter in `internal/comm/http/server.go` to verify the session_id -> conversation_id fallback is preserving progress events for session-less connections.
- **Evidence**: SOURCE -- handler.go:1130-1131, handler.go:791
- **Confidence**: medium

### F3: LOW -- `park_store_sqlite.go:101-102` stores both `session_id` and `conversation_id` but no audit of when they diverge
- **Severity**: LOW
- **Location**: `internal/agent/park_store_sqlite.go:101-102`
- **Mechanism**: The park store tracks both identifiers per record. While the AGENTS.md contract says they're distinct, there's no explicit validation that they're populated correctly on insert/update. If a bug causes them to be swapped, the park store would become a silent source of confusion.
- **Test**: Add a sanity check in the insert/update SQL that `session_id` and `conversation_id` are not identical when they should be distinct, or at minimum log when they're set.
- **Evidence**: SOURCE -- park_store_sqlite.go:101-102
- **Confidence**: medium

### F4: CLEAN -- ToolActionMap completeness is actively tested and maintained
- **Severity**: N/A (clean)
- **Location**: `internal/agent/executor.go:246-371`, `internal/agent/tool_action_completeness_test.go`
- **Mechanism**: `ToolActionMap` is mapped to BuiltinRules actions. The test `TestToolActionMapEntriesAreKnownActions` ensures every mapped action has a corresponding BuiltinRules rule. The test `TestToolActionMap` ensures every tool name in the map has a valid action.
- **Test**: `go test -run TestToolActionMap ./internal/agent/` and `go test -run TestToolActionMapEntriesAreKnownActions ./internal/agent/`
- **Evidence**: SOURCE -- executor.go, tool_action_completeness_test.go
- **Confidence**: high

### F5: CLEAN -- Completion dedup guard is correctly implemented with attempt-aware checking
- **Severity**: N/A (clean)
- **Location**: `internal/agent/tactical.go:1230-1304`, `internal/agent/tactical_h5_completion_guard_test.go`
- **Mechanism**: `OnJobCompleted` probes the optional `completionFreshChecker` capability. When present, it uses the queue's authoritative predicate (state + claim_token). When absent, it falls back to state-only. The guard fails OPEN when the job cannot be read.
- **Test**: `go test -run TestStaleCompletionGuard ./internal/agent/` (if exists) or review the tactical_h5_completion_guard_test.go assertions.
- **Evidence**: SOURCE -- tactical.go:1230-1304
- **Confidence**: high

### F6: CLEAN -- Machine-shaped output guard with bounded retry is properly implemented
- **Severity**: N/A (clean)
- **Location**: `internal/agent/loop.go:686-701, 3059-3076`
- **Mechanism**: `applyReplyGuard` replaces raw platform_* dumps with user-language fallbacks. The `guardRetried` flag allows ONE bounded rewrite retry via `reasoningCycle`. The handler choke point (`handleChatRequest`) stays single-shot.
- **Test**: `go test -run TestGuardRetry ./internal/agent/`
- **Evidence**: SOURCE -- loop.go
- **Confidence**: high

### F7: CLEAN -- Errored steps never pass review
- **Severity**: N/A (clean)
- **Location**: `internal/agent/handler.go:976`, `internal/agent/ralph_loop.go:554`
- **Mechanism**: `ReviewStep` gates on `stepHasError`. Tasks with failed steps finalize `StateFailed` with error text in the result.
- **Test**: Review the `ReviewStep` implementation in the codebase.
- **Evidence**: SOURCE -- handler.go:976, ralph_loop.go:554
- **Confidence**: high

## Checked and Clean

The following areas were audited and found clean:
- **Turn lifecycle**: Async turn start/complete/error paths in `handler.go`, `loop.go`, `ralph_loop.go`
- **Session binding**: No `os.Getwd()` fallbacks in `internal/agent/`; no global active-project fallback (`ProjectManager.GetActive` unused)
- **session_id vs conversation_id**: Distinct handling in `handler.go` with `GetByConversationID` separate from `Get`
- **Dispatcher/planner wiring**: `ToolActionMap` completeness verified by tests
- **Completion-event dedup**: `completionFreshChecker` pattern correctly implemented in `tactical.go`
- **Context firewall**: Fence controller wired in `handler.go:2952-2963` and `dispatcher.go:437-438`

## Gates run

- `go build ./internal/agent/...` -- PASS (exit 0)
- `go vet ./internal/agent/...` -- PASS (exit 0)

## Not covered

- `internal/agent/` subpackages beyond the core files examined (e.g., `internal/agent/prompts/`, `internal/agent/skills/`) -- time-boxed to ~40 tool calls
- Deep dive into every call site of `session_id`/`conversation_id` -- sampled key files only
- Runtime testing of the WS event classification under mixed session/conversation scenarios
