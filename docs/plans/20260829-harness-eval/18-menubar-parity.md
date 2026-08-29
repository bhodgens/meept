# Menubar Parity - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** macOS menubar shows last employee notify and eval fail count. Instantiate the view (do not register a dead panel).
- **Dependencies:** 03-eval-http.md, 11-speak-router.md
- **Estimated Context:** 45K
- **Concurrency Group:** F

## Goal

Detached GoalLoop notify is visible without opening TUI. A registered RPC with no view is not done.

## Context

`menubar/` Swift. Prior bug: ReasoningConfigView existed but was never in a TabView. Grep instantiation. HTTP via existing APIClient.

Lowercase UI.

Key files: menubar Settings/TabView, APIClient.swift, any GoalRow from employee goals work.

## Interface Contracts (From Parent)

C9. Poll or WS for notify. WS type is not chat_message.

### What This Leaf Exposes

A visible row: last notify text + time. Eval fail count on the menu extra. Both clickable to open the GUI or a small list.

### What This Leaf Consumes

GET eval runs. Notify bus/HTTP.

## Tasks

### Task 1: API methods

**Files:** APIClient.swift + decode C1.

### Task 2: View + instantiate

**Files:** New view AND add it to the existing TabView/menu extra. Grep the parent instantiates it.

### Task 3: Empty states

`no notifies` / `no eval runs`. Lowercase.

## Self-Verification Checklist

- [ ] View is instantiated (grep call site)
- [ ] Lowercase
- [ ] Do NOT commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] Not RPC-only
- [ ] Does not steal focus unless user clicks
- [ ] chat_message not used for notify
