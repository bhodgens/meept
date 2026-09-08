# Classifier Stage-0 Embedding Prefilter

Stage-0 of `ClassifyAndRoute` (classifier-observability follow-up,
`docs/plans/classifier-observability/HANDOFF-STAGE0.md` §5/§8/§9): a
kNN-unanimity gate over labeled example embeddings that direct-routes
confident inputs BEFORE the intent analyzer and routing classifier run —
skipping two serialized LLM calls (~0.3s embed + cosine vs 0.3–28s
analyzer + up to 15s router).

Design priority (user invariant, 2026-09-07): **a wrong answer with
overstated confidence is worse than a low-confidence correct one.** The
gate is precision-first: it abstains unless the evidence is unanimous.

## How it works

```
user input
   │
   ▼
[Stage 0: EmbeddingPrefilter.Match]
   embed input (OpenAI-compatible /v1/embeddings, local server)
   kNN over labeled example vectors (k=5)
   vote requires:
     - all 5 neighbors above the cosine floor (threshold, default 0.70)
     - ALL 5 agree on one intent (unanimity; any dissenter kills it)
   │ unanimous → route DIRECTLY (method="embedding_prefilter")
   │             analyzer + router never run
   ▼ otherwise
[existing chain unchanged]
   analyzer (ambiguity gate) → router (llm→keyword→semantic→heuristic)
```

The prefilter can only SKIP work, never degrade routing: any vote failure,
embed error, timeout, dimension mismatch, or missing index returns nil and
the LLM chain runs exactly as before. Compound-signal inputs and
`/`-commands never reach the prefilter.

Why unanimity instead of a score threshold: measured on the 136-case
corpus (HANDOFF-STAGE0.md §9), the 12-way cosine margin distribution is
flat (median top1−top2 = 0.04), so score-based routing either routes
nothing or misroutes. Unanimity converts the decision to a countable,
interpretable event — and mixed neighborhoods are exactly the ambiguous
inputs the LLM chain should see.

## Modes: direct vs assert_only

`assert_only: true` runs the prefilter on every turn but NEVER routes —
the verdict is logged (`prefilter assert (not routing; assert_only)`) and
the LLM chain always runs. This accumulates real-traffic
agreement/disagreement data with zero routing risk. It is the intended
FIRST production mode: enable it, watch the logs, flip to direct routing
only when observed precision justifies it.

Both modes require the embed server; assert_only still costs one embed
call per turn (~0.3s, no tokens).

## Configuration (`~/.meept/meept.json5`)

```json5
{
  orchestrator: {
    classifier_prefilter: {
      enabled: true,
      assert_only: true,                       // start here; flip after live data
      base_url: "http://127.0.0.1:8090/v1",    // OpenAI-compatible embeddings
      model: "qwen3-embedding",                // id echoed to the endpoint
      dimension: 1024,                         // Qwen3-Embedding-0.6B; 0 = no check
      threshold: 0.70,                         // neighbor cosine floor; 0 = default
      centroids_path: "",                      // "" = ~/.meept/classifier_prefilter_centroids.json
      timeout_seconds: 2,                      // per-embed call bound
    },
  },
}
```

Default is DISABLED with `assert_only: false`. `enabled: true` with an
empty `base_url` constructs nothing (inert). The daemon needs no restart
after rebuilding the index: it loads lazily on first match; `Reload()`
exists for eval sweeps.

## Serving the embedding model

`scripts/embed_server.py` serves the 4-bit DWQ Qwen3-Embedding weights
already on disk (no downloads, no ollama) behind the standard
`/v1/embeddings` shape, using the same MLX stack as the daemon's :18081/
:18082 model servers (homebrew python3.12):

```bash
/opt/homebrew/bin/python3.12 scripts/embed_server.py \
    --model /Volumes/LLMs/Qwen3-Embedding-0.6B-4bit-DWQ --port 8090
```

Pooling is last-token (EOS) with L2 normalization — the reference
Qwen3-Embedding recipe.

## Building the example index

```bash
# build the kNN index from the labeled corpus (136 examples, 12 intents)
python3 scripts/build_prefilter_centroids.py \
    --corpus testdata/eval/classifier-test-corpus.json5 \
    --url http://127.0.0.1:8090/v1 \
    --instruction "Given a user message, classify its intent: ..."   # optional; must match serving

# held-out sweep: LOO kNN-unanimity coverage/precision vs floor
python3 scripts/build_prefilter_centroids.py --sweep
```

The store is per-example vectors (multi-modal intents keep their
clusters — no averaging). It records model, dimension, instruction,
corpus path, and build time; the Go side refuses to match on dimension
mismatch and logs to rebuild. Legacy centroid-format stores still load
(each centroid = one pseudo-example).

## Measured numbers (0.6B-4bit embedder, 136-case corpus, LOO)

| k | vote | precision when routing | coverage |
|---|---|---|---|
| 5 | unanimity | 100% | 14.7% |
| 5 | 4-of-5 | 91% | 49% |
| 4 | 4-of-4 | 92% | 24% |
| 4 | 3-of-4 | 88% | 60% |
| 3 | 3-of-3 | 87% | 35% |
| 3 | 2-of-3 | 81% | 71% |

(Floors 0.60–0.80 change nothing — 5th-neighbor cosine median is 0.88.)
The Go gate ships k=5 unanimity: it only ever routes on perfect evidence.
Looser vote sizes are config-free constants today; changing them is a
one-line edit pending assert-mode traffic data.

## Observability

- Direct route: `prefilter direct route` (intent, agent, k,
  unanimity_floor, margin) then the standard `Dispatched request` line
  with `classification_method=embedding_prefilter`.
- Assert mode: `prefilter assert (not routing; assert_only)` — correlate
  with the subsequent chain's `Dispatched request` line for agreement
  data.
- Any fall-through logs Debug/Warn; provenance still shows whichever
  downstream classifier ran.

## Testing

```bash
go test ./internal/agent/ -run 'TestPrefilter|TestDispatcher_Prefilter'
go test ./internal/config/ -run 'ClassifierPrefilter|MeeptTemplate'
```

Unit tests cover: unanimous direct route, dissenter abstention, tilted
unanimous route, sparse-neighborhood abstention, thin-index abstention,
embed error fall-through, empty-input short-circuit, dimension mismatch
fall-through, missing-store inertness, legacy centroid store compat,
reload, dispatcher wiring (direct route, assert-only never routes,
disabled-path invariance, missing-base_url guard), json5 round-trip, and
template parse.

## Tuning workflow

1. Start the embed server; build the index.
2. Ship `enabled: true, assert_only: true`. Let real traffic accumulate.
3. Compare `prefilter assert` verdicts against the chain's
   classification in the logs → observed precision + would-be coverage.
4. If precision holds at production-worthy levels, flip
   `assert_only: false`. Otherwise try a bigger embedder
   (Qwen3-Embedding-4B) and repeat.
