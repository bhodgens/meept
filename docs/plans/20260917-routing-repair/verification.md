# Routing repair — verification report and evidence ledger

Leaf 05 of [master.md](master.md) (evidence, docs, and acceptance).
Status: offline verification COMPLETE; live acceptance remains a
separate gate. Report written during execution, per 05-acceptance.md
Task 3.

## Meta

- Author: leaf 05 worker (delegated). Date: 2026-09-18.
- Revised source baseline: **752ffb64** (branch `classifier-iteration`,
  == `main` at a66a0efa + scopes-2 fix wave; leaves 01-04 landed as
  3444943c / f7a3c646 (leaf 02+03 combined commit) / 86a34991 /
  a55b0bda).
- Working tree at report time: only daemon-rewritten plan-id churn
  (`docs/plans/sealed-plan.md`, `internal/*/docs/plans/*.md` — known
  live-session artifact, not touched by this leaf) plus this leaf's
  documentation edits. No committed numeric artifact, no
  `tools/classifier-eval` source, no `internal/` Go source, no
  `.gitignore`, no git history modified.
- Gates actually run by this leaf (narrow, offline):
  - `/opt/homebrew/opt/python@3.12/bin/python3.12 tools/classifier-eval/m4_gold_acceptance.py --self-test` → exit 0, "self-test PASSED" (includes OOD-bypass, null-ratio, and `:2d` fractional-count pins).
  - `make classifier-eval-selftest` → exit 0.
  - `python3.12 -m unittest discover -s tools/classifier-eval -p test_result_privacy.py` → OK (4 skipped; skips are live-model-dependent cases, by design).
  - `python3.12 -m unittest discover -s tools/classifier-eval -p test_provenance.py` → 27 tests OK.
  - `go test -p 2 ./internal/agent -run 'TestRoutingRepair|TestClassifierLanes|TestPrefilter' -count=1` → ok 0.724s.
- Parent-owned gates NOT run here (recorded, not claimed):
  `go test -p 2 ./internal/agent` (full), `go vet ./internal/agent`,
  `make test`, `make graphs-check` (see I13 below), and all live/scratch
  operations beyond what leaf 04 already recorded.

## Finding-by-finding evidence ledger

Dispositions per master.md C5: REPAIRED_OFFLINE, VERIFIED_OFFLINE,
VERIFIED_LIVE, SUPERSEDED, BLOCKED, DEFERRED.

### MEAS-01 — copied private text in results — REPAIRED_OFFLINE (leaf 01, 3444943c)

Four producers refactored import-safe; `misses` rows carry `case_key`
identifiers, not excerpts. `test_result_privacy.py` (18 tests, 4
live-model skips) passes at HEAD. Additional writer paths returned to
parent for adjudication per leaf 01's report.

### MEAS-02 — builder population — REPAIRED_OFFLINE (leaf 03, f7a3c646)

`scripts/build_prefilter_centroids.py` default now equals the loader's
361 eligible non-OOD keys (was 222). Pinned by `test_provenance.py`
(27 tests pass). Recorded in FINAL-REPORT CORRECTIONS 8 and
`docs/workflows/classifier-evaluation.md`.

### MEAS-03 — fixture provenance — VERIFIED_OFFLINE (adjudicated)

`internal/agent/testdata/prefilter_tfidf_veto.json` (202 training docs)
was fixture metadata, not deployed-model evidence. Deleted with
recorded approval (master.md decision 4); tests build temporary models.
Correction appended in FINAL-REPORT CORRECTIONS 8.

### MEAS-04 — forced OOD abstention — REPAIRED_OFFLINE; historical claims INVALIDATED (leaf 02)

Bypass removed by 0688a9b0; scoring path hardened; self-test pins that
every held-out case reaches `head.decide` (verified: self-test exit 0
includes the "ood-only: wrong=1 and OOD_R=0 still measured" pin).
Historical invalidation scope, verified by scanning every committed
results JSON for the signature `OOD_R == 1.0 ∧ OOD_abstain ==
OOD_total`: **119 rows** across `results/summary-20260907-235210.json`,
`results/summary-20260908-000456.json`, and the `summary-*.json` files
under `iter-3` … `iter-13`. iter-14..16 summaries carry no OOD fields.
Committed artifacts left byte-identical; corrections appended
(FINAL-REPORT CORRECTIONS 6; classifier-evaluation.md CORRECTIONS 1).

### MEAS-05 — cache identity — REPAIRED_OFFLINE (leaf 03, f7a3c646)

Cache namespace now requires a verified local content fingerprint;
unverified legacy caches are fail-closed for acceptance
(`model_identity()` in eval_harness.py; `test_provenance.py` covers
verified / unverified / changed-instruction / legacy paths).

