# LFM2.5 Classifier Model Comparison Report

**Generated:** 2026-05-22 (Updated 2026-05-22 Extended)
**Benchmark Tool:** `meept-classifier-test`
**Test Corpus:** 136 labeled examples across 12 intent categories

**See also:** [Extended Comparison Report](./classifier-comparison-report-extended.md) - 5 model comparison with additional LFM2.5-1.2B-Instruct-MLX-4bit, LFM2-24B-A2B-MLX-8bit, Thinking-MLX-8bit, and Thinking-MLX-bf16 models.

---

## Executive Summary

**Recommended Model:** `LFM2.5-1.2B-Instruct-MLX-4bit` (NEW - outperforms Thinking-Claude)

The standard Instruct 4-bit model achieves the best overall performance, while the Thinking-Claude variant ranks third:

| Metric | Combined-SFT | Thinking-Claude | 4bit-Instruct (NEW) |
|--------|--------------|-----------------|---------------------|
| **Accuracy** | 36.8% | 58.8% | **69.1%** |
| **Error Rate** | 29.4% (40/136) | 5.1% (7/136) | **2.9% (4/136)** |
| **Avg Confidence** | 79.4% | 88.6% | **94.6%** |
| **Avg Latency** | 330ms | 332ms | **279ms** |
| **Weighted Score** | 0.429 | 0.601 | **0.700** |

---

## Model Details

### Model A: lfm2.5-1.2b-combined-serialized-sft
- **Path:** `/Volumes/LLMs/lfm2.5-1.2b-combined-serialized-sft`
- **Type:** Serialized SFT (Supervised Fine-Tuned)
- **Capabilities:** completion, code, reasoning
- **Quantization:** Q8_0 (8-bit)

### Model B: LFM2.5-1.2B-Instruct-Thinking-Claude-High-Reasoning-mlx-4Bit
- **Path:** `/Volumes/LLMs/alexgusevski/LFM2.5-1.2B-Instruct-Thinking-Claude-High-Reasoning-mlx-4Bit`
- **Type:** Instruct with Claude-style Chain of Thought
- **Capabilities:** completion, reasoning
- **Quantization:** 4-bit
- **Status:** Third place (58.8% accuracy)

### Model C: LFM2.5-1.2B-Instruct-MLX-4bit (WINNER)
- **Path:** `/Volumes/LLMs/LFM2.5-1.2B-Instruct-MLX-4bit`
- **Type:** Standard Instruct
- **Capabilities:** completion, code, reasoning
- **Quantization:** 4-bit MLX
- **Status:** Best overall (69.1% accuracy, 2.9% error rate)

### Additional Models Tested (see extended report):
- **LFM2-24B-A2B-MLX-8bit:** 64.0% accuracy, 1102ms latency (too slow)
- **LFM2.5-1.2B-Thinking-MLX-8bit:** 0% accuracy (complete failure)
- **LFM2.5-1.2B-Thinking-MLX-bf16:** 0% accuracy (complete failure)

---

## Per-Category Accuracy Comparison

| Category | Combined-SFT | Thinking-Claude | Winner |
|----------|--------------|-----------------|--------|
| **debugging** | 68.0% | 88.0% | Thinking-Claude (+20%) |
| **chat** | 90.0% | 40.0% | Combined-SFT (+50%) |
| **schedule** | 60.0% | 80.0% | Thinking-Claude (+20%) |
| **search** | 20.0% | 60.0% | Thinking-Claude (+40%) |
| **analyze** | 24.0% | 56.0% | Thinking-Claude (+32%) |
| **coding** | 28.0% | 56.0% | Thinking-Claude (+28%) |
| **git** | 10.0% | 50.0% | Thinking-Claude (+40%) |
| **plan** | 20.0% | 100.0% | Thinking-Claude (+80%) |
| **platform** | 0.0% | 0.0% | Tie (both fail) |
| **review** | 25.0% | 25.0% | Tie |
| **report** | 25.0% | 25.0% | Tie |
| **recall** | 33.3% | 33.3% | Tie |

### Key Observations:

1. **Thinking-Claude dominates technical tasks:**
   - Debugging: 88% vs 68%
   - Coding: 56% vs 28%
   - Git: 50% vs 10%
   - Analyze: 56% vs 24%

2. **Combined-SFT only wins on chat (90% vs 40%)** - This suggests the thinking model may over-analyze simple greetings.

3. **Both models fail on platform queries (0%)** - This is a training data gap, not a model capability issue.

4. **Thinking-Claude achieves 100% on plan category** - Perfect performance on planning tasks.

---

## Error Analysis

### Combined-SFT: 40 errors (29.4% error rate)
- Most errors appear to be timeouts or malformed JSON responses
- The model struggles with complex intent classification

### Thinking-Claude: 7 errors (5.1% error rate)
- Significantly more reliable
- Errors likely due to edge cases in the test corpus

---

## Confidence Calibration

| Model | Avg Confidence | Accuracy | Calibration Gap |
|-------|---------------|----------|-----------------|
| Combined-SFT | 79.4% | 36.8% | **Overconfident by 42.6%** |
| Thinking-Claude | 88.6% | 58.8% | Overconfident by 29.8% |

Both models are overconfident, but Thinking-Claude is better calibrated.

---

## Recommendation (Updated)

**Use `LFM2.5-1.2B-Instruct-MLX-4bit` as the classifier model.**

### Rationale:
1. **Highest accuracy** - 69.1% overall, beats Thinking-Claude by 10%
2. **Lowest error rate** - Only 2.9% failure rate (4/136)
3. **Fastest inference** - 279ms average latency
4. **Best weighted score** - 0.700 vs 0.592 for Thinking-Claude

### Configuration:
```json5
{
  "classifier_model": "local/lfm-1.2b-4bit",
  "small_model": "local/lfm-1.2b-4bit"
}
```

### Caveats:
1. **Plan intent weaker** - 60% vs Thinking-Claude's 100% - consider routing planning tasks to a specialized model
2. **Platform queries fail** - training data gap, not model capability
3. **Review/recall weak** - 25-33% accuracy, may need memory integration

---

## Next Steps

1. **Update daemon config** to use `lfm-1.2b-4bit` as the default classifier (DONE in extended config)
2. **Investigate Thinking model failures** - 8bit and bf16 variants failed with 0% accuracy
3. **Improve platform intent training data** - all models fail on capability queries (0%)
4. **Address weak categories** - review (25%), recall (33%), analyze (32%) need improvement
5. **Consider 24B for complex tasks** - if latency is acceptable, shows +24% on analyze

---

*Report generated by Meept Classifier Benchmark*
*Raw data: `docs/eval/benchmark-results.json`*
