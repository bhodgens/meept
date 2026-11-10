# 00T — Enforcement Tests (test gate)

## DISPATCH INSTRUCTION

Write TESTS ONLY (§1-§6); no production files. Existing enforcement/
executor suites must stay GREEN with these added (no-caller golden).
**Do NOT commit** — no `git commit`, no `git add`.

- Parent: 02-enforcement/orchestrator.md
- Scope: internal/employee enforcement step-1c tests; internal/agent
  executor details-injection test.
- Dependencies: branch 01 (callerid + stamps exist and are committed).
- Estimated context: 45K

## Interface Contracts (FROZEN)

Per ../master.md §Interface Contracts: reason `caller_not_approved`; deny
text verbatim; audit type `voice.unapproved_action`; no-pause; details
keys caller_approved/caller_known presence-only; config list key.

## Tasks

1. §1 PreExecChecker table: (caller Approved:true) pass; (false + tool in
   allowed list) pass; (false + tool absent) deny with reason + pinned
   text; both ledger_append and the message tool pass under defaults.
2. §2 golden: run the EXISTING enforcement table tests with no caller in
   ctx/details -> identical results (add a wrapper case, not edits to
   existing rows).
3. §3 deny does NOT auto-pause (assert against the same state the Never
   step would have mutated; mirror existing pause assertions).
4. §4 audit seam: fake audit writer -> exactly one voice.unapproved_action
   per deny; none per allow; nil writer survives.
5. §5 executor details: caller present -> two keys; absent -> keys not
   added (map length assertion).
6. §6 deny propagation: reason+text reach the tool-result the model sees
   (existing deny-propagation test posture).

## What This Leaf Exposes to Siblings

Gates leaf 01-step1c.

## Self-Verification Checklist

- [ ] RED = missing 1c behavior/keys only (tests RUN, assertions fail —
      do not depend on new exported symbols beyond callerid).
- [ ] Existing suites unchanged GREEN.

## Review Checklist

- [ ] Control agent: §2 golden truly compares against pre-edit behavior.
