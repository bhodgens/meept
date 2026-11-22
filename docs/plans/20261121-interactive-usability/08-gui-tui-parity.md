# Leaf 08 — GUI/TUI parity: verbosity filter, mid-turn errors, truncation, keys

## DISPATCH INSTRUCTION

Implement this leaf fully. Do NOT commit. Do NOT run git add. Write code,
run tests, report results only. Working dir: the plan worktree. Grep
first (verify-or-apply).

## Meta
Parent: master.md (20261121-interactive-usability).
Scope: GUI verbosity filter actually filters; GUI shows mid-turn errors;
TUI stops truncating background-task answers; TUI error styling; misc
parity fixes. Dart-heavy leaf.
Dependencies: leaf 07 (payload boundary — client renders stay naive).
Estimated context: 85K.

## Goal

08 — GUI/TUI parity: verbosity filter, mid-turn errors, truncation, keys.

## Contract

Consumes C-5 (clean payloads). No daemon changes except where noted.

## Tasks

1. GATE (RED first): Flutter widget tests (ui/flutter_ui/test/):
   (a) verbosity quiet drops tier>0 progress frames; (b) mid-turn
   task.error event renders a transient error line in chat;
   (c) progress indicator wraps long messages (no hard 60-char cut).
   Go-side gate for TUI truncation: extend an existing tui test —
   background task.completed with a long ResultSummary renders
   unbounded (or with a high bound), not 50 chars.
2. GUI verbosity filter: chat_provider.dart:554-585 progress
   subscription — drop frames with progress.tier > verbosity level
   (read verbosityProvider; mirror internal/tui/app.go:1893
   tier <= verbosity). TUI parity: confirm gate already matches.
3. GUI mid-turn errors + review feedback: in the progress subscription,
   detect task.error and review_completed payload shapes (the
   payload-shape filtering pattern already used for quota and
   turn.terminal) and append transient system lines; mirror TUI
   app.go:1645-1719 semantics.
4. TUI truncation: models/chat.go:1361-1387 and 1535-1545 — render
   ResultSummary/result unbounded (daemon caps at ~12k) in the
   background-task boxes; error text bound raised likewise.
5. TUI error styling: add an error state/style distinct from system
   lines (models/chat.go:1347, 1525; single systemStyle at :217);
   route submit errors, failed turnTerminal, task.error through it.
6. Progress indicator wrap: agent_progress_indicator.dart:65-67 — wrap
   instead of substring; optional expand-on-tap.
7. Plans tab fixes (TUI): add reject key + reason prompt
   (models/plans.go:259-278 vs GUI plans_tab.dart:506-510); fix the
   tab-key hint to render the configured keys.ViewPlans binding
   (models/chat.go:3264 vs config.go:177); debounced fetchPlans on
   plan events (app.go:1956-1958).
8. Delete the dead TaskEventHandler package (internal/tui/handlers/
   task_events.go — zero non-test callers) if still dead at
   implementation time; drop its test with rationale in the report.
9. Tests GREEN: flutter test (new widget tests); go test -p 2
   ./internal/tui/...; dart format on changed files BEFORE staging
   (repo hook rule).

## Interface Contract Section

None (client-internal). Parity rule: every behavior here exists on BOTH
surfaces or carries a documented deviation.

## Self-Verification Checklist

- [ ] Widget gates RED -> GREEN.
- [ ] verbosity persists across restart still works (no regression of
      verbosity_provider persistence tests).
- [ ] dart format run on all changed .dart files.
- [ ] No new package dirs (feature-docs gate) — all edits in existing
      dirs.

## Review Checklist

- [ ] Parity table in the report: behavior -> TUI -> GUI.
- [ ] No dead imports; TaskEventHandler deletion verified by grep.

## Do NOT commit

The orchestrator stages and commits after review.
