# Classifier Stage-0 Embedding Prefilter

Stage-0 of `ClassifyAndRoute` (classifier-observability follow-up,
`docs/plans/classifier-observability/HANDOFF.md` §3): an embedding-based
gate that direct-routes confident inputs BEFORE the intent analyzer and
routing classifier run — skipping two serialized LLM calls (~0.3s embed +
cosine vs 0.3–28s analyzer + up to 15s router).

## How it works

```
user input
   │
   ▼
[Stage 0: EmbeddingPrefilter.Match]
   embed input (OpenAI-compatible /v1/embeddings, local server)
   cosine vs per-intent centroids (built from the labeled corpus)
   best score >= threshold AND margin over runner-up >= 0.05
   │ yes → route DIRECTLY (method="embedding_prefilter")
   │        analyzer + router never run
   ▼ no
[existing chain unchanged]
   analyzer (ambiguity gate) → router (llm→keyword→semantic→heuristic)
```

The prefilter can only SKIP work, never degrade routing: any miss, embed
error, timeout, dimension mismatch, or missing centroid store returns nil
and the LLM chain runs exactly as before. Compound-signal inputs and
`/`-commands never reach the prefilter.

Two guards prevent wrong-but-confident routes:
- **threshold** (default 0.90): minimum cosine to route directly.
- **margin** (0.05, fixed): best score must beat the runner-up by this
  much — a tie is exactly the ambiguity Stage-0 must not guess on.

## Configuration (`~/.meept/meept.json5`)

```json5
{
  orchestrator: {
    classifier_prefilter: {
      enabled: true,
      base_url: "http://127.0.0.1:8090/v1",   // OpenAI-compatible embeddings
      model: "qwen3-embedding",                // id echoed to the endpoint
      dimension: 1024,                         // Qwen3-Embedding-0.6B; 0 = no check
      threshold: 0.90,                         // 0 = built-in default
      centroids_path: "",                      // "" = ~/.meept/classifier_prefilter_centroids.json
      timeout_seconds: 2,                      // per-embed call bound
    },
  },
}
```

Default is DISABLED. `enabled: true` with an empty `base_url` constructs
nothing (inert). The daemon needs no restart after rebuilding centroids:
the store loads lazily on first match; delete the store (or point the path
elsewhere) to disable at runtime.

## Serving the embedding model

`scripts/embed_server.py` serves the 4-bit DWQ Qwen3-Embedding weights
already on disk (no downloads, no ollama) behind the standard
`/v1/embeddings` shape, using the same MLX stack as the daemon's :8082
model server:

```bash
~/.venv/bin/python scripts/embed_server.py \
    --model /Volumes/LLMs/Qwen3-Embedding-0.6B-4bit-DWQ --port 8090
```

Pooling is last-token (EOS) with L2 normalization — the reference
Qwen3-Embedding recipe.

## Building centroids

```bash
# one-shot build from the labeled corpus (136 cases, 13 intent categories)
python3 scripts/build_prefilter_centroids.py \
    --corpus testdata/eval/classifier-test-corpus.json5 \
    --url http://127.0.0.1:8090/v1

# threshold sweep: coverage/accuracy table to pick τ before building
python3 scripts/build_prefilter_centroids.py --sweep
```

The store records model, dimension, corpus path, and build time, so drift
between the serving model and the centroids is detectable (the Go side
refuses to match on dimension mismatch and logs to rebuild).

## Observability

- Every direct route logs `prefilter direct route` (intent, agent, score,
  runner-up) and the standard `Dispatched request` line carries
  `classification_method=embedding_prefilter`.
- Provenance follows the existing classifier-observability contract:
  direct routes surface through `Intent.Method` like every other branch.
- Fall-throughs (below threshold, embed failure) log at Debug/Warn and the
  turn's provenance shows whichever downstream classifier ran.

## Testing

```bash
go test ./internal/agent/ -run 'TestPrefilter|TestDispatcher_Prefilter'
go test ./internal/config/ -run 'ClassifierPrefilter'
```

Unit tests cover: direct route, below-threshold fall-through, margin tie
rejection, embed error fall-through, empty-input short-circuit, dimension
mismatch fall-through, missing-store inertness, reload, dispatcher wiring
(direct route + disabled-path invariance + missing-base_url guard), and
json5 config round-trip.

## Tuning workflow

1. Start the embed server.
2. Run `--sweep`: pick the τ with the best correct-route coverage
   (self-inclusive upper bound — the honest number comes from the live
   A/B against real traffic).
3. Build centroids at that τ; set `threshold` in config.
4. Watch `prefilter direct route` vs fall-through rates in the daemon log.
