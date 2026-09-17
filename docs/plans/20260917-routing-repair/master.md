# Routing repair - implementation orchestrator

Prepare the repair sequence for approval. Do not execute this tree yet.

## Meta

Role: root. Parent: none. Children: five leaves.
Status: PREPARED; execution approval pending.
Planning baseline: aa7cf48905df176094ccc1668c7d8a8cb1164732.
Review baseline: a384204fd5eac373b7d0a7a8a7ea383dc35df8e4.
Parent reproduction baseline: 759bdb9fcbdba4b3eacd7504047e0d3a92e81754.
The shared branch advances during planning. Reconcile source changes before execution.

## Goal

Make routing measurements trustworthy before selecting classifier changes. Prevent new private-text exports. Repair reproduced routing errors without changing model defaults.

The user authorizes preparation only. No source changes, commits, installations, model rebuilds, daemon restarts, or history rewrites belong to preparation.

## Architecture

Keep the existing Go dispatcher and Python evaluation tools. Repair each failure where the wrong decision or artifact originates. Use offline tests before isolated daemon tests. Separate observed model quality from deterministic code behavior.

An out-of-distribution case falls outside the supported intent categories. Abbreviate this category as OOD. Abstention means the classifier declines a direct route. Chain credit estimates the downstream model's accuracy; chain credit is not a measured answer.

## Interface Contracts

### C1: evidence and privacy

The existing `eval_harness.case_key(text)` provides the stable identifier for exported replay cases. Public results contain case keys, labels, stage names, and numeric scores, never private input excerpts. Producers own redaction before serialization and stdout. Private comparison tools report only paths, line numbers, and counts. Hashes are identifiers, not a guarantee of anonymity.

Historical numeric evidence remains unchanged. Append corrections with old artifact identifiers and precise invalidation scope. Removing private historical content needs separate approval. Never claim untracking removes history.

### C2: scoring counts

Leaf 02 owns the scoring definition. Preserve existing field names and keep in-domain denominators explicit:

- `total`: non-OOD test cases; `direct`: non-OOD predictions with a label; `correct`: correct non-OOD direct predictions.
- `wrong`: incorrect non-OOD direct predictions plus OOD direct predictions. OOD cases never enter training.
- `OOD_total`: OOD test cases; `OOD_abstain`: OOD predictions with no label; `OOD_R`: their ratio.
- `E2E = (correct + 0.868 * (total - direct)) / total` for positive total. Label E2E as modeled in-domain accuracy.
- `SCORE` retains the existing wrong-route penalty. Document its denominator and reject unsupported metric claims when total is zero.

Every held-out case reaches `head.decide`. Gold labels control scoring, not predictions. Freeze any necessary zero-denominator representation before implementation; prefer null for undefined ratios over a fabricated 100% success. Check all formatting consumers before changing representation.

### C3: corpus and model provenance

Leaf 03 owns provenance integration. Default builder input equals the union of eligible base and adversarial case keys from the evaluation loader. Preserve explicit single-corpus use as a labeled subset experiment.

Current measured counts: base 139, adversarial 250, combined 389; eligible 361; OOD 28. Counts are review evidence, not constants to hardcode. Test set equality on loaded cases.

Artifact metadata must identify source files, source content hashes, eligible key-set hash, document count, embedding model identity, and preprocessing settings. Keep existing runtime-required fields compatible. Installed artifacts are not replaced during this repair without operator approval.

A cache namespace must include a verified model revision or content fingerprint plus preprocessing. A URL or model alias alone cannot identify weights. Legacy unverified caches must not silently support fresh acceptance claims.

### C4: routing behavior

Leaf 04 owns `internal/agent/dispatcher.go` changes. Preserve recall before imperative arbitration before the git agreement veto. Guard repairs let ordinary classification run; guard repairs must not hardcode individual reported sentences.

An arithmetic expression is different from a question carrying a path. A media-consumption request is different from a coding request containing a URL. A locative preposition is not temporal evidence. Normalize punctuation before testing polite lead-ins.

