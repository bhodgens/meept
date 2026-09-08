# Iteration 2 Report — Adversarial corpus v1 (OOD + 6 more failure classes)

Date: 2026-09-08. Branch: classifier-iteration.

## What iteration 2 did

Authored `testdata/eval/classifier-adversarial-corpus.json5` (53 cases,
all `added_in: iteration-2`, provenance-tagged) attacking failure classes
1-6 + 8 from master.md:

| class | prefix | n | composition |
|---|---|---|---|
| 3 OOD/out-of-scope | ood- | 18 | poetry, gibberish, non-English (de/fr/ja), small talk, meta-questions about the router itself, emoji-only |
| 4 very short | short- | 8 | "ok", "yes", "go", "2", "why?", "fix it" |
| 1 near-boundary | bound- | 12 | code/debug/analyze/plan/search pairs |
| 2 compound | comp- | 3 | two+ intents in one message (gold = must NOT route; encoded as ood:true, i.e. abstain-or-chain) |
| 5 template-shift | para- | 5 | colloquial paraphrases far from corpus style |
| 6 injection | inj- | 4 | slash-commands in prose, model directives, prompt injection |
| 8 long, intent late | long- | 3 | 60-90 word inputs with intent in final clause |

Corpus total: 136 base + 53 adversarial = 189.

## Harness fixes surfaced by the adversarial cases (fix-before-measure)

1. **OOD could vote for itself.** OOD cases sat in the train index with
   label "OOD", so OOD test cases routed "OOD" unanimously (top neighbor
   sim 0.746 — another OOD case). The production gate has no OOD class;
   OOD must abstain via mixed neighborhoods, not via an OOD vote. Fix:
   OOD cases are excluded from the train index at fold build
   (eval_harness.py, `train_m = ~test_m & ~is_ood`).
2. **OOD pooling accounting bug.** OOD rows skipped in the test loop
   (stored None) were counted as NEITHER abstain nor routed in the pool
   loop — OOD-R read 0.0% with 0 abstains of 24. Fix: None now counts as
   abstain (an OOD case that never reaches a verdict is an abstain).

Both fixed in the same iteration; measured numbers below are post-fix.

## Measurement (baseline head unchanged: kNN k=5 unanimity floor 0.70)

| corpus | C | P | A | OOD-R | wrong | E2E | SCORE |
|---|---|---|---|---|---|---|---|
| base only (iter-1 re-check) | 3.7% | 100% | 100% | — | 0 | 87.29% | +0.037 |
| base + adversarial (189) | 9.1% | 93.3% | 93.3% | 100% | 1 | 87.39% | +0.049 |

Note C is not comparable to iter-1 (denominator changed: 125 non-OOD
base vs 162 non-OOD total; and the enlarged index lifts unanimity hits).
P dropped 100→93.3% — one new wrong route, one pre-existing miss.

## Orchestrator spot-check (protocol step 3)

Re-embedded + re-scored independently (fresh embedder instance,
spotcheck_iter2.py). Both wrong routes confirmed by hand:

1. `cherry-pick that commit` [git] → routed **chat** at 0.741. Top-5:
   0.741 chat "why?", 0.740 chat "2", 0.732 chat "ok", 0.718 chat "no",
   0.718 chat "continue". The empty/ultrashort chat corpus entries form a
   tight high-similarity cluster that captures anything short. Root
   cause: chat class contains degenerate near-zero-information texts
   ("", "test", "ok") whose EOS-pooled embeddings sit near the corpus
   centroid; ANY short input is then a unanimous chat vote.
2. `design the system architecture` [plan] → analyze at 0.786 (iter-1
   finding, persists — known plan/analyze boundary hole).

OOD-R 100% (24/24 abstain) is now a REAL property, not an artifact:
OOD texts lack a unanimous 5-neighborhood of any single class. The 18
gold OOD + 3 compound + 3 injection-as-abstain cases all correctly fall
through.

## Findings

- The 100%-precision property of the baseline does NOT survive contact
  with short real-world inputs: the chat micro-text cluster is a
  precision hazard (captures all short inputs as chat).
- OOD abstention is structurally sound at this embedder even before
  any fix — good news for the ≥95% OOD-R constraint.
- Coverage gain from corpus growth alone (+5.4pts) is an artifact of
  the larger index, not a gate improvement; P is the constraint.

## Fix directive for iteration 3

Attack the chat micro-cluster: separate micro-acknowledgments ("ok",
"yes", short) from real chat, and/or require min token count before the
gate runs (the Go gate already skips empty input; add length floor).
Then re-measure. No daemon code change this iteration — corpus+harness
only, per M1 scope.
