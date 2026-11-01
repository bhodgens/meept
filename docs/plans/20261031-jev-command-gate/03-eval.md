# Eval Harness: Teacher Gate + Replay Ruler - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD (selftest target). Do NOT commit - the
> orchestrator handles all git operations after review. Do NOT use read_file
> on existing source files - explore with search_files or terminal cat. After
> writing a file, do NOT read it back - write once and stop. Report what you
> built, files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** Python eval harness extending tools/classifier-eval patterns:
  teacher-gate (teacher must beat a rule-based floor before any student run),
  replay-ruler reporting over captured gate records, and the threshold freeze
  into the contract file + training manifest hash.
- **Dependencies:** 02-capture-export.md (C4 records, C5 DPO export, C6
  contract file); R0-research.md (methodology, section 3)
- **Estimated Context:** 60K
- **Concurrency Group:** D
- **Audit references:** proposal 12 (close the gap)

## Goal

The training loop lives OUTSIDE the repo. This leaf ships the in-repo eval
surface: `tools/jev-command-gate-eval/` with a replay harness that scores
captured CommandGateRecords against (a) a rule-based floor derived from
`shell.GetRiskLevel` and (b) the teacher model, gates student distillation on
the teacher beating that floor on held-out replay, freezes the score threshold
from the teacher sweep into the contract file (never re-tuned on the student),
and hashes the contract file into the training manifest.

## Context

- tools/classifier-eval/ - existing eval harness patterns: eval_harness.py,
  alt_methods_sweep.py, harvest scripts, selftest targets
  (`make classifier-eval-selftest`, `make e2e-sweep-selftest`). Extend the
  PATTERN (structure, selftest wiring, evidence files); do not modify it.
- internal/tools/builtin/shell.go:963 `GetRiskLevel` - the rule-based floor:
  map categorical RiskLevel -> the 0.0-1.0 rubric bands from C6 (mapping
  frozen in the harness config and documented).
- config/jev-command-gate.contract.json5 (C6, from 02) - the threshold field
  marked `// frozen-by: 03-eval` gets its final value here.
- R0-research.md findings doc - cite it in the harness README/method notes;
  the teacher-gate requirement and frozen-threshold rule trace to it.
- Python harness must stay offline-safe: fixtures for selftest; live scoring
  only with explicit flags pointing at a running endpoint.

## Verified Anchors

- tools/classifier-eval/eval_harness.py - harness entry pattern
- tools/classifier-eval/alt_methods_sweep.py - sweep/report pattern
- internal/tools/builtin/shell.go:963 - GetRiskLevel (floor source)
- internal/shadow/exporter.go:208 - exportDPO (corpus source format, C5)
- config/jev-command-gate.contract.json5 - C6 threshold field (created by 02)
- Makefile targets `classifier-eval-selftest`, `e2e-sweep-selftest` - selftest wiring pattern

## Interface Contracts (From Parent)

### What This Leaf Exposes

- [C7 harness + report schema] - full definition in master.md:
  `{"floor": {...}, "teacher": {...}, "student": {...}|null,
    "teacher_gate": "PASS"|"FAIL", "frozen_threshold": float}`
- `make jev-gate-eval-selftest` target (offline, fixture-driven, CI-eligible)
- Contract-file threshold freeze + sha256 into training manifest
  (`tools/jev-command-gate-eval/manifest.json`)

### What This Leaf Consumes

