# Bench Client Async Migration - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Switch meept-bench's Chat client from blocking `chat` RPC to `chat.submit` + `turn.terminal` await, recording ack/turn latencies separately.
- **Dependencies:** 01-async-rpc-mode.md (chat.submit + registry)
- **Estimated Context:** ~45K
- **Concurrency Group:** B
- **Repo:** ~/git/meept-bench (SEPARATE repository — work there, not in meept)

## Goal

The bench's client currently works around meept's 120s proxy cap with a
bus-event fallback (daemonclient.go:250-330) and still ends up recording the
"Task ... is still running" stub as a final reply whenever the daemon's 110s
sync wait fires — the exact bug that masked the #46 routing fix. This leaf
replaces the workaround with the intended contract: submit → ack → await
`turn.terminal`. A >110s task is no longer a failure; the harness waits as
long as the task's configured timeout allows and grades the REAL reply.

## Context

meept-bench is a standalone Go module at /Users/caimlas/git/meept-bench.
The chat path: internal/daemonclient/daemonclient.go `Chat(ctx, message,
sessionID)` — subscribes `chat_message` on the bus (fallback), calls RPC
"chat", and on proxy-timeout polls bus events for the reply
(proxyWindow=125s, graceWindow=10min, lines 354-355). The runner
(internal/runner/runner.go) calls Chat with chatCtx = task timeout
(runner.go:252), grades resp.Reply via checkers, and writes results.Row
(internal/results/results.go:10).

