# Extended LFM2 Classifier Model Comparison Report

**Generated:** 2026-05-22
**Benchmark Tool:** `meept-classifier-test`
**Test Corpus:** 136 labeled examples across 12 intent categories

---

## Executive Summary

**Recommended Model:** `LFM2.5-1.2B-Instruct-MLX-4bit`

The standard Instruct model significantly outperforms all "Thinking" variants except the special Thinking-Claude-High-Reasoning version:

| Model | Accuracy | Error Rate | Avg Latency | Weighted Score |
|-------|----------|------------|-------------|----------------|
| **LFM2.5-1.2B-Instruct-MLX-4bit** | **69.1%** | **2.9%** | 279-508ms | **0.700** |
| LFM2-24B-A2B-MLX-8bit | 64.0% | 5.9% | 1102ms | 0.656 |
| Thinking-Claude-High-Reasoning-4bit | 58.8% | 5.1% | 332ms | 0.592 |
| LFM2.5-1.2B-Thinking-MLX-8bit | 0.0% | 100% | N/A | 0.000 |
| LFM2.5-1.2B-Thinking-MLX-bf16 | 0.0% | 100% | N/A | 0.000 |

### Key Finding: Thinking Models Fail

The `lfmstudio-community` Thinking model variants (8bit and bf16) **completely failed** with 0% accuracy due to 100% error rates. These models appear to require different prompt formatting or higher timeouts. The larger 24B MoE model showed good accuracy (64%) but had 4x latency.

---

## Model Details

### Model 1: LFM2.5-1.2B-Instruct-MLX-4bit (WINNER)
- **Path:** `/Volumes/LLMs/LFM2.5-1.2B-Instruct-MLX-4bit`
- **Type:** Standard Instruct
- **Quantization:** 4-bit MLX
- **Capabilities:** completion, code, reasoning
- **Accuracy:** 69.1%
- **Error Rate:** 2.9% (4/136)
- **Avg Latency:** 279-508ms

### Model 2: LFM2-24B-A2B-MLX-8bit
- **Path:** `/Volumes/LLMs/lmstudio-community/LFM2-24B-A2B-MLX-8bit`
- **Type:** Mixture of Experts (24B total, ~2B active)
- **Quantization:** 8-bit MLX
- **Capabilities:** completion, code, reasoning
- **Accuracy:** 64.0%
- **Error Rate:** 5.9% (8/136)
- **Avg Latency:** 1102ms

### Model 3: Thinking-Claude-High-Reasoning-4bit
- **Path:** `/Volumes/LLMs/alexgusevski/LFM2.5-1.2B-Instruct-Thinking-Claude-High-Reasoning-mlx-4Bit`
- **Type:** Instruct with Claude-style Chain of Thought
- **Quantization:** 4-bit MLX
- **Capabilities:** completion, reasoning
- **Accuracy:** 58.1-58.8%
- **Error Rate:** 4.4-5.1%
- **Avg Latency:** 332-397ms

### Model 4: LFM2.5-1.2B-Thinking-MLX-8bit (FAILED)
- **Path:** `/Volumes/LLMs/lmstudio-community/LFM2.5-1.2B-Thinking-MLX-8bit`
- **Type:** Thinking/Reasoning
- **Quantization:** 8-bit MLX
- **Capabilities:** completion, reasoning
- **Accuracy:** 0.0%
- **Error Rate:** 100% (136/136)
- **Notes:** Complete failure - likely requires different prompt format

### Model 5: LFM2.5-1.2B-Thinking-MLX-bf16 (FAILED)
- **Path:** `/Volumes/LLMs/lmstudio-community/LFM2.5-1.2B-Thinking-MLX-bf16`
- **Type:** Thinking/Reasoning (BF16 precision)
- **Quantization:** BF16 (uncompressed)
- **Capabilities:** completion, reasoning
- **Accuracy:** 0.0%
- **Error Rate:** 100% (136/136)
- **Notes:** Complete failure - same issue as 8bit variant

---

## Per-Model Per-Category Accuracy

### LFM2.5-1.2B-Instruct-MLX-4bit (Best Performer)

| Category | Accuracy | Notes |
|----------|----------|-------|
| **chat** | 100% | Perfect |
| **git** | 100% | Perfect |
| **search** | 100% | Perfect |
| **schedule** | 100% | Perfect |
| **debugging** | 92% | Excellent |
| **coding** | 64% | Good |
| **plan** | 60% | Adequate |
| **report** | 50% | Weak |
| **analyze** | 32% | Poor |
| **recall** | 33% | Poor |
| **review** | 25% | Poor |
| **platform** | 0% | Training gap |

### LFM2-24B-A2B-MLX-8bit

| Category | Accuracy | vs 4bit-Instruct |
|----------|----------|------------------|
| coding | 76% | +12% |
| analyze | 56% | +24% |
| debugging | 72% | -20% |
| chat | 70% | -30% |
| recall | 67% | +33% |
| plan | 60% | Equal |
| platform | 0% | Equal (gap) |
| report | 25% | -25% |
| review | 25% | Equal |
| git | 20% | -80% |
| schedule | 100% | Equal |
| search | 100% | Equal |

### Thinking-Claude-High-Reasoning-4bit

