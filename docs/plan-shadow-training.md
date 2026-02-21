# Shadow Training System for Meept

## Overview

Implement a "Shadow Training" system that enables a smarter "teacher" model to shadow/observe other models and use those interactions for training improvement. The system supports three tiers of improvement:

1. **Tier 1 (API-Compatible)**: In-context learning with dynamic few-shot example selection - works with any provider
2. **Tier 2 (Local Models)**: LoRA/DPO fine-tuning with Ollama
3. **Tier 3 (Provider-Specific)**: OpenAI/Anthropic fine-tuning APIs

## Architecture

```
User Request
    │
    v
┌──────────────────────────────────────────────────────────┐
│ Agent Loop (internal/agent/loop.go)                      │
│   │                                                       │
│   ├─► Example Selector ──► Inject few-shot examples      │
│   │   (from fewshot_examples table)                       │
│   │                                                       │
│   └─► LLM Client ──► Shadow Middleware ──► Capture       │
└──────────────────────────────────────────────────────────┘
                          │
                          v
┌──────────────────────────────────────────────────────────┐
│ Shadow Manager                                           │
│   │                                                       │
│   ├─► Teacher Model ──► Get teacher response             │
│   ├─► Scorer ──► Quality assessment                      │
│   ├─► Store ──► SQLite persistence                       │
│   └─► Exporter ──► Training data export                  │
└──────────────────────────────────────────────────────────┘
                          │
                          v
┌──────────────────────────────────────────────────────────┐
│ Training Pipeline (offline or scheduled)                 │
│   │                                                       │
│   ├─► DPO training (preference pairs)                    │
│   ├─► LoRA fine-tuning (local Ollama)                    │
│   └─► API fine-tuning (OpenAI/Anthropic)                 │
└──────────────────────────────────────────────────────────┘
```

## New Package Structure

```
internal/shadow/
    config.go          # Configuration types
    models.go          # ShadowRecord, PreferencePair, FewShotExample, etc.
    store.go           # Store interface
    store_sqlite.go    # SQLite implementation
    middleware.go      # LLM client middleware for interception
    teacher.go         # Teacher model orchestration
    scorer.go          # Quality scoring (heuristic + LLM-based)
    selector.go        # Dynamic few-shot example selection
    exporter.go        # Export to fine-tuning formats
    manager.go         # Overall orchestration

internal/shadow/adapters/
    ollama.go          # Ollama LoRA adapter management
    openai.go          # OpenAI fine-tuning API
```

## Database Architecture

Separate database files for portability and compartmentalization:

```
~/.meept/shadow/
├── training.db          # Primary: shadow_records, preference_pairs
│                        # PORTABLE - copy to train elsewhere
├── examples.db          # Tier 1: fewshot_examples (runtime cache)
│                        # Can be regenerated from training.db
├── adapters.db          # Tier 2: adapter registry (machine-local)
└── exports/
    ├── dpo_YYYYMMDD.jsonl
    └── sft_YYYYMMDD.jsonl
```

### Data Reuse Strategy

Both tiers use the **same collected data** - teacher API cost is paid once:

```
Shadow Record (training.db)
       │
       ├──► Tier 1: Extract as fewshot_examples (examples.db)
       │            Immediate in-context learning benefit
       │
       └──► Tier 2: Export as JSONL for LoRA/DPO training
                    Batch training on separate machine
```

---

## Database Schema

### training.db (Portable - Primary Training Data)

#### shadow_records
Core training data - captures student response + optional teacher response.

| Column | Type | Description |
|--------|------|-------------|
| id | TEXT PK | UUID |
| created_at | TEXT | Timestamp |
| conversation_id | TEXT | Conversation context |
| messages_json | TEXT | Input messages (JSON) |
| student_model | TEXT | Student model ID |
| student_content | TEXT | Student response |
| student_tokens_in/out | INT | Token usage |
| teacher_model | TEXT | Teacher model ID (nullable) |
| teacher_content | TEXT | Teacher response (nullable) |
| quality_score | REAL | 0.0-1.0 computed score |
| preference | TEXT | "student", "teacher", "tie" |
| domain | TEXT | "code", "general", "planning" |
| task_type | TEXT | "chat", "tool_use", "reasoning" |
| is_high_quality | INT | Flag for high-quality examples |