Key files:
- internal/daemonclient/daemonclient.go — Chat() (replace), Subscribe() (reuse)
- internal/runner/runner.go — the Chat call site + Row assembly (~252-330)
- internal/results/results.go — Row struct
- internal/suite/suite.go — Task.Timeout() (300s default in regression.json)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/daemonclient/daemonclient.go:
//
// ChatAck is the chat.submit acknowledgement.
// type ChatAck struct {
//     TurnID         string `json:"turn_id"`
//     ConversationID string `json:"conversation_id"`
//     SessionID      string `json:"session_id"`
//     Accepted       bool   `json:"accepted"`
//     Note           string `json:"note"`
// }
//
// TurnResult is the awaited terminal outcome.
// type TurnResult struct {
//     Reply      string // final reply text (never a stub under the new path)
//     Status     string // completed|failed|timeout|parked
//     Error      string
//     DurationMS int64
// }
//
// // ChatAsync submits and awaits. ctx bounds the WHOLE turn (task timeout).
// func (c *Client) ChatAsync(ctx context.Context, message, sessionID string) (*TurnResult, error)
//
// Chat() REMAINS (deprecated, unused by the runner after this leaf) for the
// fallback window until leaf 07 deletes it — keep it compiling, mark
// // Deprecated: use ChatAsync.
```

### What This Leaf Consumes

```
// meept daemon (Plan 1 + leaf 01):
//   RPC "chat.submit" → ack JSON (master Contract 1)
//   bus topic "turn.terminal" → payload with conversation_id, turn_id,
//   status, reply, error, duration_ms (Plan 1 Contract 1, frozen)
// Existing: Client.Subscribe (bus), Client.Call (RPC)
```

## Tasks

### Task 1: ChatAsync

**Objective:** submit → ack → await turn.terminal (filtered by turn_id) → TurnResult.

**Files:**
- Modify: `internal/daemonclient/daemonclient.go`
- Test: `internal/daemonclient/daemonclient_async_test.go` (create; mirror
  the existing daemonclient test wiring — httptest or the fake bus used by
  existing Chat tests)

**Step 1: Failing tests**

- Happy path: fake RPC returns ack; fake bus delivers a turn.terminal event
  (payload JSON: conversation_id, turn_id "t-1", status "completed", reply
  "done", duration_ms 42) → ChatAsync returns TurnResult{Reply:"done",
  Status:"completed"}.
- Ack latency recorded: the method must expose ack time — return it via a
  second return or an out-param struct; simplest: ChatAsync fills fields on
  a *ChatMetrics out-param (`type ChatMetrics struct{ AckSeconds,
  TurnSeconds float64 }`) passed by the caller; test asserts AckSeconds>0.
- turn_id filter: an unrelated turn.terminal event (different turn_id)
  arriving first is IGNORED; the matching one is returned.
- status=failed → TurnResult{Status:"failed", Error non-empty}, err=nil
  (a failed turn is a valid outcome for the runner to grade).
- ctx deadline while waiting → error wrapping ctx.Err().
- liveness: no event for LivenessTimeout → error ErrTurnStalled (sentinel,
  errors.Is-able). LivenessTimeout is a Client field, default 120s, settable
  (test sets 50ms).
- accepted=false ack → error containing the note.

**Step 2:** fail. **Step 3:** implement: Call("chat.submit") → ack;
Subscribe(["turn.terminal"]) BEFORE the call (no gap); loop Poll with a
short tick; filter turn_id; track last-event time for liveness; return on
match. Reuse the existing Subscribe/Poll plumbing. **Step 4:** pass;
existing daemonclient tests still pass (Chat kept intact).

### Task 2: Runner consumes ChatAsync + Row extension

**Objective:** runner.go calls ChatAsync, records ack/turn seconds, grades
TurnResult.Reply.

**Files:**
- Modify: `internal/runner/runner.go` (the Chat call site ~252-330)
- Modify: `internal/results/results.go` (Row gains AckSeconds, TurnSeconds —
  master Contract 6)
- Test: extend the runner test file (find the existing runner test pattern)

**Step 1: Failing test** — a runner-level test with a fake client returning
a scripted ack+terminal: Row contains ack_seconds>0, turn_seconds>0,
wall_seconds >= turn+ack; verdict from checkers on the terminal reply.
Also: ErrTurnStalled → Row verdict "error", error_kind "stalled" (new kind;
grep for error_kind vocabulary and add alongside "timeout"/"fail").

**Steps 2-4:** standard cycle. The proxy-timeout fallback loop in Chat is
NOT ported — submit never proxy-times out.

### Task 3: suite/docs honesty

**Objective:** suite README/comment updates so gate operators know the
semantics changed (final reply is now genuinely final; wall = submit→terminal).

**Files:**
- Modify: `suites/regression.json` — no structural change required; if a
  suite-level README or the suite "description" field mentions the old
  proxy-timeout behavior, update it.
- Modify: any README in the repo root describing the harness.

**Verify:** `go build ./... && go test ./...` in meept-bench; also update
results.jsonl consumers? — diff/scorecard read Row via struct, additive
fields are backward-compatible with old jsonl (omitempty).

## Self-Verification Checklist

- [ ] ChatAsync: submit→ack→await implemented; ack-before-subscribe gap impossible
- [ ] turn_id filtering proven by test (unrelated event ignored)
- [ ] failed turns are valid outcomes (no error path conflation)
- [ ] ErrTurnStalled sentinel + configurable liveness timeout
- [ ] Row ack_seconds/turn_seconds populated; diff/scorecard still build
- [ ] Chat() kept compiling with // Deprecated marker
- [ ] `go build ./... && go test ./...` green in meept-bench
- [ ] gofmt clean; no read_file corruption; no debug prints

**DO NOT COMMIT.** Orchestrator commits after review (explicit paths, both
repos touched: this one only).

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] All tasks + tests present and passing
- [ ] Contract 6 (Row fields) and Contract 4 (loop shape) satisfied
- [ ] No reliance on the 120s proxy workaround anywhere in the new path
- [ ] Existing Chat() and its tests untouched (deprecated, not deleted)
- [ ] Runner maps stalled → error_kind "stalled" without breaking diff
- [ ] Conventions: meept-bench mirrors meept Go style; stdlib only

Output: APPROVED or specific gaps with file+line.

## Notes

- The old fallback loop's graceWindow (10min) no longer applies: ctx (task
  timeout, 300s) bounds everything; the liveness timeout (120s default) is
  the stall detector INSIDE that window.
- Do NOT change suite timeout_seconds values in this leaf — after the
  migration a 300s budget is genuinely reachable; whether to raise is the
  orchestrator's post-integration call.
- results.jsonl backward compat: old rows lack the new fields (omitempty);
  the diff gate's best() collapses fine.
