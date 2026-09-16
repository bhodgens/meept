# TUI Client Async Migration - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** The TUI chat view submits turns via chat.submit and renders live progress + the real terminal result from turn.terminal, with a stalled-turn indicator.
- **Dependencies:** 01-async-rpc-mode.md
- **Estimated Context:** ~55K
- **Concurrency Group:** B

## Goal

The TUI (bubbletea) sends chat turns via the blocking `chat` RPC
(internal/tui/rpc.go:290,328) and its UI thread hangs for the whole turn.
This leaf moves the TUI to submit-ack + event await: the input box stays
responsive, progress events render per session, the terminal event renders
the result bubble, and a stalled turn (liveness timeout) shows an honest
"no progress for Ns — task may still be running" state instead of a frozen
screen.

## Context

TUI stack: bubbletea + lipgloss + bubblezone (project conventions: all UI
text lowercase, bubblezone for positioning, clickable elements). Chat
sending: internal/tui/rpc.go Call("chat") at :290 and :328 (two paths —
identify both). Event consumption: internal/tui/events.go EventStream
(bus.subscribe + poll loop) already delivers task.completed/failed
(internal/tui/app.go:1495, handlers/task_events.go). The chat model lives
in internal/tui/models/chat.go (ChatTaskResultMsg at :591 already signals
"async task completed or failed" — there is prior art for async results).

Key files:
- internal/tui/rpc.go:280-340 — the two chat Call sites
- internal/tui/events.go — EventStream subscribe/poll
- internal/tui/models/chat.go — chat model, ChatTaskResultMsg, update loop
- internal/tui/app.go:1480-1540 — event dispatch switch
- internal/tui/handlers/task_events.go — task event handlers (pattern)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/tui/rpc.go:
//   type ChatSubmitResult struct {
//       TurnID, ConversationID, SessionID string
//   }
//   func (c *Client) SubmitChat(message, sessionID string, parts []ContentPart) (*ChatSubmitResult, error)
//      // calls "chat.submit"; returns IMMEDIATELY
//
// internal/tui/models/chat.go (new tea.Msg / tea.Cmd):
//   type turnSubmittedMsg struct{ TurnID, ConversationID string }
//   type turnProgressMsg struct{ ConversationID, Text string }   // from agent_progress events
//   type turnTerminalMsg struct{ TurnID, Reply, Status, Error string; DurationMS int64 }
//   func awaitTurnCmd(rpc *Client, turnID, conversationID string, liveness time.Duration) tea.Cmd
//      // polls the EventStream/RPC bus for turn.terminal filtered by TurnID;
//      // emits turnProgressMsg on agent_progress events for the conversation;
//      // emits turnTerminalMsg on match; emits turnTerminalMsg{Status:"stalled"}
//      // after liveness silence (never blocks the UI thread — tea.Cmd goroutine)
//
// Rendering contract: while awaiting, the chat view shows a live
// "turn <short-id> · <elapsed>s" line fed by turnProgressMsg texts
// (latest progress text appended to the transcript area as a dimmed line —
// lowercase per UI convention). On turnTerminalMsg: replace the progress
// line with the reply bubble (status completed) or an error bubble
// (failed/stalled — stalled text: "no progress for Ns — task may still
// complete; it will appear here when it finishes").
```

### What This Leaf Consumes

```
// Daemon: "chat.submit", "turn.terminal", "agent.progress"-classified
// events (the TUI EventStream already subscribes to task./step. topics —
// extend its topics list with turn.terminal and the progress topic it
// needs, mirroring events.go).
// Plan 1: TurnTerminalEvent payload keys (frozen).
```

## Tasks

### Task 1: SubmitChat client method + event plumbing

**Objective:** RPC wrapper + EventStream topics + typed msgs.

**Files:**
- Modify: `internal/tui/rpc.go` (add SubmitChat near the existing chat Call)
- Modify: `internal/tui/events.go` (topics list += "turn.terminal" and the
  progress topic already used for agent_progress — verify what the list has)
- Modify: `internal/tui/models/chat.go` (msgs + awaitTurnCmd)
- Test: `internal/tui/chat_async_test.go` (create; use the package's
  existing fake-client pattern — find it in rpc_test.go)

**Step 1: Failing tests**

- SubmitChat builds correct params (message/session_id) and parses the ack.
- awaitTurnCmd: fake event source delivering an unrelated terminal event,
  a progress event, then the matching terminal → the tea.Cmd's channel
  yields turnProgressMsg then turnTerminalMsg, unrelated ignored.
- Liveness: no events → stalled turnTerminalMsg after the (short, injected)
  timeout. Clock/liveness must be injectable — no real sleeps in tests.

**Steps 2-4:** standard cycle.

### Task 2: Chat view wiring

**Objective:** Enter on the input box → SubmitChat → progress line →
terminal bubble.

**Files:**
- Modify: `internal/tui/models/chat.go` (Update: submit action; View:
  progress line; the transcript insert on terminal)
- Test: extend `internal/tui/models/chat_test.go` (bubbletea model tests —
  follow existing patterns)

**Step 1: Failing tests**

- Submit action produces the tea.Cmd; on turnSubmittedMsg the view shows
  the pending line; on turnProgressMsg the dimmed progress line updates
  (latest wins); on turnTerminalMsg completed the reply bubble renders and
  the pending line clears; on failed/stalled an error-styled bubble renders
  with the honest stalled text; lowercase text everywhere.
- Rapid double-submit: two pending lines keyed by turn id, each resolved
  independently by its own terminal event (concurrency correctness).

**Steps 2-4:** standard cycle. Reuse existing bubble styling; bubblezone
markers on the pending line if the view already zone-marks transcript rows.

### Task 3: Remove the blocking send on the migrated path

**Objective:** The chat view's send no longer calls the blocking Call("chat").

**Files:**
- Modify: `internal/tui/rpc.go` — mark the old chat-call funcs
  `// Deprecated: SubmitChat + awaitTurnCmd` (keep for 07's removal sweep;
  other callers, if any, keep working)

