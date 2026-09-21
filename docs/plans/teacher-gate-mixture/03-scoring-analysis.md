# Leaf 03 — Scoring + Analysis: summary.json, REPORT.md, gate verdict

DISPATCH INSTRUCTION: Any agent may implement this leaf. Do NOT commit.
Do NOT run `git add`. Write files, run verification commands, and report
results. The orchestrator handles all git operations.

**Parent:** `docs/plans/teacher-gate-mixture/master.md`
**Scope:** Implement `score_mix.py`; produce `summary.json` and
`REPORT.md`; state the PASS/FAIL verdict against the 86.8% floor.
**Dependencies:** leaf 02 COMPLETE (full raw/ set).
**Estimated context:** ~35K.

## Reference numbers (frozen)

- Chain-only floor: 86.8% = 41.66/48. The mixture PASSES only if
  `correct >= 42` AND the report argues the margin exceeds single-case
  noise honestly.
- Prior cascade replay best: 83.97% (iter-20, wave 2 + conf-guard 0.15).
- Accuracy denominator: ALL 48 (non-OOD). `final.source == "error"` counts
  as WRONG. Never exclude errors from the denominator.
- Gate decision rule (from master.md): PASS = >= 42/48 (87.5%+) which
  clears the floor by more than one case. 41 or fewer = FAIL. Borderline
  exactly 41 = FAIL (floor not beaten by a whole case).

## Tasks

### Task 1 — score_mix.py

`python3.12 tools/classifier-eval/teacher_mix/score_mix.py --replay <path>
--rawdir <dir> --outdir <dir>`:

1. Load corpus (same string-aware JSON5 reader as run_mix.py — import it
   from run_mix or duplicate the 15-line function; no new deps).
2. Load raw/*.json. Join on case_id. Per case: correct =
   `final.intent == mapped(expected_intent)` and
   `final.source != "error"`.
3. Compute: n, n_nonerror, correct, accuracy (= correct / n_nonerror? NO —
   accuracy = correct / n where n is ALL non-OOD cases; report both n and
   n_nonerror separately), per_intent table {intent: {n, correct}},
   source distribution {a, judge, fallback, error}, list of error case
   ids, list of wrong case ids with (predicted, expected) pairs.
4. Write `summary.json` per master contract C3.
5. Print a compact table to stdout.

### Task 2 — REPORT.md

`tools/classifier-eval/results/teacher-mix/REPORT.md`:

1. Verdict line (first line): `VERDICT: PASS — X/48 (YY.Y%) vs floor
   86.8%` or `VERDICT: FAIL — X/48 (YY.Y%) vs floor 86.8%`.
2. Per-intent table with misses (intent, n, correct, wrong case ids).
3. Comparison table: teacher-mix vs chain-only 86.8% vs iter-20 cascade
   83.97%.
4. Error disclosure: every error/fallback case id and its resolution.
5. Confusion notes: for each wrong case, ONE line naming predicted vs
   expected intent and the worker disagreement status (did A/B agree on
   the wrong lane, or did the judge err?). This distinguishes worker-
   correlated errors (agreement on wrong = bad sign for mixture) from
   judge errors.
6. Honest caveats section: n=48 so one case = 2.1%; the sweep is zero-shot
   single-question; the teacher distributions for distillation would need
   a separate corpus-wide run with per-lane probabilities (this run
   records argmax only).
7. NO verbatim message text anywhere in the report — case ids only.

### Task 3 — Reproducibility check

- Re-run `score_mix.py` and diff summary.json against itself (deterministic
  output, byte-identical).
- Spot-verify 5 raw files by hand against their case rows; confirm the
  correct-count derives from the files, not hardcoded.

## Interface Contract (what this leaf exposes)

- `tools/classifier-eval/teacher_mix/score_mix.py` (CLI above).
- `summary.json` + `REPORT.md` per contracts C3/C4. REPORT.md's first line
  is the verdict — the orchestrator and the user read that line as THE
  result.

## Self-Verification Checklist

- [ ] summary.json byte-identical across two runs
- [ ] Denominator discipline: accuracy = correct / all-non-OOD-n
- [ ] Every wrong + error case id listed in REPORT.md
- [ ] py_compile clean; no new dependencies
- [ ] `grep -cE 'implement the plan|subagents, review' REPORT.md` = 0
      (no verbatim replay text leaked)

## Review Checklist (orchestrator)

- [ ] Recompute correct count from raw/ independently (orchestrator's own
      one-liner) and match the report's number
- [ ] Verdict rule applied exactly (>= 42 passes; 41 fails)
- [ ] Caveats section present and honest
- [ ] No verbatim text, no secrets

Suggested commit (orchestrator, after review):
`git add tools/classifier-eval/teacher_mix/score_mix.py
tools/classifier-eval/results/teacher-mix/summary.json
tools/classifier-eval/results/teacher-mix/REPORT.md &&
git commit -m "feat(classifier-eval): teacher-mix scoring + gate verdict
(leaf 03)"`
