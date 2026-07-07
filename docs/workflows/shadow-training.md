# Shadow Training

## Overview
Meept's shadow training system captures production LLM traffic, scores student responses against a teacher model, and produces LoRA training pairs that improve the student over time. Trained adapters are gated by an eval threshold and hot-swapped into the serving alias without restart.

## Architecture

```
User request
    |
    v
LLM Client (AdapterAwareChatter)
    |- serves via current alias model
    |
    v
Shadow Middleware (internal/shadow/middleware.go)
    |- intercepts every Chat() call
    |- async fetches teacher response
    |- scores (heuristic / teacher_eval / hybrid)
    |
    v
Shadow Store (training.db)
    |- ShadowRecord (student vs teacher)
    |- PreferencePair (DPO pair)
    |- FewShotExample (high-quality for in-context learning)
    |
    v  [scheduled: train_threshold pairs reached]
LoRA Trainer (internal/shadow/trainer.go)
    |- exports DPO JSONL
    |- runs Unsloth/Axolotl/TRL/LLaMA-Factory
    |
    v
Eval Gate (internal/shadow/eval_gate.go)
    |- TrainingRun.EvalScore >= EvalThreshold (default 0.7)
    |- TrainingRun.RecordsUsed >= 20
    |
    v
Hot Swap (internal/shadow/adapter_hotswap.go)
    |- Ollama: bakes adapter into a new model variant
    |- LLM client: receives HotSwapCallback with the baked model ID
    |- DB: SetActiveAdapter flips the flag
```

## Configuration

```toml
[shadow]
enabled = true
data_dir = "~/.meept/shadow"

[shadow.teacher]
model = "anthropic/claude-haiku-4-5"
fallback_model = "openai/gpt-5-mini"
max_daily_queries = 500
max_daily_cost = 10.0
requests_per_minute = 30

[shadow.adapters]
enabled = true
hot_swap_enabled = true
eval_threshold = 0.7
ollama_endpoint = "http://localhost:11434"
auto_train = true
train_threshold = 500
```

## CLI

```bash
meept shadow stats                  # capture counts, training pairs, daily cost
meept shadow adapters list          # trained adapters with eval scores
meept shadow adapters activate <id> # activate (subject to eval gate)
meept shadow adapters hotswap <id>  # activate + push to LLM client
```

## Observability

- Per-day teacher query count and cost in the metrics store.
- `TrainingRun.FinalLoss` and `TrainingRun.EvalScore` per training pass.
- Adapter activation events emit on the message bus (E4-class event).

## Safety

- **Eval gate:** adapters scoring below `eval_threshold` cannot be activated.
- **Record floor:** adapters trained on fewer than 20 records are blocked even if eval score passes.
- **Hot-swap is opt-in:** `hot_swap_enabled = false` makes `ActivateAdapter` flip a DB flag without touching the serving path.
- **Cost ceiling:** teacher queries are rate-limited per minute and per day (USD).
