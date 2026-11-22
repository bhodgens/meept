# Leaf 05 — Degraded-classifier honesty signals

## DISPATCH INSTRUCTION

Implement this leaf fully. Do NOT commit. Do NOT run git add. Write code,
run tests, report results only. Working dir: the plan worktree. Grep
first (verify-or-apply).

## Meta
Parent: master.md (20261121-interactive-usability).
Scope: surface heuristic_fallback routing and degraded planning to the
user instead of silently downgrading.
Dependencies: leaf 01 (identity; this leaf only reads meta).
Estimated context: 60K.

## Goal

05 — Degraded-classifier honesty signals.

## Contract

Reads response.Meta provenance (handler.go:1538-1541). Renders
classification_method in the GUI message footer; appends a one-line
honesty note on degraded methods; appends a degraded-planning sentence
on the fallback-step path.

## Tasks

1. GATE (RED first): e2e suite scenarios (extend
   e2e/suites/chat-identity/ or new e2e/suites/degraded-signals/):
   (a) a turn routed via heuristic_fallback carries the honesty note in
   the stored reply; (b) degraded planner fallback (planner LLM fails
   repair) reply includes the degraded-planning sentence.
2. Note emission: in the terminal-reply composition
   (internal/agent/handler.go ~1538-1600), when Intent.Method is a
   degraded method (heuristic_fallback, fallback), append one line:
   "[routed by keyword fallback - classification degraded]" (exact
   wording lowercase; reuse the [unverified: ...] label posture).
3. Degraded-planning sentence: strategic.go:997-1000 fallback path —
   append to the turn reply: "[planning degraded: single direct step]".
   Also render the persisted plan_quality marker ("degraded:
   deterministic single-step") in formatTaskCompletedMessage
   (handler.go:2415-2462) when present.
4. GUI footer: render meta.classification_method in the message bubble
   footer (ui/flutter_ui/lib/features/chat/chat_message_bubble.dart)
   when present and non-llm.
5. Cooldown shape (bounded scope): make the 60s blanket cooldown
   (llm_classifier.go:587-799) attempt-aware ONLY insofar as the
   boot-window already parks (no behavior change if
   ClassifierBootReadiness covered it) — if the audit's
   mid-life-poisoning signature persists, shorten the first cooldown to
   15s with exponential regrowth. Pin with a unit test on the cooldown
   helper. Do NOT restructure the resolver.
6. Tests GREEN: e2e; `go test -p 2 ./internal/agent/...`; flutter
   analyze + widget test for the footer.

## Interface Contract Section

None exported (string labels + meta rendering only).

## Self-Verification Checklist

- [ ] Gate RED -> GREEN.
- [ ] Non-degraded turns byte-identical (no note appended when method
      is llm) — pin with a test.
- [ ] Cooldown change covered by unit test; boot-window behavior
      unchanged (existing tests green).

## Review Checklist

- [ ] Notes are one line, lowercase, bracketed (label posture).
- [ ] No PII/log noise added.

## Do NOT commit

The orchestrator stages and commits after review.
