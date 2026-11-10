# 03 — client render swap (TUI + Flutter)

## DISPATCH INSTRUCTION

Implement in the worktree given in your dispatch context.
**Do NOT commit** — no `git commit`, no `git add`.

- Parent: docs/plans/20261110-contact-display/master.md
- Scope: internal/tui/models/voice.go (:264,275);
  ui/flutter_ui/lib/services/voice_service.dart (:32,50) +
  features/voice/voice_page.dart (:629).
- Dependencies: 02-surface-swaps (final JSON shapes).
- Estimated context: 30K

## Interface Contracts (FROZEN)

Per master §Interface Contracts: clients decode caller_name/
caller_display / target_name/target_display; render name (display form);
NO client-side resolution or number formatting logic.

## Tasks

1. TUI: table renders display string; missing-field fallback (old server)
   -> "unknown"; update the existing tui tests touching that row format.
2. Flutter: service model gains the fields (keep old field reads
   tolerant); page renders 'employee -> display'. Run dart analyze IF the
   toolchain exists in the sandbox — else state so in the report for
   orchestrator verification.
3. Check both clients for OTHER raw-target render sites (grep target/
   number) and fix or list them in the report.

## What This Leaf Exposes to Siblings

Nothing upstream; completes the disclosure chain.

## Self-Verification Checklist

- [ ] go test ./internal/tui/... GREEN; manual render sanity via testdata.

## Review Checklist

- [ ] No digit formatting added client-side.
