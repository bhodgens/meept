# LoRA Learning Pipeline for LFM2.5

**Date**: 2026-07-09
**Type**: Implementation Plan
**Priority**: High
**Status**: Complete (2026-07-10)

> Implementation lives in `internal/learning/`, `internal/llm/adapter_*.go`,
> `internal/llm/lfm_loader.go`, `cmd/meept/learning.go`, `scripts/train_lora.py`,
> `scripts/generate_adapter_config.py`, `hooks/on_adapter_trained.sh`, and
> daemon wiring in `internal/daemon/components.go`. Deferred distillation work
> remains in `.github/ISSUES/lora-distillation-pipeline.md`.
>
> **Gap closure (2026-07-10):** Fixed multi-turn tool-path pollution in
> trajectory capture (current-turn only), skip pure-chat trajectories, match
> train_lora dtype/amp (bf16 vs fp16), versioned `train_all_adapters.sh`,
> custom path support for adapter registry generation, and CLI→hook path
> passthrough. See `docs/workflows/learning.md`.

---

## Executive Summary

This plan implements a **continuous learning pipeline** that captures agent research trajectories, converts them into training data, and produces LoRA adapters for LFM2.5 models (1.2B and 8B). The system enables Meept to become smarter about your specific domains and workflows over time.

### Key Capabilities

1. **Passive capture**: All agent research (memory searches, file reads, web lookups) is automatically captured
2. **Quality filtering**: Only high-quality, successful research trajectories are retained
3. **Domain routing**: Data is sorted into domain-specific datasets (code, debugging, api_research, etc.)
4. **Manual training trigger**: User initiates training when datasets are mature
5. **Multi-adapter support**: Separate adapters for LFM2.5-8B and LFM2.5-1.2B
6. **Config auto-generation**: Adapter configs are automatically generated post-training
7. **Startup loading**: All adapters loaded at Meept startup, routed by domain

---

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────────┐
│  Agent Loop (internal/agent/loop.go)                             │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  Research Tools                                            │ │
│  │  - memory_search, file_read, grep, web_search              │ │
│  │  - Multi-hop reasoning chains                              │ │
│  └────────────────────────────────────────────────────────────┘ │
│                          │                                       │
│                          ▼                                       │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  Research Capture Layer (NEW)                              │ │
│  │  - internal/learning/capture.go                            │ │
│  │  - Intercepts tool calls + results                         │ │
│  │  - Builds (query, sources, synthesis) tuples               │ │
│  └────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│  Learning Pipeline (internal/learning/)                          │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │  JUDGE          │  │  DISTILL        │  │  DATASET        │  │
│  │  - Quality score│  │  - Extract      │  │  - Store JSONL  │  │
│  │  - Novelty      │  │    patterns     │  │  - Domain route │  │
│  │  - Dedup        │  │  - Format pairs │  │  - Retention    │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│  Training (scripts/train_lora.py)                                │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  Manual Trigger: meept learning train --domain code        │ │
│  │  - Loads LFM2.5-8B or LFM2.5-1.2B                          │ │
│  │  - PEFT/TRL LoRA training                                  │ │
│  │  - Saves adapter to ~/.meept/adapters/{domain}/{model}/    │ │
│  └────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│  Adapter Loading (internal/llm/adapter_loader.go)                │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  Meept startup:                                            │ │
│  │  - Scans ~/.meept/adapters/                                │ │
│  │  - Generates config (scripts/generate_adapter_config.py)   │ │
│  │  - Loads all adapters into memory                          │ │
│  │  - Routes by domain at inference time                      │ │
│  └────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────┘
```

---

## Phase 1: Dataset Capture

**Goal:** Intercept agent research actions and produce training examples.

### 1.1 Define Data Structures

**File**: `internal/learning/types.go`

```go
package learning

// ResearchTrajectory captures a complete research action
type ResearchTrajectory struct {
    ID          string            `json:"id"`
    SessionID   string            `json:"session_id"`
    Domain      string            `json:"domain"`  // code, debugging, api_research
    Intent      string            `json:"intent"`  // Original user intent
    Query       string            `json:"query"`   // Research query
    ToolCalls   []ToolCallRecord  `json:"tool_calls"`
    Synthesis   string            `json:"synthesis"`  // Agent's final answer
    TaskOutcome TaskOutcome       `json:"task_outcome"`
    Timestamp   time.Time         `json:"timestamp"`
}

