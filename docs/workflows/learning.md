# LoRA Learning Pipeline

The learning pipeline captures agent research trajectories, scores them for
quality, routes them into domain-specific datasets, and produces training
data for LoRA adapter fine-tuning.

## Architecture

```
Agent Loop (internal/agent/loop.go)
    |
    | tool call + output
    v
CaptureRecorder (internal/learning/capture.go)
    |
    | raw_captures.jsonl
    v
Consolidate (internal/learning/consolidate.go)
    |
    | score + dedup + route by domain
    v
DomainDatasets (internal/learning/dataset.go)
    |
    | datasets/{domain}.jsonl
    v
Training (scripts/train_lora.py)
    |
    | PEFT/TRL LoRA fine-tuning
    v
Adapters (~/.meept/adapters/{domain}/{model}-vN/)
```

## Capture Flow

1. When both `learning.enabled` and `learning.capture.enabled` are true,
   the daemon creates a `CaptureRecorder` and wires it into each agent loop
   via `WithLearningCapture`.

2. After each successful tool call in `executeToolCalls`, the agent loop
   calls `RecordResearch(ctx, conversationID, userQuery, toolName, output)`.
   Tools not listed in `learning.capture.include_tools` are skipped.

3. `RecordResearch` classifies the domain (code, debugging, api_research,
   security, meept_internal, personal), wraps the data into a
   `ResearchTrajectory`, and appends it as JSONL to
   `~/.meept/learning/raw_captures.jsonl`. Per-tool captures omit synthesis
   (audit log only). At turn end, `RecordTrajectory` records the full
   (intent, synthesis, tool path, outcome) tuple used for training. The
   tool path is scoped to the **current turn only** (tools after the last
   user message) so multi-turn history does not pollute training metadata.
   Pure chat turns with no tool use are not written as trajectories.

4. Consolidation skips empty-synthesis entries so only full trajectories
   become domain dataset training examples.

## Directory Structure

```
~/.meept/learning/
  raw_captures.jsonl       # All captures (immutable log)
  datasets/
    code.jsonl             # Domain-filtered training data
    debugging.jsonl
    api_research.jsonl
  versions/
    code_v1.jsonl          # Snapshots for retraining
    code_v2.jsonl
    versions.json          # Version metadata
```

## Quality Judgment

`ScoreExample` in `internal/learning/judge.go` computes a heuristic quality
score in [0.0, 1.0]:

- Base score: 0.5
- Task success: +0.2
- Research was used: +0.15
- Multi-hop reasoning (>1 tool call): +0.1
- User positive feedback: +0.15
- Capped at 1.0

The consolidation pass (`Consolidate`) filters out examples below the
configured `min_quality_score` (default: 0.6).

## Deduplication

`IsDuplicate` in `internal/learning/dedup.go` uses SHA-256 on the
instruction field to prevent the same training example from being added
twice to a domain dataset.

## CLI Commands

```bash
# Process raw captures into domain datasets (score, dedup, route)
meept learning consolidate

# Snapshot a domain dataset for versioned retention
meept learning snapshot code

# Train a LoRA adapter for a domain (runs post-train hook + registry regen)
meept learning train code --model lfm2.5-8b

# Apply user feedback to a session's captures (raises/lowers quality score)
meept learning feedback <session_id> positive
meept learning feedback <session_id> negative --trajectory=ltraj-...
meept learning feedback <session_id> neutral   # clear feedback

# Train all domains at/above auto_train_threshold
meept learning auto-train
meept learning auto-train --force --model lfm2.5-1.2b

# Show learning pipeline status (raw captures, datasets, adapters)
meept learning status

# List trained adapters
meept learning list

# Show dataset statistics for a domain
meept learning dataset-stats code
```

## Config Schema

```json5
{
  "learning": {
    "enabled": true,
    "data_dir": "~/.meept/learning",
    "adapters_dir": "~/.meept/adapters",
    "capture": {
      "enabled": true,
      "include_tools": ["memory_search", "file_read", "grep", "web_search"],
      "min_quality_score": 0.6,
    },
    "training": {
      "default_model": "lfm2.5-8b",
      "auto_train_threshold": 500,
      "manual_only": true,
    },
    "retention": {
      "max_dataset_size_mb": 100,
      "keep_versions": 3,
    },
  },
}
```

### Config Fields

