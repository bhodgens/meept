# Flutter Parity - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** Flutter GUI eval list, isolation badge, notify, facts. Web-safe. Lowercase UI.
- **Dependencies:** 03-eval-http.md, 11-speak-router.md, 12-memory-facts.md
- **Estimated Context:** 60K
- **Concurrency Group:** F

## Goal

TUI/GUI feature parity. Data pipeline first: if a pane is blank, the HTTP shape is wrong, not layout.

## Context

`ui/flutter_ui/`. Agents tab already shows employee goals. Reuse that pane. No dart:io in shared code. kIsWeb guards.

Key files: `ui/flutter_ui/lib/` agents tab, API client. Survey `listAgents` vs `/api/v1/agents/{id}/goals` mismatch (IDs often differ — empty pane is honest).

## Interface Contracts (From Parent)

C9 HTTP JSON from leaf 03. Do not parse GoalHealth as only string; accept num and string if you touch goals. New eval models match C1 snake_case.

UI lowercase.

### What This Leaf Exposes

Eval runs page or a section on agents tab. Isolation badge. Notify line. Facts list read-only.

### What This Leaf Consumes

GET `/api/v1/eval/runs`, facts endpoint if 12 added HTTP; if not, add a thin GET `/api/v1/memory/facts` here ONLY if leaf 12 omitted HTTP — prefer adding it in this leaf's API client against a handler you also add under `internal/comm/http/` if missing. If that handler is more than 40 lines, stop and report; do not expand this leaf past Flutter.

## Tasks

### Task 1: API models + fetch test

**Files:** Dart API client + widget test with fake HTTP.

### Task 2: Agents tab section

**Files:** agents tab. Blank = `no eval runs` / `no facts`.

### Task 3: Web compile

No dart:io. `flutter analyze` on touched files.

## Self-Verification Checklist

- [ ] Widget test for empty and one-run states
- [ ] Lowercase strings
- [ ] Do NOT commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] Did not “fix” layout when data was empty
- [ ] Orange-family accents unchanged
- [ ] Parity with TUI commands (eval, facts, notify)
