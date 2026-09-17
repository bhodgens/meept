# Scoring integrity - implementation leaf

DISPATCH INSTRUCTION: Execute only after repair-plan approval. Use tests before code. Do NOT commit or stage files.

## Meta

Parent: [master.md](master.md).
Scope: measure real OOD decisions and preserve explicit score denominators.
Dependencies: execution approval. Concurrency group: A.
Estimated Context: 45K. Effort: 2-4 hours in serial checkpoints.
Audit references: MEAS-04 and MEAS-08.

## Goal

Pass OOD cases through the prediction function without training on OOD labels. Make self-tests detect forced abstention. Repair fractional-count formatting only after reproducing the separate error.

## Context

`tools/classifier-eval/eval_harness.py:744` correctly excludes OOD from training. Lines 758-761 incorrectly insert None without calling `head.decide`. Lines 784-790 count the inserted None as successful abstention.

Parent reproduction uses one in-domain case and one OOD case with an always-code predictor. Actual result: one prediction call, OOD_abstain=1, OOD_R=1.0, wrong=0. This proves the scoring defect without model execution.

`Makefile:810-812` runs `m4_gold_acceptance.py --self-test`. Add regression coverage to this existing entry point. `test_hardening.py` exercises unrelated shell guards; do not place scoring tests there merely because the filename contains test.

## Interface Contracts (From Parent)

Preserve the C2 count definitions in master.md. Every held-out case reaches prediction. OOD labels never enter training. Preserve modeled in-domain E2E separately from OOD rejection.

Keep the deterministic chain coefficient 0.868, but label its provenance as an old estimate. Do not introduce sampled outcomes. Freeze undefined-ratio representation after checking formatters and summary consumers. Do not silently change denominators.

Ownership: this leaf owns eval_harness.py, m4_gold_acceptance.py, and a serial checkpoint for iter16_cascade3.py. Leaf 03 waits before changing harness provenance. Leaf 01 does not edit these paths.

## Tasks

### Task 1: prove the scoring failure through the existing self-test

Files: `tools/classifier-eval/m4_gold_acceptance.py`, `tools/classifier-eval/eval_harness.py`.

1. Locate the self-test registration and its existing assertion convention.
2. Add an in-memory embedder and an always-code predictor to exercise real `run_permutation`.
3. Assert two held-out prediction calls, zero OOD training labels, OOD_abstain=0, and wrong=1 for a two-case fixture.
4. Run `/opt/homebrew/opt/python@3.12/bin/python3.12 tools/classifier-eval/m4_gold_acceptance.py --self-test`.
5. Confirm failure comes from forced abstention, not missing dependencies or fixture setup.

The fixture must populate train folds sufficiently for any real head used. A constant predictor is valid for proving scorer behavior. Restore patched module globals after the test.

### Task 2: repair prediction flow and counter consistency

1. Remove the gold-label prediction bypass while retaining OOD training exclusion and self-exclusion.
2. Add always-abstain, wrong-in-domain, mixed-domain, no-OOD, and OOD-only cases.
3. Verify the in-domain E2E formula stays unchanged for identical in-domain predictions.
4. Check direct/wrong relationships, precision, F1, SCORE, confusion lists, and empty denominators. Do not let OOD errors silently corrupt in-domain-only ratios.
5. Run the self-test and `make classifier-eval-selftest`. Missing NumPy or a different Python interpreter is a blocker to resolve without installation.

Required hand calculation: one correct in-domain route and one wrong OOD route gives total=1, direct=1, correct=1, wrong=1. OOD_R=0. Modeled in-domain E2E remains 1; the wrong-route penalty must still fire. The report must explain why these measures differ.

### Task 3: repair expected-count output formatting

Files: `tools/classifier-eval/iter16_cascade3.py` and the existing self-test file.

1. Confirm the executed print expression formats a fractional expected count with `:2d`.
2. Add an offline test which exercises the real formatting function. Extract an import-safe formatter if required.
3. Confirm the current expression fails with ValueError for a fractional wrong count.
4. Format expected counts explicitly as decimal estimates, not integer observations.
5. Rerun both classifier self-test commands. Do not rerun historical training or overwrite committed results.

## Self-Verification Checklist

- [ ] The pre-fix test fails because OOD prediction is skipped.
- [ ] Every held-out case receives a prediction after repair.
- [ ] OOD labels remain absent from training.
- [ ] Count formulas and undefined ratios have explicit tests.
- [ ] Both self-test entry points pass; historical artifacts remain unchanged. Do NOT commit.

## Review Checklist (For Review Agent)

- [ ] Parent independently runs constant-prediction and abstention controls.
- [ ] No copied scoring function or sampled chain credit exists in tests.
- [ ] Formatting cannot label expected counts as observed counts.
- [ ] Leaf 05 receives the exact historical invalidation scope.
- [ ] Report APPROVED or specific gaps with file and line references.