#### preference_pairs (training.db)
DPO training format - chosen vs rejected responses. Generated from shadow_records.

| Column | Type | Description |
|--------|------|-------------|
| id | TEXT PK | UUID |
| source_record_id | TEXT FK | Links to shadow_records |
| prompt_json | TEXT | Shared prompt context |
| chosen_response | TEXT | Preferred response |
| chosen_model | TEXT | Model that produced chosen |
| rejected_response | TEXT | Non-preferred response |
| rejected_model | TEXT | Model that produced rejected |
| margin | REAL | How much better chosen is |
| exported_at | TEXT | Track export status |

---

### examples.db (Local Cache - Regenerable)

#### fewshot_examples
High-quality examples for in-context learning. Derived from training.db.

| Column | Type | Description |
|--------|------|-------------|
| id | TEXT PK | UUID |
| source_record_id | TEXT | Original shadow_record |
| domain | TEXT | Domain classification |
| task_type | TEXT | Task classification |
| user_message | TEXT | Example input |
| assistant_response | TEXT | Example output (best of student/teacher) |
| quality_score | REAL | Quality threshold filter |
| usage_count | INT | Track retrieval frequency |
| embedding_json | TEXT | Optional: vector for similarity search |

Can be regenerated: `./bin/meept shadow examples rebuild`

---

### adapters.db (Machine-Local)

#### adapters
Registry of trained LoRA/soft-prompt adapters. Machine-specific paths.

| Column | Type | Description |
|--------|------|-------------|
| id | TEXT PK | UUID |
| name | TEXT | Human-readable name |
| model_base | TEXT | Base model this adapts |
| adapter_type | TEXT | "lora", "soft_prompt" |
| adapter_path | TEXT | Local path to weights |
| source_training_db | TEXT | Which training.db was used |
| training_records | INT | How many records trained on |
| is_active | INT | Currently loaded? |

#### training_runs
Track training history for reproducibility.

| Column | Type | Description |
|--------|------|-------------|
| id | TEXT PK | UUID |
| adapter_id | TEXT FK | Links to adapters |
| started_at | TEXT | Training start time |
| completed_at | TEXT | Training end time |
| records_used | INT | Number of training examples |
| config_json | TEXT | Hyperparameters used |
| final_loss | REAL | Training loss |
| eval_score | REAL | Evaluation score |

## Shadowing Modes

### 1. Synchronous (default for high-value queries)
- Wait for both student and teacher responses
- Score immediately
- Higher latency but immediate feedback

### 2. Asynchronous (default for general use)
- Return student response immediately
- Queue teacher shadowing for background processing
- No latency impact

### 3. Selective (cost optimization)
- Only shadow based on criteria:
  - Complexity threshold (moderate+)
  - Specific domains (code, planning)
  - Sample rate (e.g., 50%)

## Quality Scoring Methods

### Heuristic Scoring (fast, cheap)
- Response length adequacy
- Tool call correctness
- Relevance keywords
- Format adherence

### Teacher Evaluation (accurate, costly)
- Teacher model rates student response
- Dimensions: correctness, completeness, style, safety
- Returns structured score

### Hybrid (recommended)
- Heuristic pre-filter
- Teacher evaluation for borderline cases

## Integration Points

### 1. LLM Client Middleware (`internal/llm/client.go`)
Wrap the `Chat()` method (lines 110-186) to capture requests/responses:

```go
// Before: direct client call
resp, err := l.llm.Chat(ctx, messages, chatOpts...)

// After: middleware-wrapped
shadowClient := shadow.NewMiddleware(l.llm, shadowManager, config)
resp, err := shadowClient.Chat(ctx, messages, chatOpts...)
```

### 2. Agent Loop Example Injection (`internal/agent/loop.go`)
Inject few-shot examples before LLM call in `reasoningCycle()` (line 254):