type ToolCallRecord struct {
    Tool    string `json:"tool"`
    Query   string `json:"query"`
    Results int    `json:"results_count"`
    Used    bool   `json:"used"`  // Was this result actually used?
}

type TaskOutcome struct {
    Success    bool    `json:"success"`
    Quality    float64 `json:"quality"`    // 0.0-1.0
    UserFeedback string `json:"feedback"`  // Optional user feedback
}

// TrainingExample is the JSONL format for training
type TrainingExample struct {
    Instruction string            `json:"instruction"`
    Input       string            `json:"input"`
    Output      string            `json:"output"`
    Metadata    ExampleMetadata   `json:"metadata"`
}

type ExampleMetadata struct {
    Source       string   `json:"source"`        // "agent_research"
    Domain       string   `json:"domain"`
    SessionID    string   `json:"session_id"`
    ToolPath     []string `json:"tool_path"`
    QualityScore float64  `json:"quality_score"`
    Timestamp    string   `json:"timestamp"`
}
```

### 1.2 Capture Hook in Agent Loop

**File**: `internal/agent/loop.go` (modification)

Add capture call after tool execution:

```go
// After tool call succeeds
if toolResult.Success && l.learningCapture != nil {
    l.learningCapture.RecordResearch(ctx, sessionID, query, toolCall, toolResult, synthesis)
}
```

### 1.3 Capture Implementation

**File**: `internal/learning/capture.go`

```go
package learning

import (
    "context"
    "encoding/json"
    "os"
    "path/filepath"
)

type CaptureRecorder struct {
    dataDir string
    mu      sync.Mutex
}

func NewCaptureRecorder(dataDir string) *CaptureRecorder {
    return &CaptureRecorder{dataDir: dataDir}
}

