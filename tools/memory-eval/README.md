# memory-eval

Grades how well local models perform meept's memory extraction work:

- **ambient** — epistemic extraction (claims / decisions / predictions from
  conversation), driving the same prompt and JSON shape as production's
  `AmbientExtractor` (`internal/memory/epistemic_ambient.go`).
- **distill** — lesson distillation (source observation → principle JSON),
  matching production's `DistillSummarizer` contract
  (`internal/memory/distill.go`).

No grammar constraint is applied by default: the eval grades the model's
native ability to emit parseable, schema-conformant JSON at temperature 0.2.
Pass `--grammar-file` to measure the constrained path — production now
attaches `llm.LessonGrammar()` to distill calls on local endpoints
(`internal/memory/distill.go`), mirroring the ambient candidate grammar.

## Corpus

`corpus.json` holds 64 synthetic segments with gold labels:
61 claims, 11 decisions, 9 predictions, 17 lessons (across 15 lesson
segments), 29 decoys, plus 4 dedupe
pairs (segments restating earlier claims in different words) and 4
corrections/contradictions. Categories: baseline, multi-claim, decoy-heavy
(hypotheticals, jokes, quoted opinions, pleasantries that must NOT be
extracted), prediction, decision, contradiction, dedupe pairs, lessons.

The lesson segments (seg-050..seg-064) vary the observation style the
distiller must handle: explicit rule statements, war stories that imply the
lesson, multi-sentence observations requiring abstraction, principles with
concrete numbers (timeouts, thresholds, rate limits), and negative lessons
("never do X because Z"). Some transcripts reference memory IDs ("memory
104", "memory 205") so evidence_ids may surface as ints or strings —
production's `DecodeLesson` coerces both (commit b4ea06d7).

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

The 8B "instruct" comparison resolves as: LFM2.5-8B-A1B is the instruct variant
(Liquid ships it alongside a separate Base model), so candidates 1 and 2 are the
same model in GGUF-Q4_K_M vs MLX-4bit formats.

## Judge lane (--judge, 2026-09-17)

The lexical grader (token overlap ≥ 0.6) penalizes correct-but-abstracted
extractions, so measured recall understates true quality. `--judge` adds a
second opinion: every candidate with NO lexical match is sent to the model as

> Does candidate C express the same assertion as gold G? Answer only yes or no.

with a strict yes/no system prompt, temperature 0, max_tokens 4, and NO
grammar. Judgments are cached per (candidate-hash, gold-hash) pair; the scan
is per unmatched candidate with early exit on the first "yes".

```bash
go run ./tools/memory-eval --endpoint http://127.0.0.1:8080/v1 \
  --model <model-id> --mode ambient --judge \
  --out tools/memory-eval/results/judge-judge.json

# optional: a different endpoint for the judge lane
go run ./tools/memory-eval --judge --judge-endpoint http://127.0.0.1:8090/v1 ...
```

The summary prints a parallel `judge:` block (`judge_matched`,
`judge_precision`, `judge_recall`, `judge_f1`) next to the lexical one, and
the metrics JSON carries it as `"judge": {...}` inside the ambient object.

### Self-judge caveat

By default the judge is **the same model on the same endpoint** that produced
the candidates — a self-judge. A model family grading its own outputs
systematically over-accepts paraphrases of its own phrasing, so judge numbers
are an **upper-bound estimate** of semantic matching quality, not a
measurement of it. The lexical number stays the reproducible regression gate;
use the judge lane to estimate how much recall the lexical gate is leaving on
the table, not as the shipping metric. (--judge-endpoint allows a different
judge model, but the bias direction is unchanged unless the judge is strictly
stronger than the extractor.)

### Confidence sweep

Both judge-on and judge-off ambient runs record every candidate into
`confidence_analysis` (`{confidence, matched_lexical, matched_judge}`) and
sweep thresholds 0.3..0.9 into `confidence_sweep`, using JUDGE matching:

| column | meaning |
|--------|---------|
| prec@t | TP among candidates with confidence ≥ t / candidates ≥ t |
| kept_frac | fraction of candidates kept at the threshold |
| precision / recall | the gate's full operating point |

This is the input to confidence-gated storage: keep the highest threshold
whose recall loss is acceptable, and gate writes on it.

## Results (2026-09-16 run, unconstrained, temp 0.2)

| Metric | LFM2-1.2B-Extract Q4 | LFM2.5-8B-A1B Q4 GGUF | LFM2.5-8B-A1B MLX-4bit |
|--------|----------------------|-----------------------|------------------------|
| ambient precision | 0.158 | 0.174 | 0.143 |
| ambient recall    | 0.547 | 0.532 | 0.483 |
| ambient F1        | 0.245 | 0.262 | 0.220 |
| json_parse_ok     | 36/51 | 40/51 | 39/51 |
| schema_conformant | 19/51 | 21/51 | 19/51 |
| decoy false pos   | 49    | 53    | 51 |
| distill recall    | 0.000* | 1.000 | 0.800 |
| distill parse     | 0/5*  | 5/5   | 5/5 |
| mean latency ms   | 1638  | 1412  | 1532 |

*first run predated a harness fix for integer evidence_ids.

## Ambient prompt iteration (2026-09-17, constrained, LFM2.5-8B Q4 on :8080)

Baseline after the grammar/slot fixes (`/tmp/ambient-post-slot.json`): precision
0.576, recall 0.605, F1 0.590, decoy FP 27, parse 51/51, conformance 51/51.
27 of 36 FPs were decoy-segment extractions (assistant restatements treated as
new claims, paraphrase fragments). The prompt was iterated against the fixed
harness; every variant below keeps the same grammar and key set.

| Variant | Prompt change | precision | recall | F1 | decoy FP | parse | conform |
|---------|--------------|-----------|--------|----|----------|-------|---------|
| baseline | post-slot-fix prompt | 0.576 | 0.605 | 0.590 | 27 | 51/51 | 51/51 |
| V1 (SHIPPED) | + anti-restatement & no-fragmentation rules | **0.708** | 0.568 | **0.630** | **13** | 51/51 | 51/51 |
| V2 | (not run — V1 cleared skip gate: precision ≥ 0.70 with recall ≥ 0.55) | | | | | | |
| V3 | (not run — skipped) | | | | | | |

Winner: **V1**. Decoy FPs halved (27→13), precision +0.13, recall drop 0.037
(inside the 0.05 budget). Applied identically to
`internal/memory/epistemic_ambient.go` (`ambientExtractionPromptTemplate`) and
the harness mirror (`ambientPrompt` in `grade.go`); sync is guarded by
`TestAmbientExtractionPromptMatchesEvalHarness`.

## Findings

1. **None of the three is production-usable UNCONSTRAINED for ambient
   extraction.** All three fragment assertions ("Go", "1200 requests per
   second" instead of full statements), all three wrap the array in
   `{"candidates": [...]}` or paraphrase into fragments, and all three treat
   ~50 of 29 decoys as extractable. Precision ~0.15 means 85% of what would be
   stored is noise.
2. **The `{"candidates": ...}` wrapper is a production risk**: production's
   `ParseAmbientCandidates` expects a bare array. Constrained decoding (GBNF
   forcing a bare array root) is mandatory before any of these models ships in
   the ambient path.
3. **Distillation is nearly solved**: the 8B (both formats) parses 5/5 and
   recalls 1.0/0.8 on lessons. The only schema miss is inventing INTEGER
   evidence_ids where production requires []string — production's
   `DecodeLesson` would reject these. Either the prompt should say "omit
   evidence_ids" or production should tolerate/coerce ints.
4. **8B vs 1.2B**: the 8B is marginally better on F1 and clearly better on
   distill, but not enough to justify a second resident model — the deciding
   factor is that ALL ambient results need grammar constraints regardless of
   model. A single shared model + GBNF is the recommended path; the Extract
   1.2B offers no advantage here over the 8B you already run.
5. GGUF Q4 vs MLX 4bit: statistically a wash on quality; latency similar. Keep
   whichever serves your existing stack (GGUF for llama.cpp lifecycle wiring).

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
| recall | of gold labels, fraction the model found (segments/lessons whose call failed or produced unparseable output count as misses) |
| f1 | harmonic mean |
| decoy_false_pos | extractions from decoy-only segments (jokes/hypotheticals treated as fact) |
| json_parse_ok | calls whose raw output parsed as JSON |
| schema_conformant | calls whose JSON matched the expected shape (types/categories/no empty text) |
| cap_violations | distilled principles over the 280-char production cap |
| mean_latency_ms | per-call latency |
| tokens_used | total prompt+completion tokens |

Watch first: **decoy_false_pos** (does the model know what NOT to store),
**json_parse_ok** (will it work in production without a grammar), then F1.

## 2026-09-17 live runs: PERFORMED (parent session, post-prompt-V1 grammar runs)

Live numbers, LFM2.5-8B-A1B Q4 on the daemon's :8080, --grammar-file
/tmp/ambient.gbnf, anti-restatement prompt (commit 9ea86850):

| lane | precision | recall | F1 | decoy FP |
|------|-----------|--------|----|----------|
| lexical | 0.649 | 0.580 | 0.657 | 13 |
| judge | 0.691 | 0.580 | 0.631 | 17 |
| judge-rescued totals | 0.735 | 0.617 | 0.671 | — |

Judge rescued 3 semantic matches the lexical gate missed (7 judge calls,
cached). Note: run-to-run variance at this corpus size is ±0.05-0.08 on
precision (temperature 0.2 sampling); compare lanes within a run, not
across runs.

Confidence sweep (judge matching): CORRECTED after distribution analysis.
Confidence is bimodal (46 candidates at 1.0, 19 at 0.0, 3 scatter) — the
flat 0.3-0.9 region masked the real signal, which lives between 0.0 and
0.3: gating at t=0.3 lifts judge precision 0.735 -> 0.918 (+0.18) by
excluding the 0.0-confidence band, which holds most decoy FPs. The
excluded band is NOT garbage: 7 of 21 low-confidence candidates are
correct (33%), so a hard drop loses real memory. Recommended wiring:
confidence < 0.3 -> auto-reject as untrusted (they are overwhelmingly
decoy activations); 0.3 <= conf < 1.0 partial band -> librarian review
pipeline (status=auto claims already have this surface); conf = 1.0 ->
auto-store. Re-calibrate the 0.3 cut on live traffic via the outcome loop
(#49 measurement pattern) before shipping.

Earlier stub-only note superseded: the shape-verification numbers in the
previous revision of this section were stub output and carried no quality
signal.

```bash
# (a) lexical baseline
go run ./tools/memory-eval --endpoint http://127.0.0.1:8080/v1 \
  --model <model-id> --mode ambient \
  --out tools/memory-eval/results/judge-lexical.json
# (b) judge lane
go run ./tools/memory-eval --endpoint http://127.0.0.1:8080/v1 \
  --model <model-id> --mode ambient --judge \
  --out tools/memory-eval/results/judge-judge.json
```

Record the resulting P/R/F1 for both lanes plus the sweep table here.
