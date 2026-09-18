# Classifier Campaign — FINAL REPORT

Campaign: docs/plans/classifier-iteration/master.md (M1-M4)
Period: 2026-09-06 → 2026-09-10
Branch: classifier-iteration
Status: COMPLETE (measurement campaign); follow-up trees authored

## Executive summary

Twenty measured iterations took the meept intent prefilter from an
irreproducible 14.7%-coverage baseline to a validated three-door
routing architecture, then produced two shipped features beyond the
original scope (QuickPlan mode, capability-aware allotment) and one
wiring candidate for which the acceptance run claimed 87.35% on the
adjudicated gold replay — a verdict now recorded as UNVALIDATED (see
CORRECTIONS).

## Headline numbers

| metric | value |
|---|---|
| gold-replay acceptance | **87.35% UNVALIDATED** (tfidf-veto policy; gate 86.8%) — see CORRECTIONS 1 |
| synthetic E2E (3-stage cascade, iter 16) | 92.8% WITHDRAWN → 91.44% FAIL — see CORRECTIONS 2 |
| chain-only baseline | 86.8% |
| corpus | 139 base + 250 adversarial = 389 gold cases (361 non-OOD / 28 OOD) — see CORRECTIONS 3 |
| adjudicated real-traffic replay | 48 cases, 6 normative taxonomy rules |
| taxonomy | 13 intents (quickplan added at iter 19) |

## Milestones

- **M1 (iters 1-9)**: honest baseline reconciliation (iter-0 corrected),
  adversarial corpus, centroid-margin head adopted over k-NN
  unanimity; zero wrong routes at m=0.030.
- **M2 (iters 10-20)**: harvest waves; ModernBERT raw REJECTED (6%
  coverage); micro-guards REJECTED; 3-stage cascade 92.8% (later
  withdrawn → 91.44% FAIL; see CORRECTIONS 2); P-constraint
  reinterpretation parked → resolved as per-stage floors.
- **M3 (iters 13-15)**: ModernBERT fine-tune diverged (50× head-lr
  underdose — researched, fixed, retrained to 91.45% E2E; under the
  91.78% bar; Stage-0.5 benched, not dead).
- **M4**: Hermes-transcript silver harvest → user adjudication (48
  gold, 6 rules) → quickplan class → architecture ceiling finding →
  QuickPlan tree (4 leaves, SHIPPED) → e2e smokes → acceptance run
  (tfidf-veto 87.35% UNVALIDATED — see CORRECTIONS 1) → this report.

## Key findings (the durable knowledge)

1. **Data repair beats gate tuning.** Corpus relabels delivered +2.0
   E2E vs +1.1 from all head/embedder permutations combined.
2. **Per-message 100% is information-theoretically unreachable.**
   Quickplan-vs-code is a session-state judgment; message text alone
   cannot decide it (iter-20 plateau, 4 residual misses all
   plan-existence-shaped).
3. **The classifier was not the over-engineered part.** The missing
   piece was orchestrator allotment: count-capped steps, no
   context-size awareness (user's diagnosis, confirmed by
   investigation).
4. **Strict gates + chain floor beat loose gates.** Every loosened-
   margin variant lost; the tfidf agreement-veto (two weak learners
   must concur) is the efficient precision play.
5. **LR underdose is the classic fine-tune failure mode.** iter-14's
   flat loss was a 50× head-lr error, not bad data or a bad model.

## Shipped code (all committed on classifier-iteration)

### QuickPlan mode (10 commits, 4870fc98..ebfc7a2b)
IntentQuickPlan + quick_plan mode; clarify-if-ambiguous-then-execute;
quickplan as final classifier fallback; QuickPlanCuePattern guard on
prefilter votes; session-context block for the orchestrator; docs.
Two latent bugs fixed en route (clarification state never recorded;
analyzer coerced quickplan to "other").

### Capability-aware allotment (3 commits, 05b63ecc..cf527d44)
AllotmentTokens/SplitStepsByAllotment math; ContextWindowProvider on
the tactical scheduler; continuation batches as real chained steps;
ExecutorModelRef plumbing; daemon resolver closure. Small models get
small batches; 128k models take whole phases. Legacy byte-identity
pinned when no window is known.

### Measurement + tooling
eval_harness.py (5-fold, seed 42, fold cache), centroid builder
(corpus-format fixes), harvest_hermes.py (untracked-output pattern),
alt_methods_sweep.py, m4_gold_acceptance.py, adjudication workflow.

## Acceptance runs (M4 gate)

