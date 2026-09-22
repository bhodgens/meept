# CORRECTION — m4-silver/cascade-validation.json was rewritten in place

Added 2026-09-12 (audit wave). The campaign's append-only rule
(`results/alt-methods-correction.md`: "The original commit and its
message are left untouched — history is preserved; this file is the
correction of record"; skill rule 8: never edit a committed results
JSON) was violated by commit `7add6ce0`, which rewrote this result file
in place. This file is the correction of record; the JSON is left as
committed.

## What happened

`git show 7add6ce0 -- tools/classifier-eval/results/m4-silver/cascade-validation.json`
replaced the `stageA` / `stageB` / `stageC_chain` / `expected_system_accuracy`
/ `tau` / `misses` values of an existing, previously verified artifact
instead of writing a new one or appending a correction. The same file is
cited by `5dd477cd` as the source of the tau `0.147 -> 0.152` correction,
i.e. the artifact was retro-fitted to the corrected number, so the
pre-edit record of what was believed at the time was destroyed in place.

## Recovered pre-edit value

The destroyed pre-edit content is recoverable from git history: blob
`44c44255d57df49856aea9e1c55368a502d6de0d` (`7add6ce0^`). It is restored
verbatim alongside the current file as
`results/m4-silver/cascade-validation.pre-edit-recovered.json` so the two
records can be compared without touching either JSON's canonical path.

| field | pre-edit (44c44255) | current HEAD (7add6ce0+) |
|---|---|---|
| stageA routes / correct / precision | 11 / 8 / 0.727 | 9 / 7 / 0.778 |
| stageB routes / correct / precision | 4 / 4 / 1.0 | 5 / 4 / 0.8 |
| stageC_chain | 33 | 34 |
| expected_system_accuracy | 0.8468 | 0.844 |
| tau | 0.147 | 0.152 |
| first miss | ("A", "[replay case 2c0434047ecad05e]", "code", "plan") | ("B", "[replay case effcf40fbc1160e3]", "git", "code") |

The pre-edit numbers match the prose in
`results/m4-silver/report.md` (A 11/8 = 72.7%, B 4/4 = 100%, chain 33,
expected system accuracy 84.7%): report.md is the surviving document of
the pre-edit state.

## Correction of record

- Pre-edit (first silver validation): stage A 11 routes / 8 correct
  (72.7%), stage B 4/4 (100%), chain 33, expected system accuracy
  **0.8468**, tau 0.147.
- Post-edit (after the 389-case corpus run): stage A 9/7 (77.8%),
  stage B 5/4 (80.0%), chain 34, expected system accuracy **0.844**,
  tau 0.152.
- Both are below the 86.8% chain-only floor. The direction of the change
  is small; the point of this correction is the record discipline, not a
  revised verdict.

## Follow-up (not fixed here)

The harness writes straight into the tracked results directory, so any
re-run silently overwrites the record (`m4_gold_acceptance.py` now
writes write-once per-policy artifacts and never clobbers a committed
JSON; other producers still need the same treatment). A pre-commit guard
that rejects modifications to an existing `results/*.json` would enforce
the rule mechanically.