Retain agent overrides, planning flags, and configuration-disable controls. No new model, threshold sweep, automatic training, or enabled-by-default detector is authorized.

### C5: acceptance and claim states

Each finding has one state: REPRODUCED, REFUTED, NEEDS_REPRODUCTION, REPAIRED_OFFLINE, VERIFIED_LIVE, or BLOCKED. A helper test does not prove a full route. A test fixture does not prove the installed model's provenance.

Leaf 05 owns the final evidence report and documentation reconciliation. Claim completion only after parent verification. Keep live-model quality separate from code repairs.

## Child Index

| Leaf | Document | Dependencies | Estimated context | Effort | Group |
|---|---|---|---|---|---|
| 01 | [Privacy prevention](01-privacy.md) | execution approval | 45K | 2-4 hours | A |
| 02 | [Scoring integrity](02-scoring.md) | execution approval | 45K | 2-4 hours | A |
| 03 | [Corpus and cache provenance](03-provenance.md) | 02 reviewed | 65K | 3-6 hours | B |
| 04 | [Routing guards and fallback](04-routing.md) | 01 and 02 reviewed | 65K | 3-6 hours | B |
| 05 | [Evidence, docs, and acceptance](05-acceptance.md) | 01-04 reviewed | 55K | 2-4 hours offline | C |

Total engineering estimate: 12-24 hours, excluding history repair, model execution, and independent review cycles. Each leaf contains serial review checkpoints. Dispatch one checkpoint at a time if a full leaf cannot fit one worker session.

## Dispatch Protocol

1. Obtain explicit execution approval. Inspect current branch, tracked diffs, and overlapping sessions. Do not modify another session's changes.
2. Run group A with separate file ownership. Leaf 01 must not edit eval_harness.py or m4_gold_acceptance.py. Leaf 02 owns both.
3. Run group B after dependencies pass. Leaf 03 owns Python provenance changes; leaf 04 owns Go routing changes.
4. Review each checkpoint in the parent. Require failing tests before fixes and passing tests after fixes. Do not dispatch overlapping writers.
5. Run group C. Stop at privacy-history and live-operation approval gates. Commit only when the user separately authorizes commits.

Load hierarchical-plan-execution before execution. Read source with read_file after locating symbols with search_files. Apply changes with patch/write_file. Workers must not stage, commit, push, install packages, read credentials, start servers, or rewrite history.

## Review Checklist

- [ ] Every review finding has a disposition in leaf 05, including refuted and deferred claims.
- [ ] Privacy tests use synthetic sentinels; public test output contains no private text.
- [ ] Predicted OOD outcomes, corpus key parity, and model identity are verified independently.
- [ ] Routing tests exercise production entry points with positive controls and injected verdicts where required.
- [ ] Source diffs, commands, artifact hashes, unresolved gates, and actual serving models support the final report.

## Coding Conventions

- Follow AGENTS.md. Go uses the module's Go 1.26 toolchain; Python requires 3.12 or later.
- No added dependencies without approval. NumPy exists in the verified Python 3.12 environment; verify other packages before use.
- Preserve error handling, nil guards, request-scoped behavior, and session working-directory rules.
- Bound Go package parallelism with `-p 2`. Only the parent runs broad suites.
- Use synthetic data in committed tests. Store temporary probes and raw private evidence under an explicitly protected temporary directory.

## Completion Tracking Table

| Child | Status | Iterations | Review notes |
|---|---|---|---|
| 01 | PENDING | 0 | Preparation only; historical removal separately gated |
| 02 | PENDING | 0 | Forced OOD abstention independently reproduced |
| 03 | PENDING | 0 | Default population mismatch independently reproduced |
| 04 | PENDING | 0 | Two route errors reproduced; other routes conditional |
| 05 | PENDING | 0 | Integration and independent review pending |

## Integration Test Plan

Run these gates during implementation, not as evidence of completed repair now:

