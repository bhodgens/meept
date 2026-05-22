---
name: mlx-classifier-benchmark
description: |
  Benchmark LLM models for intent classification using Meept classifier-test.
  Use when: (1) evaluating new classifier models for production use,
  (2) comparing model accuracy/latency tradeoffs, (3) validating model
  upgrades. Tests models against 136 labeled examples across 12 intent
  categories. Metrics: accuracy, error rate, latency, confidence calibration.
author: Claude Code
version: 1.0.0
date: 2026-05-22
---

# MLX Classifier Benchmark

## Problem

Selecting the right classifier model for intent classification requires empirical
testing. Model names and quantization levels don't reliably predict performance.
"Thinking" models may fail completely if they require different prompt formats.

## Context / Trigger Conditions

- New MLX model available and need to evaluate for classifier use
- Comparing multiple models to find best accuracy/latency tradeoff
- Debugging why a model shows 0% accuracy (likely prompt format mismatch)
- Validating model configuration before deploying to production

## Solution

### Step 1: Add Model Configuration

Add the model to `config/models.json5`:

```json5
{
  "providers": {
    "local": {
      "models": {
        "lfm-new-model": {
          "name": "/Volumes/LLMs/path/to/model",
          "path": "/Volumes/LLMs/path/to/model",
          "capabilities": ["completion", "reasoning"],
          "input_cost": 0.0,
          "output_cost": 0.0,
          "context_limit": 8192,
          "max_output": 512,  // Increase to 1024+ for Thinking models
          "temperature": 0.1  // Increase to 0.7 for Thinking models
        }
      }
    }
  }
}
```

### Step 2: Start MLX Server

```bash
# Stop any running server
pkill -f "mlx_lm.server"

# Start with specific model
cd /Volumes/LLMs/path/to/model
nohup mlx_lm.server --model . --port 8080 > /tmp/mlx-server.log 2>&1 &

# Verify health
curl -s http://localhost:8080/health
```

### Step 3: Run Benchmark

```bash
./bin/meept-classifier-test \
  --model-a "/Volumes/LLMs/path/to/new-model" \
  --model-b "/Volumes/LLMs/path/to/baseline-model" \
  --output docs/eval \
  --name "new-model-vs-baseline"
```

### Step 4: Interpret Results

**Key metrics to compare:**

| Metric | Good | Bad | Notes |
|--------|------|-----|-------|
| Accuracy | >60% | <40% | Primary selection criterion |
| Error Rate | <5% | >20% | 100% = prompt format issue |
| Latency | <400ms | >1000ms | Critical for real-time use |
| Confidence | 80-95% | <70% or >95% | Overconfidence = unreliable |

**100% Error Rate Diagnosis:**
- Model requires different prompt format (Thinking models need "think" prefix)
- Temperature too low for model type
- max_output too small for reasoning output
- Test by direct curl call to check response format

### Step 5: Check Per-Category Performance

Some models excel at specific intents:
- Planning tasks: Thinking models may score 100% vs 60% for standard
- Chat: Standard models often outperform Thinking models
- Platform queries: Common failure across all models (training gap)

## Verification

Benchmark produces two outputs:
- `docs/eval/benchmark-results.json` - Raw metrics data
- `docs/eval/benchmark-report.md` - Human-readable comparison

Verify the winning model by:
1. Checking accuracy >60%
2. Error rate <10%
3. Latency acceptable for use case
4. No critical category failures

## Example

**Scenario**: Comparing 5 LFM2 models for production classifier

```bash
# Results summary:
| Model | Accuracy | Error Rate | Latency | Decision |
|-------|----------|------------|---------|----------|
| LFM2.5-1.2B-Instruct-4bit | 69.1% | 2.9% | 279ms | WINNER |
| LFM2-24B-A2B-MLX-8bit | 64.0% | 5.9% | 1102ms | Too slow |
| Thinking-Claude-4bit | 58.8% | 5.1% | 332ms | Good fallback |
| Thinking-MLX-8bit | 0.0% | 100% | N/A | Broken |
| Thinking-MLX-bf16 | 0.0% | 100% | N/A | Broken |
```

**Key insight**: The standard Instruct model beat all "Thinking" variants despite
their reasoning capabilities. Model architecture matters more than quantization
or feature names.

## Notes

### Thinking Model Failures

Thinking models (lfmstudio-community builds) may fail with 100% error rate due to:

1. **Prompt format mismatch**: May need explicit "Think step by step" prefix
2. **Temperature settings**: Thinking models often need 0.7+ vs 0.1 for standard
3. **max_output limits**: Reasoning output may exceed 512 token limit
4. **Different chat template**: May use different system/user/assistant markers

**Debug by testing directly:**
```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "/path/to/model", "messages": [{"role": "user", "content": "hello"}]}' \
  | jq '.choices[0].message.content'
```

### Model Selection Guidelines

- **Primary classifier**: Best accuracy + lowest latency
- **Fallback chain**: Order by reliability, not raw accuracy
- **Consider use case**: High-latency models OK for batch, not real-time

### Performance Notes

- 4-bit models often match or beat higher precision variants
- Larger models (24B+) have 4x latency even on M-series chips
- Thinking models with reasoning traces use more tokens, slower inference

## References

- [Meept Classifier Benchmark Tool](../cmd/meept-classifier-test/main.go)
- [MLX LM Server Documentation](https://github.com/ml-explore/mlx-lm)
- [LFM2 Model Family](https://huggingface.co/collections/Language-Foundations/lfm2-676f9d0c8d3e5a3a3e3e3e3e)