**Step 1: Failing test** — grep-level test is not a test: instead assert by
behavior — the chat view's Update returns within the test's tick with a
turnSubmittedMsg (previously it would have blocked until the fake replied).
The existing fake-client tests that drove the old path must be updated to
the new msgs — deleting the old-path tests is EXPECTED here; note it in
Deviations.

**Steps 2-4:** standard cycle.

## Self-Verification Checklist

- [ ] All tasks implemented; `go build ./...`; gofmt/vet clean
- [ ] UI never blocks on the RPC thread (awaitTurnCmd is a tea.Cmd goroutine)
- [ ] Multiple concurrent turns tracked independently (double-submit test)
- [ ] Stalled state: honest text, lowercase, actionable wording
- [ ] turn_id filtering proven; unrelated events ignored
- [ ] Liveness injectable — zero real sleeps in tests; -race clean
- [ ] Old blocking path deprecated (not deleted); its stale tests updated/removed with a note
- [ ] Bubblezone/lowercase UI conventions respected

**DO NOT COMMIT.** Orchestrator commits after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Tasks + tests present/passing (-race)
- [ ] Contract 4 loop shape in TUI idiom (Cmd goroutine, not a blocking Update)
- [ ] EventStream topics extended; poll loop unaffected for other views
- [ ] No blank-bubble risk: turn.terminal renders via the chat model, NOT via
      the WS chat_message path (TUI is a bus subscriber, not a WS client —
      confirm no double render with task_events.go handlers; dedupe if the
      same completion arrives as both task.completed and turn.terminal)
- [ ] No scope creep: no GUI, no daemon, no CLI changes

Output: APPROVED or specific gaps with file+line.

## Notes

- DOUBLE-RENDER hazard (the one real trap): after Plan 1, a completed task
  turn produces BOTH a task.completed relay (chat message via sendResponse)
  and a turn.terminal event. The TUI must render ONE result bubble. Decide
  by turn_id: turn.terminal for a submitted-and-tracked turn suppresses the
  task.completed chat message for the same task (track task_id from the
  ack... the ack carries no task_id — tasks are created later. Simplest
  correct rule: once a turn is submitted via SubmitChat, the chat view
  ignores task.completed chat messages for that conversation until the
  turnTerminalMsg arrives; document this in code).
- Keep the sidebar's task.completed handling (sidebar.go:590) — that is the
  task LIST view, unrelated to chat bubbles.
- Liveness default 120s; make it a TUI config field (internal/tui/config.go
  ChatConfig) named `liveness_timeout_seconds`, default 120, 0=disabled.
