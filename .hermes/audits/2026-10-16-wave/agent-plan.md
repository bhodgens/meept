# internal/agent, internal/plan, internal/context, internal/task -- bughunt 2026-10-16

## Findings

1. **Predictable ID Generation (CRITICAL)**
   - **Location**: `internal/agent/backoff.go:449`, `internal/agent/tactical.go:60` (comment references old pattern)
   - **Mechanism**: `backoff.go` uses `rand.New(rand.NewSource(time.Now().UnixNano()))` for jitter in backoff delays. This uses `time.Now().UnixNano()` as a seed, which is a predictable ID generation pattern violates the invariant from `AGENTS.md` and the `predid` analyzer rule.
   - **Severity**: CRITICAL
   - **Test/Probe**: Run `go run ./tools/analyzers/predid/... ./internal/agent/...` -- should flag `backoff.go:449`.
   - **Evidence**: SOURCE -- read `backoff.go:449`: `rng:     rand.New(rand.NewSource(time.Now().UnixNano()))`
   - **Not Covered**: `internal/agent/loop.go` and other files for additional UnixNano usages.

2. **Mutex Held Across I/O in SessionTracker (HIGH)**
   - **Location**: `internal/agent/session_tracker.go:234-275`
   - **Mechanism**: `PersistIdleSessions` holds `t.mu` lock while calling `persistSession`, which performs I/O to `memvidClient.StoreWithZone`. This violates the invariant "never hold a mutex across I/O" from `docs/agents/coding-practices.md`.
   - **Severity**: HIGH
   - **Test/Probe**: Run `go run ./tools/analyzers/mutexio/... ./internal/agent/...` -- should flag this.
   - **Evidence**: SOURCE -- read `session_tracker.go:242-256`: lock acquired at line 242, I/O at line 299, lock released at line 272.
   - **Not Covered**: Other potential mutex-I/O crossings in `dispatcher.go`, `team_orchestrator.go`.

3. **Bare Untyped Numeric Constants (MEDIUM)**
   - **Location**: `internal/agent/classification_caps.go:12,16`, `internal/agent/dispatcher.go:47`
   - **Mechanism**: `classification_caps.go` defines `const classificationHardCeiling = 2048` and `const classificationFloor = 1024`. `dispatcher.go` defines `const reRouteWindow = 3`. These are bare untyped numeric constants that could benefit from explicit typing for clarity and to prevent unintended type conversions.
   - **Severity**: MEDIUM
   - **Test/Probe**: Manual review confirms these are untyped ints.
   - **Evidence**: SOURCE -- read the files directly.
   - **Not Covered**: Other bare consts in the package.

4. **Nil-Deref Risk in `handleTaskCompleted` (MEDIUM)**
   - **Location**: `internal/agent/handler.go:1715-1739`
   - **Mechanism**: `handleTaskCompleted` parses a payload into a struct. If `payload.Steps` is nil/empty, `fetchStepSummaries` is called. However, `h.taskStore` could be nil, and `fetchStepSummaries` does not nil-check `h.taskStore` before use.
   - **Severity**: MEDIUM
   - **Test/Probe**: Call `handleTaskCompleted` with a task where `h.taskStore == nil`.
   - **Evidence**: SOURCE -- read `handler.go:1736-1738`: `steps = h.fetchStepSummaries(payload.TaskID)` without nil check on `h.taskStore`.
   - **Not Covered**: Other nil-deref paths in `handler.go`.

## Checked and Clean

- `internal/agent/AGENTS.md` invariants: Async turns, per-session working dirs, no global active-project fallback, machine-shaped-output guard -- no violations found in the audit scope.
- `internal/agent/handler.go`: Chat handler logic follows AGENTS.md contracts (turn.terminal events, session ID handling).
- `internal/agent/escalation.go`: Escalation manager uses proper mutex locking for `escalations` map.
- `internal/agent/team_orchestrator.go`: Team session state access is protected by `mu` RWMutex.
- `internal/agent/dispatcher.go`: Dispatcher classification and routing logic appears consistent with invariants.

## Residual / Not Covered

- Full concurrency audit of all shared maps across `internal/agent/` package.
- Detailed error handling review of all `json.Unmarshal` paths.
- Verification of `pkg/id.Generate` usage vs. `time.Now().UnixNano()` in all files.
- Review of `internal/plan/`, `internal/context/`, `internal/task/` packages (limited tool calls used on agent package).

gates_run: read_file (BRIEF-RULES.md, AGENTS.md, handler.go, dispatcher.go, escalation.go, session_tracker.go, team_orchestrator.go, backoff.go, tactical.go, classification_caps.go); grep searches for UnixNano patterns; execute_code for static analysis of known leads.