### MEAS-06 — fold evidence — REPAIRED_OFFLINE (policy, not demonstrated error, leaf 03)

Fold-evidence recording is required for acceptance; deterministic
assignments preserved. Per leaf 03's verified report: this was a policy
repair, not a demonstrated misassignment. So recorded.

### MEAS-07 — selection-on-test — VERIFIED_OFFLINE (corrections appended by leaf 05)

Traced: the ALT policy selection (5 variants, `alt_methods_sweep.py`)
and the iter-20 tau×guard selection (13 combos, `iter20c_sweep.py`) both
selected AND judged on the same 48-case replay; the 87.35% acceptance
row reuses the ALT selection data (corpus↔replay overlap guard is
separate and exists only in `m4_gold_acceptance.py`, which was not the
producer of the 87.35% number). Append-only caveats added where the
current claim lacked one: alt-methods-correction.md addendum
(selection-on-test leg), FINAL-REPORT CORRECTIONS 7 (with the plain-
language definition), and classifier-evaluation.md Rules of evidence +
CORRECTIONS 2. Existing caveats (iter-20/report.md M6b, wave5-confusion
F36 in-sample caveat) preserved untouched. Route-count disclosure and
the `MIN_ROUTED = 20` floor remain standing checks; no new headline
claimed.

### MEAS-08 — output formatting — REPAIRED_OFFLINE (leaf 02)

`:2d` fractional-count crash reproduced and fixed via `fmt_cascade_row`
(decimal estimate + `est` suffix; integers stay integers). Pinned by
the self-test (":2d … raises ValueError (bug reproduced against the
live interpreter)" pin passes at HEAD).

### MEAS-09 — incomplete ignore patterns — REPAIRED_OFFLINE (leaf 01, 3444943c)

Scoped `*.local.json5` ignore rule added after the results
re-inclusion; known committed result files remain git-visible; ignore
tests pass in `test_result_privacy.py`.

### AR-1 — arithmetic fast path ate path questions — VERIFIED_LIVE (leaf 04, 86a34991)

Repaired: `isArithmeticExpression` is a whole-string shape test,
never evaluated (`dispatcher.go:4298-4331`). Live scratch-rig
verification recorded by the parent after leaf 04: the diagnostic path
question now dispatches intent=debug method=llm (was
chat/short_message_guard), same input_hash 71bdf6b3 on both sides of
the flip. Offline group `TestRoutingRepairArithmetic` also passes at
HEAD.

### AR-2 — media URL guard ate coding requests — VERIFIED_OFFLINE (leaf 04, 86a34991)

`mediaGuardVerdict` requires media-consumption operation evidence; a
coding request citing a URL falls through. Pinned by
`TestRoutingRepairMediaOperation` through `ClassifyAndRoute` (group
passes at HEAD). VERIFIED_OFFLINE, not live: no scratch-rig run of
this exact case is recorded.

### AR-3 — locative "at" read as time evidence — VERIFIED_OFFLINE (leaf 04, 86a34991)

`hasTimeSignal`'s " at " arm requires clock anchoring — a digit on at
least one side (`timeAtClockRe`, dispatcher.go:4780-4786). Pinned by
`TestRoutingRepairArbitrationEvidence` with schedule/0.90 injected
verdicts and positive controls.

### AR-4 — punctuation defeated imperative recognition — VERIFIED_OFFLINE (leaf 04, 86a34991)

`hasLeadingImperativeVerb` strips punctuation per field
(dispatcher.go:4503-4512); "hey, create…" is still imperative. Pinned
by paired git/platform injected verdicts in
`TestRoutingRepairArbitrationEvidence`.

### I01 — classifier description map separate despite shared lane list — VERIFIED_OFFLINE, docs accurate (no change)

Current source confirms the claim as documented: `classifierLanes`
(llm_classifier.go:56) is the single emit-side list, but the per-lane
description map remains a SEPARATE literal map
(`getIntentDescription`, llm_classifier.go:721-760) with its own
coverage guard (`TestClassifierLanes_EveryLaneHasADescription`). The
workflow doc already described this accurately; no architectural
change approved, none needed. No edit.

### I03b — no operator margin-mode switch — VERIFIED_OFFLINE (no change)

Current `ClassifierPrefilterConfig` (internal/config/schema.go:2454)
has `veto_path` (kNN+tfidf agreement to unanimity) but no margin-mode
operator switch; `Threshold` gates kNN votes with unanimity required.
No such switch was added by leaves 01-04. Documentation makes no
contrary claim. No edit.

### I10b — WS relay / chat.response documentation — SUPERSEDED (de671eb2, parallel work)

