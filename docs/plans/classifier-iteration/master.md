# Classifier Iteration Campaign — Master Plan

**Branch:** `classifier-iteration` (one commit per iteration; squash later if desired)
**Date:** 2026-09-08
**Objective:** Iterate the Stage-0/embedding classification pipeline through ≥40
adversarial test-fix-retest cycles to (a) eliminate fall-throughs by catching
issues earlier in the chain, correctly, and (b) find the component mix that
yields the best measured results. We work backwards from results, not from
architecture preference.

## Objective Function (how every iteration is scored)

User invariant: **an incorrect classification is worse than a fall-through.**
Overly-tight gates that abstain are acceptable; confidently-wrong routes are
the failure mode. Scoring per iteration:

| Metric | Symbol | Priority |
|---|---|---|
| Precision of direct routes | P | **Must not regress** (target ≥97%) |
| Coverage of direct routes | C | **Primary improvement axis** |
| Held-out accuracy of the gate | A | Report |
| Macro-F1 | F1 | Report |
| OOD abstain rate | OOD-R | **≥95% required** (OOD must fall through) |
| p50 embed+decide latency | L | Report; budget 500ms |

Composite score (documented, stable across iterations):
`SCORE = C × P² − 5 × (wrong-direct-routes / total)`
The squared precision and the explicit wrong-route penalty encode the user
invariant. Fall-throughs cost nothing except missed coverage.

**Headline metric: E2E (end-to-end simulated system accuracy).**
`E2E = (gate-correct + 0.868 × abstained) / total` — uses the measured
86.8% LLM-chain baseline for abstained cases. The campaign optimizes E2E,
not gate coverage: a gate earns its latency only by raising system accuracy
above the 86.8%-only baseline.

**STALENESS NOTE (2026-09-08 audit, M17):** the 0.868 was measured on the
ORIGINAL 136-case base corpus and is applied unadjusted to the grown
corpus in every E2E since; the chain has NOT been re-measured on the
current corpus (that needs a live chain run — deliberately not faked).
Absolute E2E values inherit this unverified constant; comparisons BETWEEN
configs on the same corpus stay internally valid. See the CHAIN_BASELINE
comment in tools/classifier-eval/eval_harness.py.

**Pre-registered winner rule (M4):** max E2E subject to P ≥ 97% and
OOD-R ≥ 95% on the final held-out set. No post-hoc metric changes.

## Current Status (2026-09-08 audit — CORRECTED)

**Iter-16's "E2E 92.8% clears the M3 bar" verdict is WITHDRAWN.** The
original run scored Stage C with a seeded random draw
(`np.random.random() < 0.868`, seed 42) instead of the campaign's
deterministic expected-credit convention; the draw realized 89.9% on 109
chain cases (~+1σ luck). Deterministic recompute from the committed
per-stage counts (results/iter-16/summary-20260908-142037.json):

- q=0.50 champion: **E2E 91.44% = (87 + 47 + 0.868×109)/274 — FAILS the
  pre-registered 91.78% M3 entry bar** (q=0.30: 91.52%, q=0.40: 91.28%
  — all fail).
  - NOTE (2026-09-13): the three percentages printed above are the values
    the 2026-09-08 correction RECORDED, but the expressions beside them do
    not evaluate to them: as written they are 83.23% / 83.44% / 83.44%
    (`/274`), and the per-stage counts account for 250 cases, not 274 (24
    cases are OOD/unscored). The q=0.50 row was flagged in
    `FINAL-REPORT.md` (CORRECTIONS 2); its identically-evaluating sibling
    q=0.40 (also 83.44%) was not, and neither was q=0.30. The verdicts are
    unaffected (all values stay below the 91.78% bar). See
    `ITERATION-LOG.md` CORRECTION (2026-09-13) and
    `tools/classifier-eval/results/iter-16-corrected/report.md`.
- Full analysis: `tools/classifier-eval/results/iter-16-corrected/report.md`.
- The script is fixed (`iter16_cascade3.py` now uses deterministic
  expected credit); the original results and report are retained as-is.
- Independent real-traffic confirmation: iter-17/m4-silver
  (ecbe086a) measured expected system accuracy **84.7%** on 48 verbatim
  Hermes messages — below the synthetic claim and below chain-only
  86.8%. Both audits agree the synthetic 92.8% does not transfer and
  was partly a measurement artifact.