| run | policy | result |
|---|---|---|
| 1 | double-confidence | 84.56% FAIL |
| 2 | tfidf-veto | **UNVALIDATED** (87.35% cannot be re-derived; see CORRECTIONS 1) |

Record: tools/classifier-eval/results/m4-gold-acceptance.md

## Open items (tracked, ordered)

1. **Outcome-loop tree** (docs/plans/classifier-outcome-loop/, authored
   + compliant): L1 persist+privacy (RAW input_summary removal —
   pre-existing gap) IN FLIGHT; L2 margin capture; L3 outcome capture;
   L4 harvest+dashboards.
2. **tfidf-veto leaf**: the acceptance run named it the candidate, but
   its verdict is UNVALIDATED and its routed sample is 2 cases (see
   CORRECTIONS 1/2) — re-measure on a larger harvested corpus before
   wiring the Go gate. The shipped Go gate and the measured policy are
   also still different programs (kNN unanimity vs centroid+veto).
3. **Centroid-margin head wiring**: subsumed by the tfidf-veto leaf
   (the veto IS the margin head plus the agreement check).
4. **Stage-0.5 revisit**: only after the outcome loop produces real
   misroute data worth training on.
5. **Codify memory of results**: see Observations below.

## Permanent rejections (do not revisit without new evidence)

- Instruction prefix on the embedder (P 93.3→88.2)
- k-NN majority heads of any k (E2E-negative)
- Plain logistic over qwen3 (softmax ceiling ~0.54)
- Per-class margin floors (no gain)
- Two-stage rescue head v1 (rescue never fired — pre-lr-fix)
- Raw ModernBERT embeddings as Stage-0 (6% coverage)
- ModernBERT fine-tune at single low lr (divergent; fixed technique
  measured under bar)

## Environment state at close

- Embed server: qwen3-0.6B-4bit-DWQ on :8090 (own instance)
- Scratch daemon: /tmp/qp-smoke rig on :18096 (throwaway)
- LFM2.5-8B runtime: :8082 (user-owned; adopted read-only)
- ModernBERT-base weights: /Volumes/LLMs/answerdotai/ (for the benched
  Stage-0.5)
- All campaign artifacts git-tracked except untracked-by-design local
  corpora (replay-gold, adjudication sheet). NOTE: the earlier claim
  that "verbatim text never enters git" was **false** and is withdrawn
  — see CORRECTIONS 4.

## CORRECTIONS (2026-09-12 audit wave)

Appended in the campaign's append-only style: the sections above are
left as written; the entries below are the corrections of record.

### 1. The 87.35% acceptance verdict is UNVALIDATED (was: PASS)

No committed script produced the 87.35% row. The only committed
acceptance artifact is `tools/classifier-eval/results/m4-gold-acceptance.json`
= `{"policy":"double-confidence", "a_routes":7, "a_correct":5, "chain":41,
"system_accuracy":0.8456, "verdict":"FAIL"}`, written by a script that
implemented ONLY the double-confidence policy. The veto policy's only
tool (`alt_methods_sweep.py`) prints to stdout, writes no artifact, and
needs the UNTRACKED replay corpus plus a live embed server on :8090.
The veto verdict is therefore recorded as UNVALIDATED in the acceptance
record, in `internal/agent/tfidf_veto.go`, and in
`docs/workflows/classification-architecture.md`. `m4_gold_acceptance.py`
now runs both policies and writes a write-once per-policy artifact.

### 2. The iter-16 92.8% "CLEARS BAR" is WITHDRAWN → 91.44% FAIL

`tools/classifier-eval/results/iter-16-corrected/report.md:44-56`
supersedes the iter-16 verdict: it records a deterministic recompute of
**91.44% FAIL** — below the pre-registered 91.78% bar — and states the
original "clears bar" claim is withdrawn. The headline tables above are
corrected to point at this.

> Note (2026-09-12): the deterministic formula PRINTED in
> `iter-16-corrected/report.md` and in the ITERATION-LOG CORRECTION block
> — `(87+47+0.868×109)/274` — evaluates to 83.44%, not the 91.44% it is
> labelled with. The discrepancy is in the campaign record and is
> reported upward, not silently re-derived here; the number cited above
> is the report's own recorded value.

### 3. The corpus row was wrong (338 → 389)

Recomputed from the committed loader
(`H.load_cases()`; command in the audit record):
base 139 + adversarial 250 = **389** (non-OOD 361, OOD 28; 0 duplicate
texts). The old "139 base + 199 adversarial = 338" row was two waves
stale.

### 4. "verbatim text never enters git" was false