Verified against CURRENT source (read this session,
internal/comm/http/server.go:694-718): `transformBusEventToWS` returns
nil for `chat.response` BEFORE classification — the relay genuinely
excludes the topic now. The stale claim (chat.* prefix → agent_progress
double-delivery) was repaired by parallel commit de671eb2 ("stop
relaying chat.response to WS clients", scopes-2 F-A) with tests
`TestTransformBusEventToWS_ChatResponseNotRelayed` and the WSClassParity
fence (5b08d5d1). Marked SUPERSEDED rather than duplicated: docs
reconciled to the new behavior (multi-participant-comms.md flow
section, agent-lateral-interrogation-howto.md event table,
concepts/request-flow.md topic table). Duplicate-bubble protection is
preserved and now stronger (the RPC reply is never relayed at all).

### I11 — fallback taxonomy — VERIFIED_OFFLINE (leaf 04, 86a34991)

Degraded path now agrees with the output-based taxonomy (review +
correction clause → quickplan; review → coder; one named defect →
debugger; doc artifact → writer; informational "help me understand" →
analyze via `informationalHelpRe` demotion at KeywordClassifier).
Pinned by `TestRoutingRepairFallbackTaxonomy`; documented in
intent-routing.md fall-through section.

### I13 — stale generated graphs — VERIFIED_OFFLINE (stale), regeneration DEFERRED to parent

`make graphs-check` fails at 752ffb64: bus-topology.md/.json and
http-routes.json are stale (graphs last regenerated at a46d02c2; later
commits 0688a9b0..752ffb64 touched bus/HTTP surfaces). Per
05-acceptance.md Task 3.3, stale line offsets alone do not prove
topology change and regeneration was not this leaf's ownership call
(parallel sessions are actively touching comm surfaces — de671eb2,
5b08d5d1). Parent should run `make graphs` when the comm fix wave
settles, then `make graphs-check`. Recorded as the only red offline
gate this leaf observed.

### D01 — prefilter order in docs — SUPERSEDED by code+docs reconciliation (this leaf)

Docs claimed short/simple guard → Door 1. Current code runs the
embedding gate in `ClassifyAndRoute` step 3.25 BEFORE
`classifyIntent`'s short-message guard (dispatcher.go:1003-1117 vs
1383). intent-routing.md pipeline diagram and fall-through list
rewritten to the shipped order (this leaf), verified by reading
`ClassifyAndRoute` end to end.

### D02 — cue gating scope — VERIFIED_OFFLINE, docs accurate

`QuickPlanCuePattern` gates ONLY the quickplan lane inside the
prefilter vote (embedding_prefilter.go:402); the prefilter skips
compound-signal inputs entirely at the dispatcher (dispatcher.go:1013).
The workflow docs already matched; no edit.

### D03 — empty-response-to-chat exception — VERIFIED_OFFLINE, now documented

