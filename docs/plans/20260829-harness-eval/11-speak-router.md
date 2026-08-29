# Speak Router - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** Harness-routed speak (Q11=A). reply_to_user tool. GoalLoop detached -> notify. Isolated child silent to user.
- **Dependencies:** 05-verify-breaker.md, 10-isolation.md
- **Estimated Context:** 65K
- **Concurrency Group:** D

## Goal

The model always ends a turn the same way. The harness decides bubble vs notify vs parent report. Same AGENT.md works in chat and on a goal.

## Context

Interactive chat already emits assistant content as the bubble. Keep that for SpeakSession. GoalLoop has no watching chat. Push service: `internal/services/push`. WS: never classify notify as `chat_message`.

loop.go: one Deliver call after the turn produces final text. Isolated child: ClassifyRun(false child=true)=SpeakParent.

Key files: `internal/agent/loop.go` (serial after 05/06), `internal/employee/goal_loop.go`, `internal/tools/builtin/`, `internal/comm/http/server.go` WS map (do not emit chat_message).

## Interface Contracts (From Parent)

C3 SpeakRouter + ClassifyRun. C4 isolated child cannot speak to user.

`reply_to_user` parameters: `{text string}`. Builtin. Mid-turn. Same ClassifyRun. Isolated: tool error.

Detached + empty text: no notify (no-op). Detached + text: notify even without the tool. If both tool notify and final text, one notify.

### What This Leaf Exposes

`internal/agent/speak.go`, builtin `reply_to_user`, GoalLoop uses Deliver(SpeakNotify, ...).

### What This Leaf Consumes

C4 isolatedChild bit on the loop. Push/notify existing channel if present; else bus topic `employee.notify` with session_id + conversation_id keys.

## Tasks

### Task 1: ClassifyRun + Deliver tests

**Files:** Create `internal/agent/speak.go`, `speak_test.go`

Matrix: attached/not × isolated/not.

### Task 2: reply_to_user tool

**Files:** Create `internal/tools/builtin/reply_to_user.go` + test. Register in builtin registry (grep how siblings register).

### Task 3: Wire loop + GoalLoop

**Files:** Modify loop.go (one site) and goal_loop.go completion/reflect path. WS test or comment: topic is not chat_message.

### Task 4: Dedup notify

Test: tool notify then final text -> one Deliver.

## Self-Verification Checklist

- [ ] Chat bubble path unchanged for attached+not isolated
- [ ] Isolated child never notifies
- [ ] No os.Getwd
- [ ] Do NOT commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] WS type != chat_message for notify
- [ ] Production Register of reply_to_user
- [ ] GoalLoop nil-safe if push missing (log + skip, no panic)
