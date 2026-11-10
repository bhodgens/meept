# 02 — docs correction + shipped-behavior proof

## DISPATCH INSTRUCTION

Implement in the worktree given in your dispatch context.
**Do NOT commit** — no `git commit`, no `git add`.

- Parent: 02-enforcement/orchestrator.md
- Scope: docs/workflows/voice-sidecar.md inbound-gating section rewrite;
  one daemon integration test encoding the full behavior matrix.
- Dependencies: 01-step1c.
- Estimated context: 35K

## Interface Contracts (FROZEN)

Per ../master.md §Interface Contracts; decisions O1-O6 in master
Owner-Ratified Decisions govern the doc text.

## Tasks

1. voice-sidecar.md: replace the "inbound and outbound both gated" claim
   with the shipped chain: webhook signature -> answer-all -> resolve
   caller (contacts voice_approved) -> per-turn PreExec step 1c gate ->
   voice.allowed_numbers stays OUTBOUND-only; note the O4 residual risk.
2. Daemon integration test `caller_matrix_test.go`: real PreExecChecker +
   real callerid ctx + fake contact store; cases: approved caller +
   employee constitution allowlist denial (O5 proof constitution routing
   works on voice-stamped loops); unknown caller -> message tool pass,
   shell_execute deny text byte-checked, no pause, one audit row;
   unknown caller + ledger_append with SetStatus -> passes TODAY, comment
   cites O4/Q1 owner decision pending.
3. If e2e voice-lifecycle is cheap to extend, add the unknown-number call
   case there; else note in report.

## What This Leaf Exposes to Siblings

Docs-code parity; the matrix test is the tree's acceptance artifact.

## Self-Verification Checklist

- [ ] Matrix test GREEN; doc diff reviewed line-by-line against code.

## Review Checklist

- [ ] No aspirational claims left in docs.
- [ ] O4 flag visible in both doc and test.