Confirmed in current source: an empty LLM classifier response routes
to chat with method `llm_empty_fallback_chat`, bypassing keyword
tables (dispatcher.go:1644-1669), by design ("empty = degraded model,
not ambiguous input", 2026-08-24 bench note). Leaf 04 was barred from
changing it without a product decision; no product decision has
occurred, so leaf 05 DOCUMENTS it (intent-routing.md fall-through
step 3, flagged as deliberate product behavior) rather than changing
it.

### D04 — canonical lane agents vs execution labels — VERIFIED_OFFLINE, docs clarified

`agentForIntent` resolution (frontmatter `intents:` index → static
`agentMapping` → DefaultAgent → chat) defines lane ownership; the
review pipelines attach reviewer execution labels at step level, not
lane level. intent-routing.md now states the distinction explicitly
(this leaf), preventing the recurring mistake of declaring a reviewer
in `intents:` frontmatter to "own" a lane.

### Additional repair verified: announced-action termination guard (a55b0bda, leaf 04 companion)

`announcedActionRe` + zero-tool-execution check in loop.go (nudge or
`[incomplete]` annotation instead of approving a turn that ends on
"let me examine…"). Offline: the antihallucination tests land in the
internal/agent suite (narrow group run passes at HEAD); live flip not
independently re-verified by this leaf (parent approved the scratch
rig for leaf 04; no fresh live run made here) — recorded as
VERIFIED_OFFLINE with live capture pending the parent's acceptance
pass. This ledger adds no claim beyond leaf 04's own report.

## Gates and approvals

| gate | state |
|---|---|
| classifier self-tests (both entry points) | PASS (exit 0) |
| leaf test files (privacy, provenance) | PASS |
| narrow agent test groups (RoutingRepair/ClassifierLanes/Prefilter) | PASS |
| full `make test`, vet, full agent suite | PARENT (not run here) |
| `make graphs-check` | FAIL (stale) — regeneration deferred to parent, see I13 |
| live scratch acceptance of remaining guards | BLOCKED on parent dispatch of Task 4 (rig session; not authorized for this leaf's solo run) |
| historical privacy-history removal | BLOCKED on separate user approval (unchanged from master.md decision 1) |
| next campaign execution + compute budget | NOT REQUESTED YET — design frozen below, execution needs separate approval |

## Task 5 — next empirical campaign: DESIGN FROZEN, NOT RUN

Nothing below has been executed. No model replacement, training,
detector activation, or threshold change follows from this repair.

1. **Corpus.** Freeze the 389-case loader population (361 eligible /
   28 OOD) plus the adjudicated 48-case gold replay as a HELD-OUT
   acceptance set only. Any harvested outcomes are adjudicated before
   training use and go into TUNING, never into the acceptance set.
   Corpus content hashes recorded at freeze time.
2. **Folds.** Deterministic 5-fold assignments from the tracked fold
   cache, recorded (fold-evidence hash) in every artifact; corpus
   growth during the campaign does not reassign existing keys.
3. **Model fingerprints.** Embedding model identity = verified local
   content fingerprint (MEAS-05 repair); the LLM chain model, its
   serving endpoint identity, and preprocessing settings recorded per
   stage. Endpoint labels never identify models; the fingerprint does.
4. **Stage order.** Stage A (embedding gate + veto) → Stage B
   (ModernBERT rescue, if measured) → Stage C (chain). Stage order and
   candidate policies are fixed IN WRITING before any scoring run.
5. **Thresholds before observation.** All margins/tau/guard_conf
   values are pre-registered in the campaign doc before the first
   acceptance run. Post-hoc threshold changes invalidate the run.
6. **Separation of sets.** Tuning examples and acceptance examples are
   disjoint; acceptance is scored once per candidate with no
   re-selection against acceptance results (selection-on-test is a
   refusal condition, not a caveat).
7. **Per-stage records.** Route counts, correct routes, abstentions,
   latency, and ACTUAL downstream chain answers (live outcome capture,
   not the 0.868 constant) recorded per stage. The chain constant may
   be reported alongside as a historical reference only.
8. **Paired session contexts.** Quickplan-vs-code validation requires
   paired session-context fixtures; per-message labels alone cannot
   validate session-dependent behavior (iter-20 ceiling finding).
9. **Pre-registered acceptance thresholds and budget.** Example shape
   (final numbers to be approved BEFORE the campaign, not here):
   acceptance set ≥ 100 adjudicated cases, ≥ MIN_ROUTED=20 effective
   routed sample per policy, system accuracy estimated on measured
   chain answers with a pre-registered pass bar, and a fixed local
   compute budget. Campaign execution requires separate user approval
   of corpus, thresholds, and budget.


## Live acceptance addendum (2026-09-18, parent-executed)

Two complete passes of suites/routing-repair.json (8 runnable cases each; media
control excluded by its blocked tags) on the isolated scratch rig
(/private/tmp/routing-smoke-ew5cfidy; provenance in the rig's provenance.json).

- Pass 1: 4/8 routing PASS. Found a new defect: the git-agreement veto killed a
  correct git@0.95 verdict on "Run git status --porcelain…" (gitVerbRe matched
  only action verbs). Fixed TDD: gitSubcommandRe admits adjacent "git <subcommand>"
  pairs (status/diff/log/show/blame/tag/fetch/remote/config/add/restore/switch/
  init/clone) as git-operation evidence; noun-tight controls ("what is my status",
  "log these hours") still pass.
- Pass 2 (post-fix binary d40990f1): 5/8 routing PASS. git-status-control ->
  committer/git/llm. All other routes stable across passes.
- Stable routing FAILs (model territory): locative-file + polite-file-* route
  write/writer instead of code/coder — the 8B classifier's lane preference on
  file-creation prompts; guards no longer hijack (pre-fix they dispatched
  chat/short_message_guard or media_url_guard). Per plan C4 this is corpus/prompt
  work, not guard territory.
- Outcomes 0/8 both passes: 8B tool-emission gaps (staged pending writes instead of
  file_write; worktree-root-as-path misreads). The announced-action guard (a55b0bda)
  fired live, annotating "[unverified: no tools were executed this turn]" on a
  fabricated-success attempt. Judge verified correct on a known-good answer, so
  outcome fails are genuine model failures, not harness defects.
- Suite SHA256 7535193e4ba4b74c9ef56133df0aec7218d49df3b356bf79c2510873cd360632;
  evidence_status=available on all 16 attempts (fresh_session_unique_dispatch).
- Media control: BLOCKED (producer lacks turn/step-to-tool + URL/content evidence).
  I13 stale graphs: still open, deferred pending clear ownership after the parallel
  comm fix wave.
