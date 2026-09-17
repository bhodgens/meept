# Privacy prevention - implementation leaf

DISPATCH INSTRUCTION: Execute only after approval of the repair plan. Do NOT commit. Do not stage, push, remove historical artifacts, or rewrite history.

## Meta

Parent: [master.md](master.md).
Scope: prevent private replay text from entering new public results.
Dependencies: execution approval. Concurrency group: A.
Estimated Context: 45K. Effort: 2-4 hours in serial checkpoints.
Audit references: MEAS-01 and MEAS-09.

## Goal

Replace private excerpts with stable case identifiers before output. Add synthetic tests and a publication check. Keep historical exposure handling separate.

## Context

Meept's Python evaluation scripts write results under an explicitly tracked results directory. Ignoring private source files cannot protect copied text.

Known producers include `tools/classifier-eval/iter20_cascade_v2.py`, `iter20_cascade_v2b.py`, `iter19_quickplan_cascade.py`, and `validate_silver.py`. The validation writer truncates private inputs before placing them in `misses`. Truncation does not provide privacy.

The parent comparison confirms private-prefix matches in four committed JSON artifacts. A wider comparison finds 35 matching lines across 10 files. Generic phrase matches still need adjudication. Do not print matched input text.

## Interface Contracts (From Parent)

Use existing `eval_harness.case_key(text)` for public identifiers. Preserve stage, expected label, predicted label, and numeric fields. Label identifiers explicitly; do not leave a field named text carrying a hash.

Public outputs must contain no raw private inputs, including stdout and exceptions. Private diagnostics require an explicitly protected output directory. Tests use synthetic unique sentinels.

This leaf does not own eval_harness.py, m4_gold_acceptance.py, committed results, or workflow documentation. Leaf 02 owns harness code. Leaf 05 owns evidence corrections.

## Tasks

### Task 1: establish producer and consumer ownership

Files to inspect: the four producer scripts, `.gitignore`, and `tools/classifier-eval/harvest_outcomes.py`.

1. Locate every private-input slice placed in a result structure.
2. Locate readers of the result structure before changing field semantics.
3. Record producer-to-output paths without input text.
4. Identify stdout, exception, Markdown, and JSON outputs using the same data.
5. Return any additional writer paths to the parent before expanding ownership.

Run no historical training script merely to import its functions. Several scripts perform model work at module import.

### Task 2: replace copied excerpts, one producer checkpoint at a time

Proposed new test: `tools/classifier-eval/test_result_privacy.py` using unittest.
Each checkpoint changes at most two producer scripts plus the test file.

1. Write a failing test with a unique synthetic private sentinel in the production result-construction path.
2. Run `/opt/homebrew/opt/python@3.12/bin/python3.12 -m unittest discover -s tools/classifier-eval -p test_result_privacy.py -v`.
3. Refactor output construction into import-safe functions only where necessary. Preserve execution behavior behind a main entry point.
4. Replace excerpts with case keys before serialization and printing. Exercise the actual writer with models replaced by in-memory fixtures.
5. Rerun the test. Assert sentinel absence, identifier presence, and unchanged stage/count fields.

Do not test a copied implementation of output logic. Do not import a script which starts model loading at module scope. If import-safe refactoring exceeds the checkpoint budget, split the producer checkpoint before implementation.

### Task 3: protect renamed local data

Files: `.gitignore`, `tools/classifier-eval/test_result_privacy.py`.

1. Write negative tests for renamed local files under classifier-eval and its results directory.
2. Confirm current `git check-ignore --no-index` behavior fails the expected protection.
3. Add a scoped `*.local.json5` rule after relevant re-inclusion rules. Avoid hiding legitimate tracked fixtures.
4. Rerun ignore tests against temporary candidate paths without creating private files.
5. Verify known committed result files remain visible to Git.

### Task 4: hand off historical exposure safely

Write a private temporary inventory containing paths, line numbers, commit identifiers, and exposure status. Do not copy private text. Mark remote exposure UNKNOWN unless checked under approval.

Propose two separate actions: current-tree sanitization and historical removal. Explain which copies each action cannot remove. Both require approval because historical evidence policy and privacy containment conflict.

## Self-Verification Checklist

- [ ] Tests reproduce copied-sentinel leakage before repair and pass after repair.
- [ ] All known producer outputs use identifiers, including stdout.
- [ ] Renamed local files are ignored without hiding normal fixtures.
- [ ] Historical files and Git history remain unchanged without separate approval.
- [ ] Report exact paths, commands, and producer coverage. Do NOT commit.

## Review Checklist (For Review Agent)

- [ ] Parent verifies a real output writer, not only a helper.
- [ ] No text-bearing debug output or exceptions escape the test path.
- [ ] Consumers understand identifier fields; aggregate results stay unchanged.
- [ ] Extra producer discoveries have explicit ownership and coverage.
- [ ] Report APPROVED or specific gaps. Do not equate prevention with history removal.
