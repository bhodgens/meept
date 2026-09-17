# memory-eval

Grades how well local models perform meept's memory extraction work:

- **ambient** — epistemic extraction (claims / decisions / predictions from
  conversation), driving the same prompt and JSON shape as production's
  `AmbientExtractor` (`internal/memory/epistemic_ambient.go`).
- **distill** — lesson distillation (source observation → principle JSON),
  matching production's `DistillSummarizer` contract
  (`internal/memory/distill.go`).

No grammar constraint is applied: the eval grades the model's native ability
to emit parseable, schema-conformant JSON at temperature 0.2 — the same
conditions production runs under today.

## Corpus

`corpus.json` holds 53 synthetic segments with gold labels:
61 claims, 11 decisions, 9 predictions, 5 lessons, 29 decoys, plus 4 dedupe
pairs (segments restating earlier claims in different words) and 4
corrections/contradictions. Categories: baseline, multi-claim, decoy-heavy
(hypotheticals, jokes, quoted opinions, pleasantries that must NOT be
extracted), prediction, decision, contradiction, dedupe pairs, lessons.

Validate without an endpoint:

```bash
go run ./tools/memory-eval --validate-corpus
```

## Running

Each candidate gets the endpoint to itself on port 8090 (one model loaded at
a time — the point is to compare memory footprint too).

### Candidate 1: LFM2.5-8B-A1B Q4_K_M (GGUF)

```bash
~/.meept/deps/llama.cpp/build/bin/llama-server \
  --model /Volumes/LLMs/LiquidAI/LFM2.5-8B-A1B-GGUF/LFM2.5-8B-A1B-Q4_K_M.gguf \
  --port 8090 --host 127.0.0.1 --jinja --reasoning-budget 0 --ctx-size 8192
```

`--jinja` is required so the chat template renders; `--reasoning-budget 0`
stops the LFM2.5 thinking spiral from eating the token budget.

### Candidate 2: LFM2.5-8B-A1B MLX-4bit

MLX serves the same OpenAI shim, so the eval binary is unchanged:

```bash
~/.local/share/uv/tools/... # or your mlx env:
python -m mlx_lm.server --model /Volumes/LLMs/LiquidAI/LFM2.5-8B-A1B-MLX-4bit \
  --port 8090
```

Note: mlx_lm.server ignores `--jinja`/`--reasoning-budget`; the thinking
behavior (if any) is part of what's being measured for this candidate.

### Candidate 3: LFM2-1.2B-Extract Q4_K_M (GGUF)

```bash
~/.meept/deps/llama.cpp/build/bin/llama-server \
  --model /Volumes/LLMs/LiquidAI/LFM2-1.2B-Extract-GGUF/LFM2-1.2B-Extract-Q4_K_M.gguf \
  --port 8090 --host 127.0.0.1 --ctx-size 8192
```

The Extract model has no chat template thinking mode; `--jinja` is harmless
either way.

### Evaluate

```bash
go run ./tools/memory-eval \
  --endpoint http://127.0.0.1:8090/v1 \
  --model /Volumes/LLMs/LiquidAI/LFM2.5-8B-A1B-GGUF/LFM2.5-8B-A1B-Q4_K_M.gguf \
  --out /tmp/memory-eval-lfm25-8b-q4.json
```

`--model` must be the exact id the endpoint reports (llama-server uses the
model path; mlx_lm.server uses the directory name).

Stop the server, start the next candidate, repeat. Compare the three JSON
outputs.

## Metrics

| Metric | Meaning |
|--------|---------|
| precision | of extracted items, fraction matching a gold label (token overlap ≥ threshold, default 0.6) |
| recall | of gold labels, fraction the model found |
| f1 | harmonic mean |
| decoy_false_pos | extractions from decoy-only segments (jokes/hypotheticals treated as fact) |
| json_parse_ok | calls whose raw output parsed as JSON |
| schema_conformant | calls whose JSON matched the expected shape (types/categories/no empty text) |
| cap_violations | distilled principles over the 280-char production cap |
| mean_latency_ms | per-call latency |
| tokens_used | total prompt+completion tokens |

Watch first: **decoy_false_pos** (does the model know what NOT to store),
**json_parse_ok** (will it work in production without a grammar), then F1.