- **M3 status: NOT MET / remains open.** No config advances to M4
  daemon wiring on this evidence. Ladders that could change this: live
  chain re-measurement on the current corpus (0.868 is stale — see
  CHAIN_BASELINE note above), corpus/taxonomy expansion per m4-silver,
  then a deterministic re-run.

## M4 Wiring Constraint (2026-09-08 audit, H9-docs)

**The shipped Go gate is NOT the campaign's champion head, and it is the
head the campaign measured FAILING.** Shipped
(`internal/agent/embedding_prefilter.go`, read-only for this audit):
k=5 UNANIMITY kNN, floor 0.70. Campaign champion
(iter16_cascade3.py, iter-12/14 reports): CENTROID cosine, floor 0.60 +
margin 0.030 — C 34-36% at P 97.8-100%. The harness measured the
shipped unanimity head at P 90.9-91.7% (iter-12 report row: C 9.6%, P
91.7%) — precision-bar failing and coverage-starved (iter-1: top-1-NN
intent agreement is only 43%, k=5-unanimous neighborhoods ~5%).

**Consequence:** M4 daemon wiring REQUIRES REPLACING the shipped
kNN-unanimity head with the centroid-margin head (+ possibly the
Stage-0.5 cascade: ModernBERT probe rescue → chain). The shipped gate is
the algorithm the campaign measured as failing. Wiring the CURRENT gate
behind a config flag would ship a measured failure. Implementing the Go
centroid head is an OWNER DECISION and is deliberately NOT done here.

## OOD-R Gate: UNMET in iters 14-16 (pre-registered gap)

master.md requires OOD-R ≥ 95% as part of the M4 winner rule, but
iter-14/15/16 scripts skip OOD cases before Stage B/C
(`if is_ood[qi]: continue` — they exit via Stage-A's low-similarity
abstain path only, unmeasured per-stage). NO iteration 14-16 result
carries a measured per-stage OOD-R, so the OOD-R ≥ 95% leg of the
winner rule is **UNEVALUATED, not satisfied** — an unmet pre-registered
gate. Closing it: run OOD cases through Stage A AND B offline (cached
qwen3 + ModernBERT embeddings make this a pure re-scoring exercise) and
report per-stage OOD-R (e.g. iter16b), or re-run the harness, which
does score OOD. Until then any M4 claim is incomplete on this leg.

**Evaluation protocol: 5-fold gate evaluation** (not a single 70/30 split —
recall has 3 cases). Rotate: 4 folds form the kNN/train index, 1 fold is
queried; report the pooled result. Larger index folds better mimic
production. Fixed fold assignment (seed 42) for the whole campaign.

**Silver-label isolation:** chat-log replay cases with LLM-chain verdicts are
NEVER mixed into headline P/C/A. Reported in a separate table; a hand-
verified subset may be promoted to gold by explicit decision only.

**Confusion-driven corpus growth:** each iteration harvests the gate's
near-miss confusions (margin < 0.05 or dissenting-vote cases) as the primary
source of new adversarial cases — more signal per authored case than blanket
generation. Dedup guard: reject new cases with cosine > 0.95 to any existing
case (same embedder).

## Test Corpus Policy

Base corpus: `testdata/eval/classifier-test-corpus.json5` (**139 cases, 12
intents** — CORRECTED 2026-09-13; the previously recorded "136 cases" was
the pre-iter-2 size, superseded as the corpus grew. Verified:
`python3 -c "import sys;sys.path.insert(0,'tools/classifier-eval');import eval_harness as H;b,a=H.load_cases();print(len(b),len(a))"`
→ `139 250`, i.e. 139 base + 250 adversarial = 389 total). Every iteration N
adds adversarial cases inspired by (a) meept chat
logs in `~/.meept/home` session DBs and Hermes transcripts where readable, (b)
the failure classes below. Adversarial cases accumulate into
`testdata/eval/classifier-adversarial-corpus.json5` with per-case provenance
(`added_in: iteration-N, source: <chat-log|synthetic|regression>`).