```go
// Get relevant examples
examples, _ := shadowManager.GetFewShotExamples(ctx, domain, taskType, query, 3)

// Inject into messages after system prompt
messages = injectFewShotExamples(messages, examples)
```

### 3. Daemon Initialization (`internal/daemon/components.go`)
Add shadow manager to daemon startup (after line 111).

## Configuration

Add to `~/.meept/meept.toml`:

```toml
#==============================================================================
# SHADOW TRAINING CONFIGURATION
#==============================================================================
# Enables model improvement through teacher shadowing and training data collection.
# Data collected here serves BOTH Tier 1 (in-context learning) and Tier 2 (LoRA/DPO).

[shadow]
# Master switch - set to true to enable shadow training
enabled = true

# Base directory for all shadow training data
# Contains: training.db, examples.db, adapters.db, exports/
data_dir = "~/.meept/shadow"

#------------------------------------------------------------------------------
# SHADOWING - Controls when and how student responses are shadowed by teacher
#------------------------------------------------------------------------------
[shadow.shadowing]
# Shadowing mode:
#   "async"     - Return student response immediately, shadow in background (recommended)
#   "sync"      - Wait for both student and teacher responses (higher latency)
#   "selective" - Only shadow based on criteria below (cost optimization)
mode = "async"

# Selective mode filters (only apply when mode = "selective")
# Complexity threshold: "simple", "moderate", "complex"
# Only shadow queries at or above this complexity
min_complexity = "moderate"

# Only shadow specific domains (empty = all domains)
# Options: "code", "general", "planning", "debugging", "analysis"
domains = []

# Only shadow specific task types (empty = all types)
# Options: "chat", "tool_use", "reasoning", "multi_step"
task_types = []

# Random sampling rate (0.0 - 1.0)
# 0.5 = shadow 50% of eligible requests
sample_rate = 0.5

# Background queue settings (for async mode)
queue_size = 1000
worker_count = 2

#------------------------------------------------------------------------------
# TEACHER MODEL - The "smarter" model that shadows student responses
#------------------------------------------------------------------------------
[shadow.teacher]
# Primary teacher model (provider/model-id format)
# REQUIRED: No default - must be explicitly configured
# Examples:
#   "anthropic/claude-opus-4-5-20251101"
#   "openai/gpt-4o"
#   "ollama/llama3.3-70b"
model = ""

# Fallback teacher if primary is unavailable or rate-limited
fallback_model = ""

# Teacher generation settings
temperature = 0.0           # Use 0 for deterministic/consistent responses
max_tokens = 4096           # Match or exceed student model's max
timeout_seconds = 120       # Timeout for teacher API calls

# Cost control - prevent runaway teacher costs
max_daily_queries = 500     # Max teacher calls per day (0 = unlimited)
max_daily_cost = 10.0       # Max teacher API cost per day in USD (0 = unlimited)

# Rate limiting
requests_per_minute = 30    # Limit teacher API calls per minute

#------------------------------------------------------------------------------
# QUALITY SCORING - How to evaluate and compare responses
#------------------------------------------------------------------------------
[shadow.quality]
# Scoring method:
#   "heuristic"    - Fast, cheap, uses pattern matching (no API cost)
#   "teacher_eval" - Teacher model evaluates student response (API cost)
#   "hybrid"       - Heuristic first, teacher for borderline cases (recommended)
method = "hybrid"

# Quality thresholds (0.0 - 1.0)
# Responses above this are marked as high-quality examples
high_quality_threshold = 0.85

# Responses above this are kept for potential training
# Below this threshold, responses may be discarded
trainable_threshold = 0.6

# Preference margin - how much better one response must be to establish preference
# Used when generating DPO training pairs
preference_margin = 0.1

# Heuristic scoring weights (must sum to 1.0)
[shadow.quality.heuristic_weights]
relevance = 0.30            # Does response address the query?
completeness = 0.25         # Is the response thorough?
correctness = 0.35          # Is the content accurate? (tool calls, code, facts)
style = 0.10                # Formatting, clarity, tone

# Custom evaluation prompt (for teacher_eval method)
# Leave empty to use default prompt
eval_prompt_template = ""

#------------------------------------------------------------------------------
# TIER 1: IN-CONTEXT LEARNING - Few-shot example injection
#------------------------------------------------------------------------------
[shadow.examples]
# Enable Tier 1 in-context learning
enabled = true

# Maximum examples to store per domain/task_type combination
# Older low-scoring examples are pruned when limit is reached
max_per_category = 100

# Minimum quality score for an example to be stored
# Higher = fewer but better examples
min_quality = 0.8

# How many examples to inject into prompts by default
default_count = 3

# Maximum examples to inject (prevents context overflow)
max_count = 5

# Example selection weights (must sum to 1.0)
similarity_weight = 0.7     # How similar is the query to stored examples?
recency_weight = 0.2        # Prefer recent examples?
quality_weight = 0.1        # Prefer higher-quality examples?

# Context budget - max tokens to use for injected examples
# Prevents examples from consuming too much context
max_context_tokens = 2000

#------------------------------------------------------------------------------
# EXPORT - Training data export for external training pipelines
#------------------------------------------------------------------------------
[shadow.export]
# Output directory for exported training data
output_dir = "~/.meept/shadow/exports"

# Default export formats
# Options: "jsonl" (general), "dpo" (preference learning),
#          "openai" (OpenAI fine-tuning), "alpaca" (instruction format)
formats = ["jsonl", "dpo"]

# Minimum records before allowing export
min_records = 100

# Include records below trainable_threshold in exports
include_low_quality = false

# Deduplication - remove near-duplicate training examples
deduplicate = true
dedup_similarity_threshold = 0.95

#------------------------------------------------------------------------------
# TIER 2: ADAPTER MANAGEMENT - LoRA/soft-prompt loading for local models
#------------------------------------------------------------------------------
[shadow.adapters]
# Enable Tier 2 adapter management
enabled = false

# Ollama endpoint for adapter operations
ollama_endpoint = "http://localhost:11434"

# Auto-training (requires GPU and training infrastructure)
auto_train = false

# Minimum preference pairs before auto-training triggers
train_threshold = 500

# Training schedule (cron format) - when to check for retraining
# Empty = manual training only
train_schedule = ""

# Default adapter directory
adapter_dir = "~/.meept/shadow/adapters"

# LoRA training defaults (used when auto_train = true)
[shadow.adapters.lora]
rank = 16                   # LoRA rank (8, 16, 32, 64)
alpha = 32                  # LoRA alpha (typically 2x rank)
dropout = 0.05              # Dropout rate
target_modules = ["q_proj", "v_proj", "k_proj", "o_proj"]  # Modules to adapt
learning_rate = 2e-4        # Learning rate
epochs = 3                  # Training epochs
batch_size = 4              # Batch size (reduce if OOM)
gradient_accumulation = 4   # Effective batch = batch_size * gradient_accumulation
warmup_ratio = 0.03         # Warmup steps as ratio of total
max_grad_norm = 1.0         # Gradient clipping

# DPO-specific settings (for preference-based training)
[shadow.adapters.dpo]
beta = 0.1                  # DPO beta parameter (lower = more aggressive)
loss_type = "sigmoid"       # "sigmoid" or "hinge"
```