1. `/opt/homebrew/opt/python@3.12/bin/python3.12 tools/classifier-eval/m4_gold_acceptance.py --self-test` and `make classifier-eval-selftest`. Both must exercise the new scoring cases after leaf 02 wiring.
2. Run every new leaf-specific Python test command and `go test -p 2 ./internal/agent -run 'TestRoutingRepair|TestClassifierLanes|TestPrefilter' -count=1`.
3. Parent runs `go test -p 2 ./internal/agent`, `go vet ./internal/agent`, and `make test`. Record unrelated failures separately; never label a partial sweep complete.
4. Validate changed docs and generated surfaces. Run `make graphs-check`; regenerate only when relevant source is stable and ownership is clear.
5. After live-operation approval, use an isolated scratch home with rebuilt binaries and local-only providers. Verify model response shapes and session paths before requests. Await terminal events, not submission acknowledgements. Require the exact artifact or route evidence for every accepted runtime repair.

If a required tool is missing, report BLOCKED. Do not install or silently skip. Live measurements and history operations remain distinct gates.

## Findings coverage

| Finding class | Owner | Disposition |
|---|---|---|
| MEAS-01 copied private text; MEAS-09 incomplete ignore patterns | 01 | Prevention; history removal blocked on separate approval |
| MEAS-04 forced OOD abstention; MEAS-08 output formatting | 02 | Reproduce and repair |
| MEAS-02 builder population; MEAS-03 fixture provenance; MEAS-05 cache identity; MEAS-06 fold evidence | 03 | Repair proven defects; adjudicate fixture and fold-policy claims |
| AR-1 arithmetic; AR-2 media URL; AR-3 locative time; AR-4 punctuated lead-in; I11 fallback taxonomy | 04 | Reproduce full routes before fixes |
| MEAS-07 selection-on-test; I01 description ownership; I03b unavailable margin mode; I10b relay documentation; I13 stale graphs; D01-D04 routing docs | 05 | Append evidence corrections and reconcile current source |

Reviewer passes I02, I03a, I04-I10a, I14, D05, A01, A02, A04, and A05 are regression constraints, not new repair tasks. I12 is a design judgment, not a mechanically proven defect. A03 and A06 require broader session/plan audits and remain outside this repair. The missing-detector lead is REFUTED. No detector implementation is missing.

## Recorded Decisions

User decisions recorded on 2026-09-17:

1. Historical exposure: provide the candidate inventory only. No sanitization or history rewrite approved. Inventory: `/tmp/meept-private-exposure-inventory-20260917.md`; 36 matching lines across 11 files. Matches require adjudication. Remote-tracking references are local, possibly stale observations, not proof of remote absence.
2. Undefined ratios: emit JSON null and display n/a. Update all metric consumers consistently during leaf 02.
3. Cache identity: hash local model content. For directory-based models, identify all weight shards and relevant preprocessing files. Never substitute an unverified server alias for content identity.
4. Unused fixture: deletion approved after reader checks. Removed `internal/agent/testdata/prefilter_tfidf_veto.json`; tests create their own temporary model. The builder still names this path as its default output, so regeneration can recreate the artifact. Leaf 03 must reconcile the output convention.
5. Live acceptance: isolated scratch rig and local model turns approved. This does not authorize production restarts, package installation, cloud requests, commits, or history changes.

The user has not issued blanket execution approval for all repair leaves. Only the requested inventory and conditional fixture deletion execute in this turn.

## Open Questions

1. Historical privacy removal remains undecided. Review the candidate inventory before approving sanitization or history changes.
2. Undefined metrics are resolved: JSON null and display n/a. Leaf 02 must update all consumers consistently.
3. Model identity is resolved: local content hashing. Leaf 03 must identify the complete model and preprocessing file set.
4. Fixture removal is complete. Focused prefilter tests and `go build ./internal/agent` pass. The unchanged builder can recreate the file; output-location policy remains for leaf 03.
5. Scratch-daemon and local-model verification is approved. Leaf 05 remains dependent on completed repairs. A new model-quality campaign still needs a separate dataset and execution budget.

Recorded Decisions override earlier approval-request wording in the leaves. No blanket repair execution or commit authorization is inferred.