Failure classes to attack (each iteration picks ≥1):
1. **Near-boundary intent pairs** — code vs debug (API errors), analyze vs
   search ("compare X" = research or search?), plan vs analyze ("design the
   architecture").
2. **Compound inputs** — two intents in one message (must abstain or route
   compound; never confidently pick one half).
3. **OOD / out-of-scope** — poetry, gibberish, non-English, personal chat that
   is NOT platform intent, prompts about the router itself.
4. **Very short inputs** — "ok", "yes", "go", "2", emoji-only.
5. **Template-shift paraphrases** — same intent, phrasing far from corpus
   style ("wouldja mind taking a gander at why this crashes" = debug).
6. **Injection-flavored** — inputs that look like skill invocations ("/plan"
   inside prose), model directives ("use model X to ...").
7. **Real-traffic replays** — actual user messages sampled from chat logs with
   LLM-chain verdicts as silver labels (marked silver, not gold).
8. **Long inputs** — 200+ word requests where the intent appears late.

## Component Variants Under Test

Baseline (iteration 0, current HEAD f272771c): Qwen3-Embedding-0.6B-4bit EOS
pooling + k=5 unanimity kNN, threshold 0.70 floor.

Permutation axes (each iteration toggles ≥1 axis, everything measured against
the same corpus split):
- **Embedder:** Qwen3-0.6B-4bit | Qwen3-0.6B instruction-prefixed |
  embeddinggemma-300m (if downloadable) | ModernBERT-base raw embeddings via
  mlx-raclate | Qwen3-4B (if download approved).
- **Head:** kNN k=5 unanimity | kNN k∈{3,4,6,8} | kNN majority k-1/k |
  centroid cosine | logistic head (SetFit-style, sklearn LogisticRegression
  over frozen embeddings) | logistic + calibrated threshold (per-class
  quantile) | two-stage: kNN abstain → logistic rescue | prototype+examples
  hybrid (intent-description line as an extra index entry per intent — the
  SemanticIndex buildIntentText pattern; cheap, targets sparse classes).
- **Thresholds:** floor ∈ {0.50..0.90 step 0.05}; per-class floors.
- **Pipeline order:** prefilter position, assert-only vs direct, prefilter →
  analyzer skip logic.
- **Additive stage (user-sanctioned):** ModernBERT-base fine-tuned classifier
  between prefilter and final LLM. Trained via mlx-raclate on Apple Silicon.
  Sits at "Stage 0.5": runs only when Stage-0 abstains, routes when its
  calibrated confidence ≥ τ; otherwise falls to the LLM chain.

Reference model from the ideation conversation (ChatGPT share, 2026-09-07):
cascade of ModernBERT classifier → embedding router → tiny LLM, with
calibrated probabilities, deterministic overrides, and OOD fallback. The
campaign tests whether each stage earns its latency.

## Per-Iteration Protocol (identical every time)

1. **Plan:** pick failure class(es) + component permutation; write hypothesis
   in the iteration log (one line: what we expect and why).
2. **Measure** — embeddings cached on disk keyed by (model, text-hash);
   nothing re-embeds. TWO measurement modes by token cost:
   - **Mechanical sweep (no subagent):** pure permutation sweeps over cached
     embeddings run in-session via execute_code. One sweep evaluates 5-10
     permutations (embed once, score all heads/thresholds against the cache).
   - **TEST subagent** (`delegate_task`, leaf): dispatched only for reasoning
     work — authoring adversarial cases (from chat logs / confusion harvest),
     analyzing misclassifications, or anything needing judgment. The brief
     INLINES the corpus format + harness usage (no rediscovery). Output rule:
     write `results/iter-N/report.md` + `predictions.json` to disk; return
     ≤10 lines + the path. Never paste tables into chat.
3. **Orchestrator review in-session:** spot-check ≥3 cases myself (re-embed,
   re-score) before trusting any number. Subagent reports are self-reports.
4. **Dispatch FIX subagent** (only if issues found): brief = report path +
   one-line directive (≤3 files, run exact test commands, report pass/fail +
   numbers only, write fix summary to `results/iter-N/fix.md`). Do NOT commit.
5. **I verify the fix in-session** (read diff, run tests, re-run eval).
6. **Commit** on `classifier-iteration` with message
   `iter(N): <change> — E2E=xx.x% C=xx% P=xx% wrong=N`.
7. **Update `ITERATION-LOG.md`** (append row). Full metrics live in the
   iteration's report file; the log row + 4 headline numbers are all that
   enters session context.

**Compaction-proof rule:** all campaign state lives in files (master.md,
ITERATION-LOG.md, results/). Every subagent brief is assembled from files at
dispatch time — never from session memory. Session compaction mid-campaign
costs a re-read, nothing else.

## 429 / Rate-Limit Policy

A subagent paused or delayed by provider rate limits (429 / quota wait) is
NOT abandoned and its work is NOT re-dispatched. Protocol:

1. On a 429 signal for a dispatched subagent, the orchestrator MARKS the
   iteration state in `ITERATION-LOG.md` (row appended: iteration number,
   stage, `WAITING-429`) and WAITS — re-polling the delegation result rather
   than spawning a replacement agent.
2. When the agent returns, resume the normal protocol at the review step
   (step 3). Its report, whatever the wall-clock delay, is treated as valid.
3. Never kill, supersede, or duplicate a 429-delayed agent. Re-dispatching
   wastes the tokens the original agent already spent and risks divergent
   results between two agents that ran the same brief.
4. This applies identically to TEST and FIX subagents. The daemon's own
   quota-parking (agent.quota_wait) already models this behavior; the
   campaign follows the same policy at the orchestration layer.

Subagent pairing per user directive: one TEST agent + one FIX agent per
iteration, with orchestrator (me) reviewing between them. FIX agent is skipped
only when the iteration is a pure measurement with zero issues.

## Harness (built once, iteration 1)

`tools/classifier-eval/` — Go-free Python harness, `eval_harness.py`:
- Loads both corpora (base + adversarial), splits train/test with a fixed
  seed (stratified 70/30; adversarial cases always in test when first added,
  graduate to train only by explicit decision).
- Loads a permutation spec (JSON: embedder endpoint, head type, thresholds).
- Produces the metrics table + per-case predictions file
  (`results/iter-N-predictions.json`) for misclassification analysis.
- Measures p50 latency (embed+decide).
- Computes E2E using the configurable chain baseline (default 0.868).
- Embedding cache: `~/.meept/classifier-eval-cache/<model-hash>/<text-hash>.npy`
  — no corpus text is ever embedded twice, and sweeps run without the server.
- NEVER mutates the main daemon config; runs entirely offline.

## Milestones (not strict iteration boundaries)

- M1 (iters 1-8): harness + baseline perms of existing components; establish
  the precision/coverage frontier; corpus grows to ~250.
- M2 (iters 9-20): head permutations (logistic, calibrated, two-stage);
  per-class thresholds; corpus ~400; target: C ≥ 35% at P ≥ 97%.
- M3 (iters 21-32): ModernBERT-base classifier trained (mlx-raclate), inserted
  as Stage-0.5; ModernBERT raw embeddings as alternate Stage-0; corpus ~500.
  Pre-registered entry bar: ModernBERT must beat M2's best E2E by ≥1pt to
  justify its runtime slot (guards against shiny-model bias). Weights download
  needs user approval — request at the M2/M3 boundary.
- M4 (iters 33-40+): best-of permutations re-validated on fresh held-out
  real-traffic replays; daemon wiring of the winner behind config; final
  report comparing against 86.8% LLM-only baseline; recommend production
  configuration.

## Non-Goals

- No wide architectural changes to ClassifyAndRoute order (guards, analyzer,
  chain stays). New stages are additive and config-gated.
- No cloud APIs — local models only, per user model policy.
- No deletion of fall-through behavior; the LLM chain is always the backstop.
- No per-iteration changes to the plan/approval pipeline.

## Risks & Mitigations

- **Subagent self-reports wrong numbers:** orchestrator spot-checks ≥3 cases
  per iteration (protocol step 3) before any fix/commit.
- **Overfitting to adversarial corpus:** train/test split fixed at first use;
  test cases never enter training; M4 re-validates on fresh replays.
- **Local model downloads need approval:** Qwen3-4B / embeddinggemma /
  ModernBERT weights require user OK — request at M3 boundary if not already
  present under /Volumes/LLMs.
- ** mlx-raclate early-release bugs:** pin to ModernBERT-base inference only
  initially; fall back to HF transformers on CPU if MLX path proves broken
  (measure, don't assume).

## Completion Criteria

40+ logged iterations, each committed. Final report:
`docs/plans/classifier-iteration/FINAL-REPORT.md` with the frontier table,
the winning configuration, its full metrics vs the LLM-only baseline, and the
explicit recommendation for (or against) production enablement.
