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
Adapters (~/.meept/adapters/{domain}/{model}-v1/)
```

## Capture Flow

1. When `learning.enabled` is true in config, the daemon creates a
   `CaptureRecorder` and wires it into each agent loop via
   `WithLearningCapture`.

2. After each successful tool call in `executeToolCalls`, the agent loop
   calls `RecordResearch(ctx, conversationID, "", toolName, output)`.

3. `RecordResearch` classifies the domain (code, debugging, api_research),
   wraps the data into a `ResearchTrajectory`, and appends it as JSONL to
   `~/.meept/learning/raw_captures.jsonl`.

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
# Train a LoRA adapter for a domain
meept learning train code --model lfm2.5-8b

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
    "enabled": false,
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
| `learning.enabled` | Master switch for learning capture | `false` |
| `learning.data_dir` | Directory for raw captures and datasets | `~/.meept/learning` |
| `learning.adapters_dir` | Directory for trained adapters | `~/.meept/adapters` |
| `learning.capture.enabled` | Enable/disable capture within the learning subsystem | `true` |
| `learning.capture.include_tools` | Tool names to capture (empty = all) | `["memory_search", "file_read", "grep", "web_search"]` |
| `learning.capture.min_quality_score` | Minimum quality for consolidation | `0.6` |
| `learning.training.default_model` | Default base model for training | `lfm2.5-8b` |
| `learning.training.auto_train_threshold` | Example count threshold for auto-training | `500` |
| `learning.training.manual_only` | Disable auto-training entirely | `true` |
| `learning.retention.max_dataset_size_mb` | Max size per domain dataset | `100` |
| `learning.retention.keep_versions` | Number of dataset versions to retain | `3` |

## Adapter Loading

At daemon startup, adapters are loaded from `~/.meept/adapters/` via
`internal/llm/adapter_loader.go`. The `AdapterRouter` in
`internal/llm/adapter_router.go` selects the appropriate adapter by domain
at inference time, falling back to a general adapter or base model.

## Training Scripts

Training is performed via Python scripts (not part of the Go binary):

- `scripts/train_lora.py` -- PEFT/TRL LoRA training for LFM2.5 models
- `scripts/generate_adapter_config.py` -- Generate adapter registry JSON
- `scripts/train_all_adapters.sh` -- Batch train all domains

Training configs live in `config/training/lora_lfm2.5_8b.yaml` and
`config/training/lora_lfm2.5_1.2b.yaml`.

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