func (c *CaptureRecorder) RecordResearch(ctx context.Context, sessionID, query string, toolCall ToolCall, result ToolResult, synthesis string) error {
    c.mu.Lock()
    defer c.mu.Unlock()

    trajectory := ResearchTrajectory{
        ID:        generateID(),
        SessionID: sessionID,
        Query:     query,
        // ... populate fields
    }

    // Write to raw captures file
    capturesFile := filepath.Join(c.dataDir, "raw_captures.jsonl")
    f, err := os.OpenFile(capturesFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    defer f.Close()

    data, _ := json.Marshal(trajectory)
    f.Write(data)
    f.Write([]byte("\n"))

    return nil
}
```

### 1.4 Domain Classifier

**File**: `internal/learning/classifier.go`

Simple keyword-based domain routing (can evolve to ML-based later):

```go
func classifyDomain(query, toolOutput string) string {
    codeKeywords := []string{"code", "function", "package", "import", "interface"}
    debugKeywords := []string{"error", "fail", "bug", "panic", "stack trace"}
    apiKeywords := []string{"API", "endpoint", "HTTP", "REST", "authenticate"}

    text := strings.ToLower(query + " " + toolOutput)

    // Count keyword matches
    codeScore := countKeywords(text, codeKeywords)
    debugScore := countKeywords(text, debugKeywords)
    apiScore := countKeywords(text, apiKeywords)

    // Return highest scoring domain
    if codeScore >= debugScore && codeScore >= apiScore {
        return "code"
    } else if debugScore >= apiScore {
        return "debugging"
    }
    return "api_research"
}
```

### 1.5 Dataset Writer

**File**: `internal/learning/dataset.go`

```go
package learning

// DomainDatasets manages domain-specific JSONL files
type DomainDatasets struct {
    baseDir string
    files   map[string]*os.File  // domain -> open file handle
}

func (d *DomainDatasets) Append(domain string, example TrainingExample) error {
    filePath := filepath.Join(d.baseDir, domain+".jsonl")
    f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    defer f.Close()

    data, _ := json.Marshal(example)
    f.Write(data)
    f.Write([]byte("\n"))
    return nil
}
```

---

## Phase 2: Quality Judgment

**Goal:** Automatically score research examples for training quality.

### 2.1 Heuristic Scorer

**File**: `internal/learning/judge.go`

```go
func ScoreExample(traj ResearchTrajectory) float64 {
    score := 0.5  // Base score

    // Task success bonus
    if traj.TaskOutcome.Success {
        score += 0.2
    }

    // Research was used bonus
    if traj.WasResearchUsed() {
        score += 0.15
    }

    // Multi-hop reasoning bonus
    if len(traj.ToolCalls) > 1 {
        score += 0.1
    }

    // User positive feedback
    if traj.TaskOutcome.UserFeedback == "positive" {
        score += 0.15
    }

    return min(score, 1.0)
}
```

### 2.2 Novelty Detection (Dedup)

**File**: `internal/learning/dedup.go`

```go
func IsDuplicate(newExample TrainingExample, existingFile string) bool {
    // Simple hash-based dedup on instruction
    existingHashes := loadHashes(existingFile)
    newHash := sha256.Sum256([]byte(newExample.Instruction))

    for _, h := range existingHashes {
        if bytes.Equal(newHash[:], h[:]) {
            return true
        }
    }
    return false
}
```

---

## Phase 3: Dataset Management

**Goal:** Maintain clean, deduplicated, domain-routed datasets.

### 3.1 Directory Structure

```
~/.meept/learning/
├── raw_captures.jsonl      # All captures (immutable log)
├── datasets/
│   ├── code.jsonl          # Domain-filtered
│   ├── debugging.jsonl
│   ├── api_research.jsonl
│   ├── security.jsonl
│   ├── meept_internal.jsonl
│   └── personal.jsonl
└── metadata.json           # Provenance, stats
```

### 3.2 Consolidation Job

**File**: `internal/learning/consolidate.go`

Runs periodically (or on manual trigger):
1. Read raw_captures.jsonl
2. Score each example
3. Deduplicate against existing datasets
4. Route to domain files
5. Apply retention (drop oldest if file > max size)

---

## Phase 4: LoRA Training Integration

**Goal:** Train LFM2.5 adapters from captured datasets.

### 4.1 Training Stack Setup

**File**: `scripts/setup-training.sh`

```bash
#!/bin/bash
# Install training dependencies

pip install \
    torch torchvision torchaudio \
    transformers \
    peft \
    trl \
    accelerate \
    bitsandbytes \
    datasets

# Verify GPU availability (optional, CPU works but slow)
python -c "import torch; print(f'CUDA available: {torch.cuda.is_available()}')"
```

### 4.2 Model Architecture Inspection

**File**: `scripts/inspect_lfm_architecture.py`

```python
#!/usr/bin/env python3
"""Find target modules for LoRA in LFM2.5"""

from transformers import AutoModelForCausalLM

model = AutoModelForCausalLM.from_pretrained("LFM/LFM2.5-8B")

print("=== Target Modules for LoRA ===")
for name, module in model.named_modules():
    module_type = type(module).__name__
    if "linear" in module_type.lower() or "conv" in module_type.lower():
        print(f"{name}: {module_type}")
```

### 4.3 LoRA Training Script

**File**: `scripts/train_lora.py`

```python
#!/usr/bin/env python3
"""Train LoRA adapter for LFM2.5"""

import argparse
from transformers import AutoModelForCausalLM, AutoTokenizer, TrainingArguments
from peft import LoraConfig, get_peft_model
from trl import SFTTrainer
from datasets import load_from_disk
import torch

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", required=True, choices=["lfm2.5-8b", "lfm2.5-1.2b"])
    parser.add_argument("--dataset", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--rank", type=int, default=16)
    parser.add_argument("--epochs", type=int, default=3)
    args = parser.parse_args()

    # Load model
    model_map = {
        "lfm2.5-8b": "LFM/LFM2.5-8B",
        "lfm2.5-1.2b": "LFM/LFM2.5-1.2B",
    }
    model = AutoModelForCausalLM.from_pretrained(
        model_map[args.model],
        torch_dtype=torch.bfloat16,
        device_map="auto"
    )

    # LoRA config - target modules specific to LFM2.5 hybrid architecture
    lora_config = LoraConfig(
        r=args.rank,
        lora_alpha=args.rank * 2,
        target_modules=[
            "q_proj", "k_proj", "v_proj", "o_proj",  # Attention
            "gate_proj", "up_proj", "down_proj",     # MLP
            # "conv_*"  # LFM-specific conv layers (discover via inspect script)
        ],
        lora_dropout=0.05,
        bias="none",
        task_type="CAUSAL_LM"
    )

    model = get_peft_model(model, lora_config)
    model.print_trainable_parameters()

    # Load dataset
    dataset = load_from_disk(args.dataset)

    # Training
    trainer = SFTTrainer(
        model=model,
        train_dataset=dataset,
        dataset_text_field="formatted",
        max_seq_length=2048,
        args=TrainingArguments(
            per_device_train_batch_size=4,
            gradient_accumulation_steps=4,
            num_train_epochs=args.epochs,
            output_dir=args.output,
            fp16=True,
            logging_steps=10,
            save_strategy="epoch",
        )
    )

    trainer.train()
    trainer.save_model()
    print(f"Adapter saved to {args.output}")

if __name__ == "__main__":
    main()
```

### 4.4 Training Configs

**File**: `config/training/lora_lfm2.5_8b.yaml`

```yaml
# LoRA Training Config for LFM2.5-8B
model:
  name_or_path: "LFM/LFM2.5-8B"
  dtype: "bfloat16"

lora:
  rank: 16
  alpha: 32
  dropout: 0.05
  target_modules:
    - "q_proj"
    - "k_proj"
    - "v_proj"
    - "o_proj"
    - "gate_proj"
    - "up_proj"
    - "down_proj"

training:
  batch_size: 4
  gradient_accumulation: 4
  epochs: 3
  learning_rate: 1.0e-4
  max_seq_length: 2048
  fp16: true

output:
  base_dir: "~/.meept/adapters"
```

**File**: `config/training/lora_lfm2.5_1.2b.yaml`

Same structure, different `model.name_or_path`.

### 4.5 Training Runner Script

**File**: `scripts/train_all_adapters.sh`

```bash
#!/bin/bash
# Train adapters for all domains and both model sizes

DOMAINS=("code" "debugging" "api_research" "security" "meept_internal" "personal")
MODELS=("lfm2.5-8b" "lfm2.5-1.2b")

for domain in "${DOMAINS[@]}"; do
    DATASET="$HOME/.meept/learning/datasets/${domain}.jsonl"
    if [ ! -f "$DATASET" ]; then
        echo "Skipping $domain: dataset not found"
        continue
    fi

    for model in "${MODELS[@]}"; do
        OUTPUT="$HOME/.meept/adapters/${domain}/${model}-v1"
        echo "Training $domain for $model..."
        python scripts/train_lora.py \
            --model "$model" \
            --dataset "$DATASET" \
            --output "$OUTPUT" \
            --epochs 3
    done
done
```

---

## Phase 5: Config Auto-Generation

**Goal:** Automatically generate adapter configs after training.

### 5.1 Adapter Registry Schema

**File**: `internal/config/adapter_config.go`

```go
package config

type AdapterRegistry struct {
    Adapters []AdapterEntry `json:"adapters"`
    Version  int            `json:"version"`
}

type AdapterEntry struct {
    ID          string   `json:"id"`            // "code-lfm2.5-8b-v1"
    Domain      string   `json:"domain"`
    Model       string   `json:"model"`         // "lfm2.5-8b"
    Path        string   `json:"path"`          // "~/.meept/adapters/code/lfm2.5-8b-v1"
    CreatedAt   string   `json:"created_at"`
    TrainingMD5 string   `json:"training_md5"`  // Dataset fingerprint
    Enabled     bool     `json:"enabled"`
}
```

### 5.2 Config Generation Script

**File**: `scripts/generate_adapter_config.py`

```python
#!/usr/bin/env python3
"""Generate adapter registry config after training"""

import json
import hashlib
from pathlib import Path
from datetime import datetime

def get_dataset_md5(dataset_path):
    """Hash the training dataset for provenance"""
    with open(dataset_path, 'rb') as f:
        return hashlib.md5(f.read()).hexdigest()

def scan_adapters(base_dir):
    """Scan adapter directory structure"""
    adapters = []
    base = Path(base_dir)

    for domain_dir in base.iterdir():
        if not domain_dir.is_dir():
            continue
        domain = domain_dir.name

        for model_dir in domain_dir.iterdir():
            if not model_dir.is_dir():
                continue

            # Find training metadata
            meta_file = model_dir / "training_meta.json"
            if meta_file.exists():
                meta = json.loads(meta_file.read_text())
            else:
                meta = {"dataset": f"{domain}.jsonl"}

            adapters.append({
                "id": f"{domain}-{model_dir.name}",
                "domain": domain,
                "model": model_dir.name.split("-v")[0],  # "lfm2.5-8b"
                "path": str(model_dir),
                "created_at": datetime.now().isoformat(),
                "training_md5": get_dataset_md5(
                    Path.home() / ".meept/learning/datasets" / meta["dataset"]
                ),
                "enabled": True
            })

    return adapters

def main():
    base_dir = Path.home() / ".meept/adapters"
    adapters = scan_adapters(base_dir)

    registry = {
        "adapters": adapters,
        "version": 1,
        "generated_at": datetime.now().isoformat()
    }

    output = Path.home() / ".meept/adapter_registry.json"
    output.write_text(json.dumps(registry, indent=2))
    print(f"Generated adapter registry: {len(adapters)} adapters")

if __name__ == "__main__":
    main()
```

### 5.3 Post-Training Hook

**File**: `hooks/on_adapter_trained.sh`

```bash
#!/bin/bash
# Called after each training run

DOMAIN=$1
MODEL=$2
OUTPUT_PATH=$3

# Save training metadata
cat > "$OUTPUT_PATH/training_meta.json" << EOF
{
  "domain": "$DOMAIN",
  "model": "$MODEL",
  "dataset": "${DOMAIN}.jsonl",
  "trained_at": "$(date -Iseconds)"
}
EOF

# Regenerate adapter registry
python scripts/generate_adapter_config.py
```

---

## Phase 6: Adapter Loading at Startup

**Goal:** Load all adapters automatically when Meept starts.

### 6.1 Adapter Registry Loading

**File**: `internal/llm/adapter_loader.go`

```go
package llm

import (
    "encoding/json"
    "os"
    "path/filepath"
)

type AdapterRegistry struct {
    Adapters []AdapterEntry `json:"adapters"`
}

func LoadAdapterRegistry(registryPath string) (*AdapterRegistry, error) {
    data, err := os.ReadFile(registryPath)
    if err != nil {
        if os.IsNotExist(err) {
            return &AdapterRegistry{}, nil
        }
        return nil, err
    }

    var registry AdapterRegistry
    if err := json.Unmarshal(data, &registry); err != nil {
        return nil, err
    }
    return &registry, nil
}
```

### 6.2 LFM Model Launcher

**File**: `internal/llm/lfm_loader.go`

```go
package llm

// LFMLoader manages LFM2.5 model + adapter loading
type LFMLoader struct {
    BaseModel   string  // "lfm2.5-8b" or "lfm2.5-1.2b"
    ModelPath   string
    Adapters    map[string]*LoadedAdapter  // domain -> adapter
}

type LoadedAdapter struct {
    Domain string
    Path   string
    Model  interface{}  // PEFT-wrapped model
}

func (l *LFMLoader) LoadAllAdapters(registry *AdapterRegistry) error {
    l.Adapters = make(map[string]*LoadedAdapter)

    for _, entry := range registry.Adapters {
        if !entry.Enabled {
            continue
        }

        // Only load adapters for this base model
        if entry.Model != l.BaseModel {
            continue
        }

        adapter, err := l.loadAdapter(entry.Path)
        if err != nil {
            log.Printf("Failed to load adapter %s: %v", entry.ID, err)
            continue
        }

        l.Adapters[entry.Domain] = adapter
        log.Printf("Loaded adapter: %s (%s)", entry.ID, entry.Domain)
    }

    return nil
}
```

### 6.3 Adapter Router

**File**: `internal/llm/adapter_router.go`

```go
package llm

// AdapterRouter selects the right adapter per request
type AdapterRouter struct {
    adapters map[string]*LoadedAdapter
    fallback *LoadedAdapter  // Used when no domain-specific adapter
}

func (r *AdapterRouter) SelectAdapter(domain string) *LoadedAdapter {
    if adapter, ok := r.adapters[domain]; ok {
        return adapter
    }
    return r.fallback  // Fall back to base model or general adapter
}
```

### 6.4 Integration with Agent Loop

**File**: `internal/agent/loop.go` (modification)

```go
// In NewAgentLoop or similar constructor
func NewAgentLoop(...) (*AgentLoop, error) {
    // ... existing setup ...

    // Load adapter router
    registry, _ := llm.LoadAdapterRegistry("~/.meept/adapter_registry.json")
    loader := &llm.LFMLoader{BaseModel: config.LFMModel}
    loader.LoadAllAdapters(registry)

    loop.adapterRouter = &llm.AdapterRouter{
        adapters: loader.Adapters,
        fallback: nil,  // Or load "general" adapter
    }

    return loop, nil
}

// In chat/completion handler
func (l *AgentLoop) Chat(ctx context.Context, input string) (string, error) {
    // Classify domain from input
    domain := classifyDomain(input)

    // Select adapter
    adapter := l.adapterRouter.SelectAdapter(domain)

    // Use adapter for inference
    if adapter != nil {
        return l.llmClient.ChatWithAdapter(ctx, input, adapter)
    }
    return l.llmClient.Chat(ctx, input)
}
```

---

## Phase 7: Training Data Retention

**Goal:** Preserve raw data for retraining on new models.

### 7.1 Raw Dataset Storage

All captures are appended to `~/.meept/learning/raw_captures.jsonl` (immutable log).

### 7.2 Dataset Versioning

**File**: `internal/learning/dataverse.go`

```go
type DatasetVersion struct {
    Version     int       `json:"version"`
    CreatedAt   time.Time `json:"created_at"`
    Domain      string    `json:"domain"`
    ExampleCount int      `json:"example_count"`
    MD5         string    `json:"md5"`
    Source      string    `json:"source"`  // "raw_captures" or "distillation"
}

func CreateSnapshot(domain string) (*DatasetVersion, error) {
    // Copy current dataset to versioned file
    // datasets/code.jsonl -> versions/code_v1.jsonl
}
```

---

## Phase 8: CLI Commands

**Goal:** User-facing commands for learning management.

### 8.1 Learning CLI

**File**: `cmd/meept/learning.go`

```go
var learningCmd = &cobra.Command{
    Use:   "learning",
    Short: "Manage LoRA learning pipeline",
}

var learningTrainCmd = &cobra.Command{
    Use:   "train [domain]",
    Short: "Train LoRA adapter for a domain",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []error) error {
        domain := args[0]
        model, _ := cmd.Flags().GetString("model")
        return runTraining(domain, model)
    },
}

var learningStatusCmd = &cobra.Command{
    Use:   "status",
    Short: "Show learning pipeline status",
    RunE: func(cmd *cobra.Command, args []error) error {
        return showLearningStatus()
    },
}

var learningListCmd = &cobra.Command{
    Use:   "list",
    Short: "List trained adapters",
    RunE: func(cmd *cobra.Command, args []error) error {
        return listAdapters()
    },
}
```

### 8.2 Example Commands

```bash
# Train adapter for code domain
meept learning train code --model lfm2.5-8b

# Check learning status
meept learning status

# List all adapters
meept learning list

# Show dataset stats
meept learning dataset-stats code
```

---

## Phase 9: Configuration

**File**: `config/meept.json5` (additions)

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
      "auto_train_threshold": 500,  // Don't auto-train
      "manual_only": true,
    },
    "retention": {
      "max_dataset_size_mb": 100,
      "keep_versions": 3,
    },
  },
}
```

---

## Success Criteria

| Criterion | Target |
|-----------|--------|
| Capture latency | <10ms per tool call |
| Training convergence | <6 hours per adapter (8B), <2 hours (1.2B) |
| Adapter quality | >80% match to teacher (for distillation) |
| Startup time | <5s adapter loading |
| Inference overhead | <20ms adapter routing |

---

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| LFM2.5 model not available | Use Qwen2.5 as fallback (similar architecture) |
| Training too slow on CPU | Document GPU requirement, add Colab script |
| Adapters conflict at runtime | Namespace by domain, clear routing rules |
| Dataset grows unbounded | Retention policy, automatic pruning |
| Poor adapter quality | Quality filtering, more training data |

---

## Implementation Order

1. **Phase 1** - Dataset capture (foundational)
2. **Phase 3** - Dataset management (storage, routing)
3. **Phase 2** - Quality judgment (filtering)
4. **Phase 4** - Training integration (core capability)
5. **Phase 5** - Config auto-generation (automation)
6. **Phase 6** - Adapter loading (runtime)
7. **Phase 7** - Data retention (versioning)
8. **Phase 8** - CLI commands (UX)
9. **Phase 9** - Config schema (integration)

---

## Deferred: Distillation Pipeline

See `.github/ISSUES/lora-distillation-pipeline.md` for future work on:
- Knowledge distillation from Claude/GPT-4
- DPO training for alignment
- Hidden state distillation (local teachers only)

---

## References

- PEFT Documentation: https://huggingface.co/docs/peft
- TRL Documentation: https://huggingface.co/docs/trl
- LFM2.5 Model Card: (TBD - model not publicly documented)
- Qwen2.5 Architecture: https://huggingface.co/Qwen/Qwen2.5-7B
