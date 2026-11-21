# Specialist execution runs - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below. Make the 00T specialist test pass. Do not weaken it.
> Do NOT commit. Do NOT git add.

## Meta

- **Parent:** master.md
- **Scope:** Write one execution_runs row for each completed specialist chat turn.
- **Dependencies:** 00T-test-gate.md, contract C5
- **Estimated Context:** 45K
- **Concurrency Group:** A

## Goal

The agents tab history includes coder, planner, and other dispatcher-routed turns. Those turns stay on turn.terminal for the chat bubble.

## Tasks

1. In `internal/activity`, subscribe to `turn.terminal` from the execution projector wiring. On each event, append one row with tier `specialist`.

2. Map agent_id, status, output, tokens if present, stop_reason, and session_id from the turn.terminal payload. Do not invent values. Absent tokens stay 0 and are honest only when the event had no usage.

3. Do not publish this row on bot.result or employee.result. Do not add a kind field.

4. A nil store or a decode error logs and drops that event. It does not fail the chat turn.

5. Make `TestTurnTerminalAppendsSpecialistRun` pass.

File ownership: `internal/activity/execution_projector.go` and its test. Do not edit goal_loop.go or the chat handler reply path.

## Interface Contract

Exposes C5.

## Self-Verification Checklist

- [ ] Specialist row tier is the string specialist
- [ ] Chat reply path is unchanged
- [ ] Do NOT commit

## Review Checklist

- [ ] No second chat bubble was added
- [ ] Projector Stop still drains before store close
- [ ] Do NOT commit