## CLI Commands

```bash
# Status and stats
./bin/meept shadow status

# Export training data
./bin/meept shadow export --format=dpo --min-quality=0.8
./bin/meept shadow export --format=openai --since=2026-02-01

# Few-shot example management
./bin/meept shadow examples list
./bin/meept shadow examples prune --max-age=30d

# Adapter management (local models only)
./bin/meept shadow adapters list
./bin/meept shadow adapters train --base=llama3.2 --type=lora
./bin/meept shadow adapters activate <id>
```

## Export Formats

### JSONL (general purpose)
```json
{"messages": [...], "metadata": {"domain": "code", "quality": 0.92}}
```

### DPO (preference learning)
```json
{"prompt": "...", "chosen": "...", "rejected": "..."}
```

### OpenAI Fine-tuning
```json
{"messages": [{"role": "system", "content": "..."}, ...]}
```

## Portable Training Workflow

The `training.db` file is designed to be self-contained and portable:

```bash
# 1. On meept host: Export training database
cp ~/.meept/shadow/training.db /mnt/shared/training.db
# Or: ./bin/meept shadow export-db --output=/mnt/shared/

# 2. On training machine (GPU server): Import and train
# Option A: Direct export from DB
./meept-train export \
    --db=/mnt/shared/training.db \
    --format=dpo \
    --min-quality=0.8 \
    --output=dpo_data.jsonl

# Option B: Use existing tooling (Axolotl, LLaMA-Factory)
sqlite3 /mnt/shared/training.db \
    "SELECT prompt_json, chosen_response, rejected_response FROM preference_pairs" \
    > dpo_data.jsonl

# 3. Train adapter
axolotl train config.yaml  # Uses dpo_data.jsonl

# 4. Copy adapter back to meept host
scp -r ./output/adapter/ meept-host:~/.meept/shadow/adapters/v1/

# 5. Register and activate
./bin/meept shadow adapters register v1 --path=~/.meept/shadow/adapters/v1/
./bin/meept shadow adapters activate v1
```

