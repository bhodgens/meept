# Flutter GUI quota status surfaces - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** The Flutter GUI agent list mirrors the TUI: quota_wait and
  blocked states with countdown, primary/active model lines, live WS updates.
  Web-compatible (kIsWeb-safe) per AGENTS.md.
- **Dependencies:** 05 (states + events), 07 (WS classification),
  08 (countdown format parity)
- **Estimated Context:** 70K
- **Concurrency Group:** F (runs alongside 08; shares only the FORMAT
  contract — the "3h 12m" string — no code dependency)

## Goal

GUI parity with the TUI (AGENTS.md hard requirement): the same information,
the same lowercase labels, the same countdown format, on the Flutter
desktop and web clients.

## Context

Flutter app in `ui/flutter_ui/lib` (find the agents list screen and its
WS/event subscription — the client already renders agent_progress events
and agent states; quota states ride the same path). Platform rules: no
top-level dart:io in shared code; kIsWeb guards; platform_service for
abstractions. The web audit script
(`scripts/audit-dart-enum-name-shadow.py`) runs in lint-ci — avoid Dart
extensions shadowing Enum.name/index.

Key files to understand before implementing:

- `ui/flutter_ui/lib/` — the agents screen, agent state enum/model class,
  WS message handling, theme (tokens mirror `theme/tokens.json5`).
- The existing agent-status rendering (colors per state) to extend.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// Flutter agent list (and detail) rendering:
//   state "quota_wait": label "quota wait" (lowercase), warning tone,
//     countdown "quota resets in 3h 12m" — SAME FORMAT as TUI leaf 08's
//     util (mirror the format; if the TUI util lands first, copy its
//     exact output shape; the format is: <N>h <M>m, or <M>m <S>s under
//     an hour, or "resuming…" past due)
//   state "blocked": label "blocked", error tone, hint "action required"
//   detail view with fallback_model: lines
//     "primary: <model> (blocked until <time>)"
//     "active: <fallback model>"
//   Live updates: the agent_progress WS event for agent.quota_wait updates
//     the row; state back to running clears episode data.
// Web-safe: no dart:io outside kIsWeb guards; no new platform channels.
```

### What This Leaf Consumes

```
// leaf 05: agent states + agent.quota_wait payload (via agent_progress WS)
// leaf 07: classification guarantees type == agent_progress
// leaf 08: countdown FORMAT (string shape parity only)
```

## Tasks

### Task 1: State model + rendering

**Objective:** quota_wait/blocked render distinctly in the agents list.

**Files:**
- Modify: the agent model/enum file + agents list widget (exact paths from
  exploration)
- Test: `ui/flutter_ui/test/quota_status_test.dart`

**Step 1: Write failing test**

Widget or model-level tests (follow existing Flutter test style in
ui/flutter_ui/test/): an agent with quota_wait + unblock_at renders
"quota wait" and the countdown; blocked renders "blocked" + "action
required"; other states unchanged.

**Step 2: Run test to verify failure**

Run: `flutter test test/quota_status_test.dart` (workdir ui/flutter_ui)
Expected: FAIL

**Step 3: Write minimal implementation**

Extend the state enum/mapping + list tile rendering. Use existing theme
colors (warning/error tones).

**Step 4: Run test to verify pass**

Run: `flutter test test/quota_status_test.dart`
Expected: PASS

### Task 2: Countdown + detail lines

**Objective:** Live countdown, primary/active model block in detail.

**Files:**
- Modify: same widgets
- Test: same test file

**Step 1: Write failing test**

Countdown formatting matches the parity format for 3h12m, 45m, 90s, and
past-due ("resuming…"); detail shows primary/active lines only when
fallback_model is present. Countdown refreshes via the app's existing
ticker/timer pattern (find it; if the list has no ticker, a periodic
Timer while any episode is visible, disposed properly).

**Step 2: Run test to verify failure**

Run: `flutter test test/quota_status_test.dart`
Expected: FAIL

**Step 3: Write minimal implementation**

**Step 4: Run test to verify pass**

Run: `flutter test test/quota_status_test.dart`
Expected: PASS

### Task 3: WS event handling

**Objective:** agent.quota_wait events (arriving as agent_progress) update
the list live.

**Files:**
- Modify: the WS message handler / state notifier for agent updates
- Test: same test file

**Step 1: Write failing test**

Feed the handler an agent_progress payload carrying quota fields (mirror
the real payload shape from internal/comm/http tests): row transitions to
quota_wait; cleared transition resets to running and clears episode data;
malformed payloads are ignored without crash.

**Step 2: Run test to verify failure**

Run: `flutter test test/quota_status_test.dart`
Expected: FAIL

**Step 3: Write minimal implementation**

Extend the existing agent-state update path.

**Step 4: Run test to verify pass**

Run: `flutter test test/quota_status_test.dart`
Then the existing agent tests: `flutter test` (full suite, confirm no
regressions)
Then web-audit script: `python3 scripts/audit-dart-enum-name-shadow.py`

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep — only what the tasks specify
- [ ] All new UI text lowercase
- [ ] No top-level dart:io in shared code; kIsWeb-safe
- [ ] Countdown format byte-matches the TUI format

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (labels + countdown parity)
- [ ] Full `flutter test` suite green (no regressions)
- [ ] Web audit script clean
- [ ] No dart:io outside guards

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- If leaf 08 has not landed when you start, implement the countdown
  formatter from THIS spec's format string and the orchestrator verifies
  parity at integration (the format is fully specified above — no
  ambiguity).
- Keep widgets const-friendly where possible; this list re-renders on every
  countdown tick.
