# Evidence, documentation, and acceptance - verification leaf

DISPATCH INSTRUCTION: Execute only after leaves 01-04 pass review. Do NOT commit. Do not operate live daemons or rewrite private history without separate approval.

## Meta

Parent: [master.md](master.md).
Scope: reconcile claims and verify repaired components together.
Dependencies: leaves 01-04 reviewed. Concurrency group: C.
Estimated Context: 55K. Effort: 2-4 hours offline; live evaluation separately budgeted.
Audit references: MEAS-07, I01, I03b, I10b, I13, D01-D04, and all repair acceptance gates.

## Goal

Publish a precise correction of record and prove which repairs work. Separate offline correctness, live routing, and measured model quality. Do not declare a new accuracy winner from old chain credit.

## Context

Source claims live in `docs/plans/classifier-iteration/FINAL-REPORT.md`, `tools/classifier-eval/results/alt-methods-correction.md`, `tools/classifier-eval/results/wave5-confusion.md`, and `docs/workflows/intent-routing.md`.

The historical 87.35% result uses two correct direct routes and 46 assumed chain calls from a 48-case replay. This is not fresh chain measurement. The margin-optimum analysis uses training data and remains unvalidated. The earlier review brief overstated the optimum as a proven dead end.

The repo and generated graphs change under parallel sessions. Reconcile current source before changing documentation. The WebSocket relay code already has active parallel work; do not repair stale claims from an old snapshot blindly.

## Interface Contracts (From Parent)

Use the finding states from master.md. Preserve historical numeric JSON artifacts. Append dated correction notes naming affected formulas, denominators, and artifact identities. Privacy removal follows a separate approved procedure, never an ordinary evidence edit.

Own workflow and report documentation, plus AGENTS.md amendments only when current invariants genuinely change. Do not overwrite parallel edits. Broad verification belongs to the parent; workers run narrow tests only.

## Tasks

### Task 1: verify and correct measurement claims

1. Trace ALT policy selection to its replay arrays. Confirm whether acceptance reuses policy-selection data.
2. Add an append-only selection-on-test caveat where the current claim lacks one. Explain selection-on-test as choosing and judging a policy on the same examples.
3. Identify historical outputs produced by the forced OOD path. Invalidate those abstention claims precisely; do not invalidate unrelated arithmetic without evidence.
4. Record builder population differences, cache identity status, and fixture role from leaf 03.
5. Preserve route-count disclosure and minimum coverage checks. No new headline score without measured provenance and coverage.

Files: FINAL-REPORT.md, alt-methods-correction.md, proposed `docs/workflows/classifier-evaluation.md`. Split documentation edits into checkpoints of at most three files.

### Task 2: reconcile routing documentation with current code

Files: docs/workflows/intent-routing.md, AGENTS.md, and the new evaluation workflow.

1. Verify I01: the classifier description map remains separate despite shared lane lists. Prefer accurate documentation unless a separate architectural change is approved.
2. Verify I03b: veto_path adds agreement to unanimity; no operator margin-mode switch exists at the reviewed baseline.
3. Verify D01-D03: prefilter order, cue gating scope, and empty-response-to-chat exception.
4. Verify D04: distinguish canonical lane agent IDs from special-workflow execution labels.
5. Verify I10b against current relay source. Preserve duplicate-bubble protection; do not claim full chat.response exclusion unless the relay actually excludes the event.

Reproduce low-severity claims before changing docs. If parallel source already repairs a claim, mark SUPERSEDED with the current commit and test rather than duplicating changes.

### Task 3: run offline integration gates

1. Run all leaf-specific test commands and the existing classifier self-test target.
2. Parent runs bounded agent tests, vet, and make test. Record exact commands and exit status.
3. Check graph freshness on a stable snapshot. Regenerate only with clear ownership; stale line offsets alone do not prove topology changes.
4. Recheck modified paths for private excerpts and unapproved historical changes.
5. Produce a finding-by-finding evidence ledger with actual results, not expected output.

Proposed report: `docs/plans/20260917-routing-repair/verification.md`. Create this report during execution, not during preparation. Include revised source baseline and unresolved approvals.

### Task 4: run approved scratch verification

The user approves scratch-daemon operations and local model requests on 2026-09-17. This task still waits for leaves 01-04 and execution dispatch. Production restarts, installations, cloud calls, and commits remain unauthorized.

1. Load meept-e2e-harness and meept-async-turn-architecture procedures. Inspect current CLI help before writing commands.
2. Create an isolated scratch home with both config files, no cloud providers, and no ownership of operator runtimes. Verify existing endpoint identities; never assume port labels identify models.
3. Build scratch binaries from the reviewed source. Verify session working directories, registered providers, actual serving models, and terminal-event handling.
4. Exercise accepted routing reproductions with positive controls. Await final turn events; do not score submission acknowledgements or helper results as completed work.
5. Check live outcome capture for attempted turns using identifiers and counts only. Keep private text out of the report. Do not enable drift or burst detectors.

If local providers are unavailable, record VERIFIED_OFFLINE and BLOCKED_LIVE separately. Never substitute synthetic model predictions for live evidence.

### Task 5: define the next empirical campaign, without running it

Prepare the next campaign only after measurement repairs pass:

1. Freeze corpus, fold assignments, model fingerprints, stage order, and scoring code revision before policy selection.
2. Separate tuning examples from untouched acceptance examples. Adjudicate harvested outcome labels before training.
3. Record per-stage route counts, correct routes, abstentions, latency, and actual downstream chain answers.
4. Include paired session contexts for quickplan versus code decisions. Per-message labels alone cannot validate session-dependent behavior.
5. Set acceptance thresholds and sample budget before observing results. Request approval for the campaign and local compute budget.

No model replacement, new training, detector activation, or geometric tuning follows automatically from this repair plan.

## Self-Verification Checklist

- [ ] Every finding has a current disposition and owner.
- [ ] Correction notes preserve numerical history while identifying invalid claims.
- [ ] Documentation reflects actual code paths, not remembered campaign rules.
- [ ] Offline and live evidence remain separate; blockers are explicit.
- [ ] No raw private text enters the report. Do NOT commit.

## Review Checklist (For Review Agent)

- [ ] Parent independently checks all accepted high-severity closures.
- [ ] Held-out claims use data not used for policy selection.
- [ ] Runtime verification identifies actual serving models and final outcomes.
- [ ] Graph and relay claims use a stable, current snapshot.
- [ ] Report APPROVED or unresolved gates. Do not mark the tree complete while live acceptance or privacy containment remains blocked.