### training.db Contents

Everything needed to reproduce training:
- Raw shadow records with full message history
- Pre-computed preference pairs
- Quality scores and metadata
- Export tracking (what was already exported)

### Incremental Training

Track what's been exported to support incremental training:

```sql
-- Only export new records since last training run
SELECT * FROM preference_pairs
WHERE exported_at IS NULL
  AND margin > 0.1;  -- Strong preference

-- Mark as exported after successful training
UPDATE preference_pairs SET exported_at = datetime('now')
WHERE id IN (...);
```

---

## Implementation Phases

### Phase 1: Core Infrastructure + Data Collection
*Shared foundation for both tiers*

1. Create `internal/shadow/` package structure
2. Implement `training.db` store (shadow_records, preference_pairs)
3. Implement shadow middleware for LLM client interception
4. Configuration loading with configurable teacher model
5. Background worker queue for async shadowing

**Deliverable**: Data collection pipeline that captures training data usable by both tiers

### Phase 2: Quality Scoring + Preference Generation
*Makes collected data useful*

1. Heuristic scoring (fast, no API cost)
2. Optional teacher evaluation scoring
3. Automatic preference pair generation (student vs teacher)
4. Quality-based filtering

**Deliverable**: High-quality preference pairs accumulating in training.db

### Phase 3: Tier 1 - In-Context Learning
*Immediate improvement, no training needed*

1. Implement `examples.db` store (fewshot_examples)
2. Example extraction from high-quality shadow records
3. Similarity-based example selector
4. Agent loop integration for example injection

**Deliverable**: Models improved via few-shot examples at inference time

### Phase 4: Export + CLI
*Enable external training workflows*

1. Export to JSONL, DPO, OpenAI formats
2. CLI commands for shadow management
3. `export-db` command for portable training.db
4. Statistics and monitoring

**Deliverable**: Training data exportable for use with Axolotl, LLaMA-Factory, etc.

### Phase 5: Tier 2 - Adapter Management (Optional)
*Runtime adapter loading for trained models*

1. Implement `adapters.db` store
2. Ollama adapter loading integration
3. Adapter registration and activation
4. Training run tracking

**Deliverable**: Trained LoRA adapters loadable at runtime

## Critical Files to Modify

| File | Changes |
|------|---------|
| `internal/llm/client.go` | Add middleware wrapping support |
| `internal/agent/loop.go` | Add shadow manager option, example injection |
| `internal/daemon/components.go` | Initialize shadow manager |
| `internal/config/schema.go` | Add ShadowTrainingConfig |
| `cmd/meept/main.go` | Add shadow subcommand |

## Testing Strategy

1. **Unit tests**: Store operations, scoring, example selection
2. **Integration tests**: Middleware interception, teacher calls
3. **Manual testing**: `agent-tui ./bin/meept chat` with shadow enabled
4. **Export validation**: Verify format compatibility with training tools

## Worktree

Implementation will be done in: `../meept-shadow-training` (branch: `feature/shadow-training`)