| Field | Description | Default |
|-------|-------------|---------|
| `learning.enabled` | Master switch (capture job + scheduled consolidate) | `true` |
| `learning.data_dir` | Directory for raw captures and datasets | `~/.meept/learning` |
| `learning.adapters_dir` | Directory for trained adapters | `~/.meept/adapters` |
| `learning.capture.enabled` | Enable/disable capture within the learning subsystem | `true` |
| `learning.capture.include_tools` | Tool names to capture (empty = all) | `["memory_search", "file_read", "grep", "web_search"]` |
| `learning.capture.min_quality_score` | Minimum quality for consolidation | `0.6` |
| `learning.training.default_model` | Default base model for training | `lfm2.5-8b` |
| `learning.training.auto_train_threshold` | Example count threshold for auto-training | `500` |
| `learning.training.manual_only` | When `true`, only hints; when `false`, consolidate auto-trains ready domains | `true` |
| `learning.retention.max_dataset_size_mb` | Max size per domain dataset | `100` |
| `learning.retention.keep_versions` | Number of dataset versions to retain | `3` |

### User feedback

`ScoreExample` applies:

- **positive** → +0.15
- **negative** → −0.2
- **neutral** → clears feedback

`meept learning feedback` rewrites `raw_captures.jsonl` and re-scores matching
rows already present in domain datasets (so feedback is not stuck behind
dedup). Re-run consolidate only if you need newly scored rows that were
previously filtered below `min_quality_score`.

### Auto-train

When `manual_only` is **false**:

- CLI `meept learning consolidate` trains each ready domain (grown past last
  successful auto-train size and ≥ threshold).
- Daemon scheduled consolidate enqueues `pending_auto_train.jsonl` and runs
  training asynchronously via `scripts/train_lora.py`.

When `manual_only` is **true** (default), consolidate only prints a train
hint. Use `meept learning auto-train` (or `train`) explicitly.

## Adapter Loading

At daemon startup, the adapter registry (`~/.meept/adapter_registry.json`)
is loaded via `internal/llm/adapter_loader.go`. `LFMLoader` validates PEFT
artifacts on disk (`adapter_config.json`, `*.safetensors`, etc.), keeps the
highest `-vN` per domain, sets a `general` (or first) fallback, and builds
an `AdapterRouter`. At chat time the last user message is domain-classified
and the matching adapter path is passed to the LLM client via `WithAdapter`
(providers without adapter support ignore it). Incomplete adapter dirs are
never selected.

## Training Scripts

Training is performed via Python scripts (not part of the Go binary):

- `scripts/train_lora.py` -- PEFT/TRL LoRA training for LFM2.5 models
  (dtype/amp matched: bf16 when supported, else fp16/fp32; passes tokenizer
  to SFTTrainer; auto-loads `config/training/lora_lfm2.5_*.yaml`)
- `scripts/generate_adapter_config.py` -- Generate adapter registry JSON
  (`--adapters-dir`, `--output`, `--datasets-dir` for custom paths)
- `scripts/train_all_adapters.sh` -- Batch train all domains (auto-versions
  as `{model}-vN`; respects `MEEPT_ADAPTERS_DIR` / `MEEPT_DATASETS_DIR`)
- `hooks/on_adapter_trained.sh` -- Writes `training_meta.json` and regenerates
  the registry next to the adapters parent (daemon load path)

Training configs live in `config/training/lora_lfm2.5_8b.yaml` and
`config/training/lora_lfm2.5_1.2b.yaml`. Base model IDs:

| CLI model | Hugging Face id |
|-----------|-----------------|
| `lfm2.5-8b` | `LiquidAI/LFM2.5-8B-A1B` |
| `lfm2.5-1.2b` | `LiquidAI/LFM2.5-1.2B-Instruct` |

With `manual_only: true` (default), `auto_train_threshold` is advisory
(hints only). Set `manual_only: false` to enable automatic training on
consolidate once a domain reaches the threshold.

## Dataset Versioning

`CreateSnapshot` in `internal/learning/dataverse.go` copies the current
domain dataset to a versioned file and records metadata (MD5, example count,
timestamp) in `versions/versions.json`. This enables reproducible retraining
when new base models become available.

## Packages

| Package | Purpose |
|---------|---------|
| `internal/learning` | Capture, classify, score, dedup, consolidate, version |
| `internal/config` | `LearningConfig` schema + `AdapterRegistry`/`AdapterEntry` |
| `internal/llm` | `AdapterRegistry` loader, `LFMLoader`, `AdapterRouter` |
| `cmd/meept/learning.go` | CLI commands |