- [C4 CommandGateRecord] ListRecords output (replay corpus)
- [C5 ExportDPO rows] (training-pair view of the same corpus)
- [C6 contract file] (threshold freeze target + hash input)
- internal/tools/builtin/shell.go GetRiskLevel semantics (floor mapping)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Harness skeleton + replay ruler: load CommandGateRecords (or fixture corpus), replay-score each against the rule-based floor (GetRiskLevel -> rubric band mapping) and a scorer backend (teacher via explicit endpoint flag; offline fixture backend for selftest), emit precision/recall/abstention metrics per scorer | Create: tools/jev-command-gate-eval/eval.py, tools/jev-command-gate-eval/replay.py, tools/jev-command-gate-eval/fixtures/corpus.jsonl | `python3 tools/jev-command-gate-eval/eval.py --selftest` | package absent | metrics emit for floor + teacher views over the corpus | Given a record corpus; When replay runs; Then per-scorer precision/recall vs the human-decision ground truth emit in the C7 report shape |
| T2 | Teacher gate: compute the rule-based floor metrics and the teacher metrics on a held-out fold; teacher_gate=PASS iff teacher beats the floor on the chosen primary metric (document the metric + tie rule in the harness README); when FAIL, student runs are refused (exit non-zero, loud message) | Create: tools/jev-command-gate-eval/gate.py, tools/jev-command-gate-eval/README.md; Modify: tools/jev-command-gate-eval/eval.py | `python3 tools/jev-command-gate-eval/eval.py --selftest` | gate absent | selftest includes a sub-floor fixture teacher -> FAIL (non-zero exit) and an above-floor fixture teacher -> PASS | Given a teacher scoring below the floor; When the gate runs; Then it returns FAIL and refuses student runs; given an above-floor teacher, PASS |
| T3 | Threshold freeze + manifest: sweep teacher scores over the corpus to pick the operating threshold per the R0 methodology (cite the findings doc), write it into config/jev-command-gate.contract.json5 replacing the provisional value, hash the contract file (sha256) into tools/jev-command-gate-eval/manifest.json; the student eval path READS the frozen threshold and never re-tunes it | Modify: config/jev-command-gate.contract.json5; Create: tools/jev-command-gate-eval/manifest.json, tools/jev-command-gate-eval/freeze.py | `python3 tools/jev-command-gate-eval/freeze.py --check` (verifies hash + frozen flag) | provisional marker present | threshold frozen; manifest carries matching sha256; student path has no tuning flag | Given teacher sweep results; When freeze runs; Then the contract threshold updates once, the hash lands in the manifest, and any attempt to re-tune on the student side is structurally absent |
| T4 | Makefile target + selftest wiring: `make jev-gate-eval-selftest` running eval.py --selftest offline (fixtures only); wire into the existing selftest target family; verify the sibling classifier-eval selftests still pass | Modify: Makefile; Test: target itself | `make jev-gate-eval-selftest && make classifier-eval-selftest` | target absent | both targets GREEN; no interference | Given a fresh checkout; When the selftest targets run; Then jev-gate-eval passes offline and classifier-eval is unaffected |

## Self-Verification Checklist

- [ ] All tasks implemented; `make jev-gate-eval-selftest` GREEN
- [ ] C7 report schema emitted exactly (floor/teacher/student/teacher_gate/frozen_threshold)
- [ ] Teacher gate FAILs a sub-floor teacher and blocks student runs
- [ ] Threshold frozen from the teacher sweep only; student path has no
      re-tuning surface
- [ ] Contract file hash in the training manifest matches the frozen file
- [ ] Harness is offline-safe (fixtures); live scoring requires explicit flags
- [ ] ASCII only; python stdlib style; no new third-party deps beyond
      classifier-eval's existing set
- [ ] No deviations (or documented in the report)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** `make jev-gate-eval-selftest` (last 20 lines)
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; selftest target passing
- [ ] Report matches C7 schema; teacher-gate logic gates student runs
- [ ] Threshold freeze lands in C6 + manifest hash; no student re-tuning path
- [ ] Floor mapping documented and deterministic
- [ ] R0 findings cited in the README/method notes
- [ ] classifier-eval untouched and still passing; no scope creep; no debug
      artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

- Fold assignment: follow tools/classifier-eval/fold-assignment.json patterns
  for the held-out fold so the gate is not graded on its own training slice.
- The harness never imports Go code: the floor mapping is reimplemented in
  python and pinned by a fixture test asserting agreement with recorded
  GetRiskLevel outputs for the fixture commands.
