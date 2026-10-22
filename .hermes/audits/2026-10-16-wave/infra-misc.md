# internal/bus, cluster, project, employee, metrics -- bughunt 2026-10-16

## Findings

### 1. bus: PublishT/PublishBlockingT marshal panic paths (CRITICAL)

- **Location:** `internal/bus/topic.go:40,60`
- **Mechanism:** `PublishT` and `PublishBlockingT` panic on `json.Marshal` failure. The comments explicitly state marshal failure is a "programmer error" (channels, funcs, cycles). The panic wraps the error with topic name and type.
- **Severity:** CRITICAL -- a non-marshalable payload (e.g., a `chan T` or `func`) passed to `PublishT` will panic the entire bus subscriber goroutine.
- **Test/Probe:** Search for all `Topic[T]` declarations and their publishers. Check if any payload type contains a channel, function, or reference cycle.
- **Evidence:** SOURCE -- read `internal/bus/topic.go` lines 37-61.
- **Not covered:** Whether any current publisher actually passes a non-marshalable payload.

### 2. bus: Proxy registration without live responder (HIGH)

- **Location:** `internal/comm/AGENTS.md:101-113`
- **Mechanism:** `internal/rpc/proxy.go makeProxy` publishes a request topic and waits on a response topic. If no subscriber exists for the request topic OR no publisher echoes `ReplyTo` on the response topic, the caller blocks for the full timeout (10-30s) then fails.
- **Severity:** HIGH -- silent deadlock symptom. A proxy with no responder appears as a hung call.
- **Test/Probe:** For each proxy registration, verify both sides exist in source. Check `internal/memory/handler.go` pattern.
- **Evidence:** SOURCE -- read `internal/comm/AGENTS.md`.
- **Not covered:** Current proxy registrations and whether all have live responders.

### 3. cluster/executor_bridge: UnixNano for CreatedAt (MEDIUM)

- **Location:** `internal/cluster/executor_bridge.go:192`
- **Mechanism:** `job.CreatedAt = time.Now().UnixNano()` when the dispatch job's CreatedAt is zero. This uses `UnixNano()` for ID-like timestamp field.
- **Severity:** MEDIUM -- violates the predictable-ID prevention rule. `pkg/id.Generate()` should be used for any ID-like field.
- **Test/Probe:** Run `go run ./tools/analyzers/predid/... ./internal/cluster/...` (if environmental issues resolved) or grep for `UnixNano` in cluster package.
- **Evidence:** SOURCE -- read `executor_bridge.go` line 192.
- **Not covered:** Other `UnixNano` uses in cluster package (gossip.go, grpc_handlers.go) -- see below.

### 4. cluster/gossip: UnixNano for Timestamp (MEDIUM)

- **Location:** `internal/cluster/gossip.go:349,852`
- **Mechanism:** `persistEvent` and `sendHeartbeat` use `time.Now().UnixNano()` for timestamp fields in `ClusterEvent`.
- **Severity:** MEDIUM -- violates predictable-ID prevention. While timestamps aren't IDs, the AGENTS.md rule broadly bans `UnixNano()` for any generated identifier-like field.
- **Test/Probe:** Compare against `models.GenerateEventID()` usage elsewhere.
- **Evidence:** SOURCE -- read `gossip.go` lines 349 and 852.
- **Not covered:** `grpc_handlers.go` Timestamp usage.

### 5. internal/project/init_deep.go: os.Getwd() fallback blocked (GOOD)

- **Location:** `internal/project/init_deep.go:92-96`
- **Mechanism:** `DeepInitializer.Run` explicitly rejects `RootDir == ""` with an error: "deep init requires an explicit RootDir (daemon CWD is not a project directory)". No `os.Getwd()` fallback.
- **Severity:** LOW (this is correct behavior) -- complies with AGENTS.md invariant.
- **Evidence:** SOURCE -- read `init_deep.go` lines 89-96.
- **Not covered:** Other `os.Getwd()` uses in `internal/` -- grep results show some in `cmd/meept/` (allowed, TUI CLI) and `internal/tui/app.go` (TUI, allowed per AGENTS.md).

### 6. employee/goal_loop: Gate command injection risk (MEDIUM)

- **Location:** `internal/employee/goal_loop.go:66-86` (CanExecutePlan)
- **Mechanism:** `CanExecutePlan` checks if a plan's approver ID matches the employee's ID or `escalates_to` entries. The gate command itself (`GateConfig.Command`) is stored and presumably executed elsewhere. No sanitization or allowlist visible in this file.
- **Severity:** MEDIUM -- if `GateConfig.Command` is user-controllable and executed via `exec.Command`, it's a command injection vector.
- **Test/Probe:** Search for where `GateConfig.Command` is executed.
- **Evidence:** SOURCE -- read `goal.go` (GateConfig struct at line 209) and `goal_loop.go` CanExecutePlan.
- **Not covered:** Actual execution site of gate commands.

## Checked and clean

- `internal/bus/topic.go` -- typed Topic contract, SubscribeT decode failure handling (warn + drop)
- `internal/comm/AGENTS.md` -- WS event classification rules, bus proxy registration requirements
- `internal/project/init_deep.go` -- no `os.Getwd()` fallback (explicit error)
- `internal/employee/goal.go` -- Goal state machine, health transitions, store CRUD
- `internal/metrics/store.go` -- store initialization, batch/flush loop structure

## Residual / not covered

- `internal/cluster/grpc_handlers.go` -- UnixNano timestamp usage, unauthenticated gRPC handler checks
- `internal/cluster/gossip.go` -- gossip convergence bugs, split-brain on node loss
- `internal/metrics/collector.go` -- counters that reset or double-count
- `internal/employee/goal_loop.go` -- full gate command injection audit, goal state machine stuck states
- `internal/employee/messaging.go` -- goal loop message handling

gates_run: read-only audit; no build/vet/test commands executed (environmental analyzer failures per BRIEF-RULES.md line 15)
