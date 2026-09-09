# Iteration Log

One row per iteration. SCORE = C × P² − 5 × (wrong/total). C=coverage of
direct routes on the full test split, P=precision of those routes, A=held-out
accuracy when forced (no abstain), OOD-R=OOD abstain rate, L=p50 latency.

| # | Permutation (embedder / head / threshold) | Corpus | C | P | A | F1 | OOD-R | L(ms) | Wrong | Fix applied | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 0 | qwen3-0.6B-4bit EOS / kNN k=5 unanimity / floor 0.70 (pre-campaign baseline, LOO) | 136 | 14.7% | 100% | — | — | untested | ~300 | 0 | self-match exclusion (f272771c) | baseline — CORRECTED at iter 1: 14.7% not reproducible (see results/iter-1/report.md); honest 5-fold value is C 3.7% |
| 1 | qwen3-0.6B-4bit EOS / kNN k=5 unanimity / floor 0.70 — 5-fold cross, seed 42 | 136 | 3.7% | 100% | 100% | 0.056 | n/a (no OOD cases yet) | ~8/embed cached | 0 | harness built (tools/classifier-eval); iter-0 reconciled | E2E 87.29% ≈ chain-only 86.8%; coverage structurally capped (top-1-NN agreement 43%) |
| 2 | same head; +53 adversarial cases (OOD/short/boundary/compound/para/inj/long) | 189 | 9.1% | 93.3% | 93.3% | 0.084 | 100% | ~8/embed cached | 1 | harness: OOD excluded from train index + OOD pooling fix | E2E 87.39%; OOD abstains 24/24; chat micro-cluster is a precision hazard (captures short inputs as chat) |
| 3 | instruction-prefixed embedder × kNN k/vote sweep (mechanical) | 189 | 10.3% | 88.2% | 88.2% | 0.094 | 100% | ~15/embed cached | 2 | none (pure measurement) | REJECTED: prefix lowers P 93→88% at kNN; all majority heads E2E-negative; keep plain EOS pooling |
| 4 | head-family sweep: centroid-margin / logistic / prototype-hybrid / two-stage (mechanical) | 189 | 34.5% | 93.0% | 93.0% | 0.367 | 100% | ~0 (cached) | 4 | none (pure measurement) | CENTROID margin 0.03 WINS: E2E 88.94% (+2.1pts over chain-only); P 93% still under 97% bar; logistic dead (prob ceiling 0.54); per-class margins next |
| 5 | per-class margin floors vs uniform (mechanical) | 189 | 34.5% | 93.0% | 93.0% | 0.367 | 100% | ~0 (cached) | 4 | none (pure measurement) | per-class REJECTED (no E2E gain); uniform 0.045 = precision point (P 96.8% C 18.8% E2E 88.67%); 4 wrongs audited → 3 are base-corpus label defects → iter-6 repairs corpus |
| 6 | base-corpus repair (label defects) + margin fine-bracket | 192 | 34.5% | 96.6% | 96.6% | 0.407 | 100% | ~22/embed cached | 2 | corpus: scheduled-job code example replaced (2 code anchors), report/recall +1 anchor each | BIGGEST GAIN: E2E 90.17% (+1.23pts); P 93→96.6% from data repair alone; remaining 2 wrongs are genuine boundary ambiguity (chain's job) |
| 7 | confusion-harvest +29 cases (dedup-guarded); margin brackets | 221 | 26.9% | 98.1% | 98.1% | 0.351 | 100% | ~0 (cached) | 1 | corpus: boundary anchors code/search/analyze/chat/git/sched/plan/platform | FIRST ≥97% P config: centroid m0.035 C 26.9% P 98.1% E2E 89.84%; coverage point m0.025 C 37.6% E2E 90.24%; fix-shape-wins convention adopted |
| 8 | fix-shape relabel measured; M1 frontier freeze | 221 | 34.0% | 98.5% | 98.5% | 0.427 | 100% | ~0 (cached) | 1 | corpus: segfault-fix case code→debug (fix-shape convention) | M1 CHAMPION: centroid m0.030 C 34.0% P 98.5% E2E 90.78% (+3.98 over chain-only); data repairs +2.0 E2E vs +1.1 from all gate perms; M3 entry bar set at E2E ≥ 91.78% |
| 9 | hotfix relabel code→debug + 3 non-fix-verb code anchors | 225 | 34.0% | 100% | 100% | 0.430 | 100% | ~0 (cached) | 0 | corpus: fix-shape convention completed; code anchors avoid fix-verbs | ZERO WRONG at champion: m0.030 C 34.0% P 100% E2E 91.29% (+4.49 over chain-only); m0.025 also bar-compliant now (C 41.0% P 97.6%) |
| 10 | harvest wave 2: +39 near-miss-cluster cases | 264 | 33.5% | 97.5% | 97.5% | 0.496 | 100% | ~0 (cached) | 2 | none (measurement) | E2E dip is mechanical (new cases land in TEST fold first); 2 new wrongs are intentional boundary cases (abstain-correct at m0.035); centroid-vs-kNN head gap now decisive |
| 11 | micro-guards: word-count floor + absolute sim floor (both REJECTED) | 264 | 33.5% | 97.5% | 97.5% | 0.496 | 100% | ~0 (cached) | 2 | none (measurement) | word floor removes easy coverage, wrongs survive ('fix it' is 2 words); sim floor inert (margin subsumes it); champion unchanged: plain centroid m0.030 |
| 12 | harvest wave 3: +11 near-miss reinforcement (4 dedup-rejected) | 275 | 35.6% | 97.8% | 97.8% | 0.524 | 100% | ~0 (cached) | 2 | corpus: debug/plan/analyze/report/review/code cluster anchors | C 33.5→35.6% at steady P; E2E recovering 90.38→90.70%; review#2 stays top1-wrong → M4 replay validation will adjudicate review-intent scope |
| 13 | ModernBERT-base raw embeddings as alternate Stage-0 (weights user-approved, downloaded) | 274 | 6.0% | 93.3% | 93.3% | 0.080 | 100% | ~90/embed (MPS) | 1 | corpus: none | REJECTED: raw ModernBERT C ~6% vs qwen3 35.6% at comparable P; fails entry bar badly; fine-tuned Stage-0.5 head queued as iter-14 (training reshapes the space — raw failure doesn't predict it) |
| 14 | fine-tuned ModernBERT Stage-0.5 (tau 0.70-0.90): training divergent | 275 | 35.6% | 97.8% | 97.8% | 0.524 | 100% | ~90/embed (MPS) | 2 | none (measurement) | STAGE-0.5 ADDED 0 ROUTES: loss flat at ln(12), confidence uniform 1/12, forced acc = chance — fine-tune did not train (MPS fused-kernel or lr/step-dose suspect). Parked: CPU + frozen-backbone variants vs closing M3 early |
| 15 | RESEARCH-DRIVEN RETRAIN: head-lr 1e-3 (was 2e-5, 50x underdose), CPU, quantile tau | 274 | 56.4% | 95.0% | 95.0% | 0.640 | 100% | ~90/embed CPU | 7 | technique fix: split head/backbone lr + quantile calibration | STAGE-0.5 WORKS: rescue adds +10-21pts coverage at 90-92% rescue precision; best E2E 91.45% (q=0.40) — 0.23pt under the 91.78% entry bar; iter-16: margin-gated rescue or corpus wave 4 |
| 16 | 3-STAGE CASCADE (user arch): A centroid / B ModernBERT probe / C lfm-8b chain @0.868 | 274 | 100% | 92.8% | 92.8% | 0.690 | 100% | ~90/embed CPU | 18 | arch: chain folded in as stage C, aggregate E2E objective | CLEARS BAR: E2E 92.80% (q=0.50) vs 91.78% bar — +6.0 over chain-only; A 97.8% P, B 90.4% P; ~half chain load removed; P>=97% reinterpretation parked with user (a/b/c options in report) |
| 17 | Hermes-transcript silver validation (48 cases, untracked corpus) | 275 | 100% | — | — | — | — | ~90/embed CPU | — | harvester + silver harness (validate_silver.py) | REAL-TRAFFIC GAP: expected system acc 84.7% vs 92.8% synthetic; misses = plan-execution + doc-writing shapes absent from gold; no daemon wiring until silver acc > 86.8% w/ margin |
| 18 | wave 4: +39 plan-execution/docs/compound cases (subagent-authored, dedup 0 rej) | 314 | 100% | — | — | — | — | ~0 (cached) | — | corpus: taxonomy expansion | silver E2E flat (84.4% vs 84.7%, noise); plan/code execution boundary is now THE ambiguity — 1 miss became a correct abstain, 1 became a confident plan-route; silver-label adjudication recommended before more anchors |

## CORRECTION (2026-09-08 audit) — iter-16 verdict withdrawn

**Row 16's "CLEARS BAR" is wrong and is superseded; the row is retained
as history.** `iter16_cascade3.py:141` scored Stage C with a seeded
random draw (`np.random.random() < 0.868`, seed 42) instead of the
campaign's deterministic expected-credit convention (eval_harness.py:579).
The draw realized 98/109 = 89.9% on chain-destined cases (~+1σ luck
over the true 86.8%) — that luck alone manufactured the headline.

Deterministic recompute from the committed per-stage counts
(results/iter-16/summary-20260908-142037.json; E2E is an exact linear
function of them):

- q=0.30: (87+23+0.868×136)/274 = **91.52%** — below bar
- q=0.40: (87+34+0.868×124)/274 = **91.28%** — below bar
- q=0.50: (87+47+0.868×109)/274 = **91.44%** — **FAILS the
  pre-registered 91.78% M3 entry bar**

**M3 verdict: NOT MET.** Full analysis:
`results/iter-16-corrected/report.md`; the script is fixed in place.

**Independent real-traffic confirmation (iter-17/m4-silver, ecbe086a):**
the same-cascade silver replay of 48 verbatim Hermes messages measured
expected system accuracy **84.7%** — below the synthetic claim AND below
the 86.8% chain-only floor. The two findings agree: the synthetic 92.8%
was partly measurement artifact (this correction) and does not transfer
to real traffic (m4-silver's taxonomy/corpus-mix gap).

Also recorded this audit (details in master.md): (1) the shipped Go gate
is k=5-unanimity/floor-0.70 — the head the campaign measured FAILING
(P 90.9-91.7%) — while the champion is centroid/floor-0.60/margin-0.030;
M4 wiring requires REPLACING the shipped head, not flagging it.
(2) iters 14-16 skip OOD before Stage B, so the pre-registered
OOD-R ≥ 95% leg is UNEVALUATED for the cascade configs — an unmet gate,
not a satisfied one.
