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

**Pre-registered winner rule (M4):** max E2E subject to P ≥ 97% and
OOD-R ≥ 95% on the final held-out set. No post-hoc metric changes.

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

Base corpus: `testdata/eval/classifier-test-corpus.json5` (136 cases, 12
intents). Every iteration N adds adversarial cases inspired by (a) meept chat
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
