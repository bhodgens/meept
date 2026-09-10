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
acceptance-passing wiring candidate (tfidf-veto centroid, 87.35% on
the adjudicated gold replay).

## Headline numbers

| metric | value |
|---|---|
| gold-replay acceptance | **87.35% PASS** (tfidf-veto policy; gate 86.8%) |
| synthetic E2E (3-stage cascade, iter 16) | 92.8% (bar was 91.78%) |
| chain-only baseline | 86.8% |
| corpus | 139 base + 199 adversarial = 338 gold cases |
| adjudicated real-traffic replay | 48 cases, 6 normative taxonomy rules |
| taxonomy | 13 intents (quickplan added at iter 19) |

## Milestones

- **M1 (iters 1-9)**: honest baseline reconciliation (iter-0 corrected),
  adversarial corpus, centroid-margin head adopted over k-NN
  unanimity; zero wrong routes at m=0.030.
- **M2 (iters 10-20)**: harvest waves; ModernBERT raw REJECTED (6%
  coverage); micro-guards REJECTED; 3-stage cascade CLEARS bar
  (92.8%); P-constraint reinterpretation parked → resolved as
  per-stage floors.
- **M3 (iters 13-15)**: ModernBERT fine-tune diverged (50× head-lr
  underdose — researched, fixed, retrained to 91.45% E2E; under the
  91.78% bar; Stage-0.5 benched, not dead).
- **M4**: Hermes-transcript silver harvest → user adjudication (48
  gold, 6 rules) → quickplan class → architecture ceiling finding →
  QuickPlan tree (4 leaves, SHIPPED) → e2e smokes → acceptance run
  (tfidf-veto PASSES) → this report.

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
| 2 | tfidf-veto | **87.35% PASS** |

Record: tools/classifier-eval/results/m4-gold-acceptance.md

## Open items (tracked, ordered)

1. **Outcome-loop tree** (docs/plans/classifier-outcome-loop/, authored
   + compliant): L1 persist+privacy (RAW input_summary removal —
   pre-existing gap) IN FLIGHT; L2 margin capture; L3 outcome capture;
   L4 harvest+dashboards.
2. **tfidf-veto leaf**: PROMOTED by the acceptance run — the
   acceptance-passing configuration must actually ship in the Go gate.
   Blocked on outcome-loop L1 (disjoint files, could parallelize).
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
  corpora (replay-gold, adjudication sheet) — verbatim text never
  enters git
