# TUI Parity - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** TUI shows eval runs, isolation, last notify, memory facts. Lowercase UI.
- **Dependencies:** 03-eval-http.md, 11-speak-router.md, 12-memory-facts.md
- **Estimated Context:** 55K
- **Concurrency Group:** F

## Goal

Wave 2. TUI parity with HTTP. Feature parity with Flutter (leaf 17), not limitation parity.

## Context

TUI: `internal/tui/`. Slash commands in command_handler.go. Agents panel already shows goals. Add read-only panes/commands. Survey existing `/agents` and status bar before adding a second status widget.

Key files: `internal/tui/command_handler.go`, `internal/tui/agents_panel.go`, `internal/tui/rpc.go`.

## Interface Contracts (From Parent)

C9. Consume GET `/api/v1/eval/runs` via existing RPC/HTTP client the TUI already uses (prefer RPC `eval.list` if leaf 03 registered it).

UI strings lowercase: `eval`, `pass`, `fail`, `isolated`, `notify`.

### What This Leaf Exposes

`/eval` list+show. Isolation badge on pair/handoff UI if that view exists; else a line on agents panel. Last employee notify on agents panel. `/facts` list active MemoryFacts (read-only).

### What This Leaf Consumes

C1 JSON, C3 notify events, C6 facts.

## Tasks

### Task 1: /eval command

**Files:** `internal/tui/command_eval.go` + handler register. Empty list: `no eval runs` not a fake dashboard.

### Task 2: Notify + isolation lines

**Files:** agents panel. Missing data shows honest empty, not zero-success.

### Task 3: /facts

**Files:** command + RPC. Multiuser-off lists daemon-owner facts.

## Self-Verification Checklist

- [ ] All new UI lowercase
- [ ] No duplicate status widgets
- [ ] Do NOT commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] Keyboard pattern matches existing slash commands
- [ ] Does not call os.Getwd
- [ ] RPC methods exist (leaf 03) before the command is shipped