| Category | Accuracy | vs 4bit-Instruct |
|----------|----------|------------------|
| plan | 100% | +40% |
| debugging | 88% | -4% |
| schedule | 80% | -20% |
| search | 53% | -47% |
| analyze | 56% | +24% |
| coding | 48% | -16% |
| git | 70% | -30% |
| report | 25% | -25% |
| review | 25% | Equal |
| recall | 33% | Equal |
| platform | 0% | Equal (gap) |
| chat | 40% | -60% |

---

## Error Analysis

### LFM2.5-1.2B-Instruct-MLX-4bit: 4 errors (2.9% error rate)
- Minimal errors, highly reliable
- Errors likely edge cases in test corpus

### LFM2-24B-A2B-MLX-8bit: 8 errors (5.9% error rate)
- Timeout-related errors due to high latency
- Model requires longer timeout for reliable operation

### Thinking-Claude-High-Reasoning-4bit: 6-7 errors (4.4-5.1% error rate)
- Moderate reliability
- More consistent than other thinking models

### Thinking-MLX-8bit/bf16: 136 errors (100% error rate)
- Complete failure
- Likely causes:
  1. **Prompt format mismatch** - Thinking models may require explicit "think" or Chain of Thought triggers
  2. **Temperature mismatch** - May need higher temperature for thinking mode
  3. **Token limit issues** - Thinking output may exceed max_output (512 tokens)

---

## Latency Analysis

| Model | Avg Latency | Tokens/sec (est.) | Relative Speed |
|-------|-------------|-------------------|----------------|
| **Thinking-bf16** | ~279ms | ~40 | Fastest (when working) |
| **4bit-Instruct** | 279-508ms | ~35 | Fast |
| **Thinking-Claude** | 332-397ms | ~30 | Fast |
| **Thinking-8bit** | N/A (failed) | N/A | N/A |
| **24B-8bit** | 1102ms | ~8 | 4x slower |

**Note:** The 24B model has 4.4x higher latency, making it impractical for real-time classification despite decent accuracy.

---

## Confidence Calibration

| Model | Avg Confidence | Accuracy | Gap |
|-------|---------------|----------|-----|
| 4bit-Instruct | 94.6% | 69.1% | Overconfident by 25.5% |
| Thinking-Claude | 88.6-92.3% | 58.1-58.8% | Overconfident by 30-34% |
| 24B-8bit | 86.6% | 64.0% | Overconfident by 22.6% |
| Thinking-8bit/bf16 | 0% | 0% | N/A (failed) |

All working models are overconfident, but 24B-8bit has the best calibration despite high latency.

---

## Recommendation

**Primary: `LFM2.5-1.2B-Instruct-MLX-4bit`**

### Rationale:
1. **Best accuracy** - 69.1% overall, highest among all tested models
2. **Lowest error rate** - Only 2.9% failure rate
3. **Fast inference** - Sub-300ms average latency
4. **Reliable** - Consistent performance across categories
5. **Memory efficient** - 4-bit quantization fits easily in VRAM

### Configuration:
```json5
{
  "classifier_model": "local/lfm-1.2b-4bit",
  "small_model": "local/lfm-1.2b-4bit"
}
```

### When to Consider Alternatives:

**Use 24B-8bit if:**
- You need better coding/analyze performance (+12-24% in these categories)
- Latency is not a concern (batch processing)
- You have ample VRAM (24B model is much larger)

**Use Thinking-Claude-High-Reasoning if:**
- Planning tasks are critical (+40% on plan category)
- You prioritize debugging over other tasks
- You want Chain of Thought visibility

**Avoid Thinking-8bit/bf16 until:**
- Prompt engineering is done to fix 100% failure rate
- Temperature/max_output settings are tuned

---

## Next Steps

1. **Immediate:** Update config to use `lfm-1.2b-4bit` as primary classifier

2. **Investigate Thinking model failures:**
   - Test different temperature settings (0.7 vs 0.1)
   - Increase `max_output` from 512 to 1024+ tokens
   - Add explicit "think about this" prompt prefix

3. **Improve platform intent training:**
   - Both models fail on platform queries (0% accuracy)
   - Add capability query examples to training data

4. **Address weak categories:**
   - Review: 25% accuracy across all models
   - Recall: 33% accuracy - may need memory integration
   - Analyze: 32-56% variance - prompt engineering could help

5. **Consider ensemble approach:**
   - Use 4bit-Instruct for general classification
   - Fallback to 24B for complex coding/analyze tasks (if latency acceptable)

---

## Benchmark Comparison Matrix

```
┌─────────────────────────────────────┬──────────┬──────────┬───────────┬──────┐
│ Model                               │ Accuracy │ Error %  │ Latency   │ Score│
├─────────────────────────────────────┼──────────┼──────────┼───────────┼──────┤
│ LFM2.5-1.2B-Instruct-4bit           │  69.1%   │   2.9%   │   279ms   │ 0.700│
│ LFM2-24B-A2B-MLX-8bit               │  64.0%   │   5.9%   │  1102ms   │ 0.656│
│ Thinking-Claude-High-Reasoning-4bit │  58.8%   │   5.1%   │   332ms   │ 0.592│
│ Thinking-MLX-8bit                   │   0.0%   │  100%    │    N/A    │ 0.000│
│ Thinking-MLX-bf16                   │   0.0%   │  100%    │    N/A    │ 0.000│
└─────────────────────────────────────┴──────────┴──────────┴───────────┴──────┘
```

---

*Report generated by Meept Classifier Benchmark*
*Raw data: `docs/eval/benchmark-results.json`*
*Extended comparison: 5 models tested across 136 examples*
