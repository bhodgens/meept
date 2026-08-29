# Event Rewake - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** World-push events inject at iteration boundary via existing rewake. No mid-turn kill. Event-triggered register is a thin wrapper on scheduler/hooks.
- **Dependencies:** 11-speak-router.md
- **Estimated Context:** 50K
- **Concurrency Group:** E

## Goal

Mail, timer, and user cut-in wake the agent without aborting a running tool. Detached runs notify via leaf 11.

## Context

`internal/agent/loop_rewake.go` already drains at iteration start. Scheduler and hooks exist. Do not invent a second wake channel. hook.async_rewake was a documented-but-unwired contract historically — verify with grep that armRewakeConsumer is called from the PRIMARY loop (daemon AgentLoop), not only registry loops.

Key files: `loop_rewake.go`, `internal/scheduler`, `internal/agent/hooks.go`.

## Interface Contracts (From Parent)

Rewake payload gains an optional `Source string` (`timer|hook|user|notify_reply`). Speak routing for a detached wake uses SpeakNotify.

Do not close-then-nil the wake channel (channel-nilafterclose hook). Use existing rewakeStopOnce.

### What This Leaf Exposes

A scheduler job completion can publish the existing rewake topic with Source=timer. Test: loop injects a system note at next iteration.

### What This Leaf Consumes

C3 Deliver for detached. Existing rewake bus topic — grep before adding a new topic.

## Tasks

### Task 1: Source field on RewakePayload

**Files:** Modify `loop_rewake.go` + tests. Unknown source still injects.

### Task 2: Scheduler -> rewake publish

**Files:** One publish at job complete in scheduler or a small adapter. If a publisher already exists, write a test and stop.

### Task 3: Prove primary loop is armed

**Files:** Test or grep-backed daemon wiring. If unarmed, call armRewakeConsumer from the same place other loop Start hooks run. Do not put the consumer on AgentRegistry only.

## Self-Verification Checklist

- [ ] No mid-turn cancel
- [ ] Primary loop owns the consumer
- [ ] Do NOT commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] No new bus topic without subscriber
- [ ] close-then-nil absent
- [ ] session_id/conversation_id on payload
