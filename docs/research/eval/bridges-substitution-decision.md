# Bridges Substitution Experiment - Decision Record

Leaf: docs/plans/20261030-memory-second-brain/07-bridges-substitution-eval.md
Date: 2026-11-04 (MST)
Artifact: docs/research/eval/bridges-substitution-eval.json
Fixture: tools/bridges-eval/fixtures/substitution-fixture.json
Fixture hash (sha256-12): 227f35842d69c7e5bcf4e45f
Config hash: 893275a4af9c16daba3711dd (kb=2, top_k=8, match_threshold=0.6, bridge_df_max=10, verify=true, live=true)

## Verdict

**Bridges STAY default-ON.** The frozen decision rule is satisfied:

> bridges STAY default-on iff arm B (bridges) strictly beats arm A on
> substitution questions by >= 5 percentage points AND does not regress
> non-substitution questions by > 2 points.

No config edit is required; leaf 02's `memory.bridges.enabled=true` default
stands as shipped.

## Numbers (live run, raw artifact cited)

Both arms ran on the SAME fixture, model, endpoint, and config; the harness
resume logic keys rows by fixture+config hash. Per-subset fact-coverage and
question-correctness were computed from the per-question rows in the artifact
(`bridges-substitution-eval.json`, `questions[]`, grouped by question id
prefix: `sub-` = substitution, `ctl-` = control).

| Metric | Arm A (flat top-8) | Arm B (top-8 + bridges) | Delta |
|---|---|---|---|
| substitution fact-coverage | 38.10% | 85.71% | **+47.61pp** |
| substitution question-correct | 5/12 (41.7%) | 9/12 (75.0%) | +33.3pp |
| control fact-coverage | 91.67% | 100.00% | +8.33pp |
| control question-correct | 11/12 (91.7%) | 12/12 (100.0%) | +8.3pp |
| mean context tokens | 344.3 | 377.4 | +9.6% |
| errors | 0 | 0 | - |

Rule application:

1. Substitution win >= 5pp: 47.61pp >= 5pp -> PASS.
2. Control regression > 2pp: regression is 0.00pp (control IMPROVED) -> PASS.
3. Verdict: STAY ON (both conditions satisfied; the orchestrator executes any
   flip, and no flip is warranted).

Token cost: arm B carries +9.6% mean context tokens (2 bridges/query, capped
at kb=2). The frozen rule does not gate on tokens; the increase is recorded
here for the owner's awareness (the prior live eval on the original fixture
showed ~10% more tokens for a tie; the substitution fixture shows the tie was
an artifact of facts always fitting top-8).

## Run conditions

- Model: LFM2.5-8B-A1B Q4_K_M GGUF (same model family and quant as the prior
  bridges live eval baseline).
  sha256 (prefix): af0cc01b3cd9122ae0f4589464053e33
  Path on run host: /Volumes/LLMs/LiquidAI/LFM2.5-8B-A1B-GGUF/LFM2.5-8B-A1B-Q4_K_M.gguf
- Server: llama-server b7730, 127.0.0.1:8401, --jinja --ctx-size 8192;
  readiness verified via /v1/models (not just /health); owned by this run,
  killed after.
- Answer lane: harness --live (liveAnswerClient, reasoning_content fallback,
  max_tokens 2048, temperature 0.2). Grader: unchanged mock grader
  (token-overlap threshold 0.6) over the model's reply.
- Artifact `run_at`: 2026-11-05T01:57:02Z. Bridge pool: 3 generated, 3
  verified, entities [harbor, kestrel, zenith].

## Fixture design (what makes it a substitution test)

Each cluster (zenith/harbor/kestrel) has 14 memories: 2 long TAIL memories
whose FIRST sentence carries the answer facts, followed by fact-free filler
(30-40 tokens of neutral office noise); 8 short SHELL memories that share the
cluster head entity and the question's event nouns but carry no answers; and
4 control-answer memories. Every question declares 12 relevant memories (>8),
so answers do not fit a flat top-8 by construction.

The shell families are tuned per event noun family so, under the harness's
hash-word-embedding ranking, the tail sinks below the top-8 cut for 6 of 12
substitution questions (50%, requirement >=30%): those questions REQUIRE a
two-hop connection (question -> shared entity -> bridge fact assembled from
the tail's first sentence) that flat top-8 recall structurally cannot
satisfy. Arm B injects the kb=2 bridge cap, and the bridge carries the tail's
answer sentence verbatim, so arm B context covers 12/12 substitution
questions (hermetic context-coverage check in the artifact; live answers
recover 9/12).

Validation performed before the live run (recorded here because it shaped the
fixture):

- LoadFixture constraints hold (24 questions, valid references).
- Flat top-8 starves the answer fact for 6/12 substitution questions
  (>= 30% two-hop requirement).
- Arm B injected context covers all 12 substitution questions.
- All 12 control questions are answerable from flat top-8 (controls measure
  non-regression, not substitution).
- Duplicate-question pairs (z-01/z-02 etc.) target the same tail fact with
  different phrasings to damp single-prompt variance.

## Known limitations (recorded, not judgment overrides)

- The frozen rule was applied to fact-coverage (the harness's primary
  aggregate); question-correctness deltas (+33.3pp substitution, +8.3pp
  control) point the same direction, so the verdict is robust to either
  metric choice.
- sub-k-03 fails BOTH arms ("not in context"): the model does not map
  "what number ... did kestrel keep?" to the spelled-out "kept ninety percent
  of trial users" in the bridge clause. The failure is arm-symmetric, so it
  does not bias the comparison, but it shows the 1.2B-active model
  sometimes fails to use a bridge even when present.
- sub-z-01/sub-z-02 score 50% in arm B (model drops the "closed in march"
  date). Arm-symmetric risk: arm A scores 0% there, so direction is
  unaffected.
- The fixture's displaced-vocabulary design interacts with the harness's
  lexical hash embeddings: any question phrased so a 1.2B-active model can
  parse it tends to also rank the tail into flat top-8 (the two effects share
  the same lexical signal). The shell/crowd architecture above is the
  workaround; a semantic-embedder rerun would likely show a STRONGER
  substitution effect, not a weaker one, since semantic ranking would bury
  the filler-laden tails harder while the bridge facts stay dense.
- Temperature 0.2 is not greedy; single-run variance exists. Duplicate
  question pairs bound it; a confirmation rerun on a different day is cheap
  (both arms together take ~2 minutes on this stack) and is recommended
  before relying on the exact percentages.

## Action items

- None for config (bridges remain default-ON; no flip-back).
- Optional follow-up for the owner: rerun the eval with the production
  semantic embedder once recall-side embedding swap lands, to confirm the
  effect survives lexical-embedding removal.
- This leaf does NOT edit config; leaf 02's knobs stay as committed.