The gitignored harvest output directory and the untracked replay keep
verbatim text local, but harvested rows adjudicated into the committed
`testdata/eval/classifier-adversarial-corpus.json5` are byte-identical
user messages from `~/.hermes/sessions`. Eight such rows
(`h18-planexec-001`, `h20-002`, `h20-003`, `h20-004`, `h20-008`,
`h20-013`, `h20-019`, `h20-020`) were committed with
`source: "hermes-inspired"`; they are now labelled `source:
"live-session"`. See `docs/workflows/classification-architecture.md`
(harvest section) for the corrected invariant and the harvest-time
overlap guard.

### 5. Acceptance sensitivity (one route decides the verdict)

At n=48 with 46 chain-destined cases (under the veto) credited at the
hard-coded `CHAIN = 0.868`, the veto policy's 87.35% is
`(2 + 46×0.868)/48`: two correct routes plus the chain's credit on 46 of
48 cases — 95.83% of the CASES, 95.23% of the SCORE
(`46×0.868 / (2 + 46×0.868)`; the two shares are not the same number).
One flipped route gives `(1 + 46×0.868)/48 = 85.27%`, below the 86.8%
gate. The acceptance
script now enforces a coverage floor (`MIN_ROUTED = 20`) and reports
`INSUFFICIENT_COVERAGE` instead of PASS/FAIL below it; the observed
values (87.35 / 84.56) are unchanged.

### 6. Forced-abstention fabrication: OOD fields of iter-1..13 summaries are invalid (2026-09-18)

`eval_harness.py` historically inserted `None` for OOD cases without
calling `head.decide`, then counted the inserted `None` as a successful
abstention (`OOD_abstain == OOD_total`, `OOD_R = 1.0` on every row). The
bypass was removed by `0688a9b0`; leaf 02 of the routing-repair tree
hardened the self-tests so any held-out case skipping prediction fails
`m4_gold_acceptance.py --self-test`.

Invalidation scope, precisely: the `OOD_total` / `OOD_abstain` / `OOD_R`
FIELDS of 119 committed summary rows —
`results/summary-20260907-235210.json`,
`results/summary-20260908-000456.json`, and every `summary-*.json` under
`results/iter-3` through `results/iter-13` — are fabricated, not
measured. iter-14..16 summaries carry no OOD fields and make no OOD
claim. WITHOUT evidence of a prediction call these rows never
demonstrated OOD rejection; the M1 claim "zero wrong routes at m=0.030"
rests only on in-domain wrong counts and stands, but every
OOD-abstention-derived quality claim in this report is struck. The
in-domain arithmetic in the same files (`total`, `direct`, `correct`,
`wrong`, `E2E`, `SCORE`, `P`, `A`, `F1`) comes from real prediction
calls and is NOT invalidated. The committed JSON artifacts are left
byte-identical (this section and
`docs/workflows/classifier-evaluation.md` are the corrections of
record).

### 7. Policy-selection data reuse (2026-09-18 selection-on-test note)

The M4 acceptance run re-used policy-SELECTION data: ALT-5 (tfidf-veto)
was selected by comparing five variants on the same 48-case replay the
acceptance later scored, and the iter-20 cascade picked its tau×guard
policy by a 13-way sweep on the same 48 cases (selection-on-test, see
`results/iter-20/report.md` CAVEAT M6b). Selection-on-test means
choosing and judging a policy on the same examples: the reported number
is a max over in-sample scores and overstates held-out accuracy. No
headline in this report is a held-out estimate; the 87.35% verdict is
UNVALIDATED (CORRECTIONS 1/5), not merely unmeasured. The follow-up
campaign design (`docs/plans/20260917-routing-repair/verification.md`)
separates tuning from acceptance sets and freezes thresholds before
observation.

### 8. Corpus, cache, and fixture status as of the routing-repair tree (2026-09-18)

- Corpus row (CORRECTIONS 3) confirmed at 389 = 139 + 250 (361 non-OOD /
  28 OOD) against the committed loader.
- The centroid builder previously trained Door 1 on a 222-key subset of
  the 361 eligible keys (MEAS-02); the default now equals the loader's
  eligible population (`tools/classifier-eval/test_provenance.py`).
- Embedding cache identity previously keyed on the model ALIAS
  (MEAS-05); it now hashes verified local model content, with
  fail-closed `unverified` namespaces for legacy caches.
- `internal/agent/testdata/prefilter_tfidf_veto.json` (202 training
  docs, cited in the acceptance discussion) was FIXTURE metadata, never
  evidence about a deployed model; it was deleted with recorded
  approval and tests build temporary models.
- Historical artifacts remain byte-identical; corrections are appended
  only. Measurement methodology lives in
  `docs/workflows/classifier-evaluation.md`.

