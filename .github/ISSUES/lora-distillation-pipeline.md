# LoRA Distillation Pipeline for Non-LFM Models

**Date**: 2026-07-09
**Type**: Feature Extension
**Priority**: Deferred (future work)
**Related**: LoRA Learning Pipeline (primary implementation)

---

## Executive Summary

This issue tracks future work to extend the LoRA training pipeline to support **knowledge distillation** from non-LFM teacher models (Claude, GPT-4, etc.) into LFM student adapters. This enables transferring capabilities from closed-source models into local, trainable adapters.

---

## Motivation

The primary LoRA learning pipeline captures agent research trajectories and trains LFM2.5 adapters. However, this is limited to:
- **LFM-only training**: Adapters only learn from LFM2.5's own behavior
- **No capability transfer**: Can't inherit strengths from more capable models
- **Cold start**: New domains require extensive data collection before adapters are useful

**Distillation solves this** by using powerful teacher models to generate training data for LFM students.

---

## Proposed Approach

### Option A: Output Distillation (Simpler)

Train LFM to match teacher model outputs on the same inputs.

```
┌─────────────────────────────────────────────────────────┐
│  Captured Research Queries                              │
│  (instructions from agent work)                         │
└─────────────────────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────────────────────┐
│  Teacher Model (Claude/GPT-4) generates responses       │
│  - Same prompts, teacher answers                        │
│  - Store: (instruction, teacher_response)               │
└─────────────────────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────────────────────┐
│  LFM2.5 Student Fine-tuning (SFT)                       │
│  - Standard supervised fine-tuning                      │
│  - Loss: CE(student_output, teacher_response)           │
└─────────────────────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────────────────────┐
│  LFM2.5 adapter distilled from teacher                  │
└─────────────────────────────────────────────────────────┘
```

**Pros:**
- Simple SFT training (same pipeline as regular LoRA)
- No architectural changes needed
- Works with any API-accessible teacher

**Cons:**
- Only matches outputs, not reasoning process
- Teacher biases transferred

---

### Option B: Hidden State Distillation (More Complex)

Match intermediate representations, not just outputs.

```
Loss = α * CE(output_teacher, output_student)
     + β * KL(hidden_teacher || hidden_student)
     + γ * KL(policy_teacher || policy_student)  # for DPO
```

**Pros:**
- Transfers reasoning patterns, not just answers
- Better capability transfer

**Cons:**
- Requires teacher hidden state access (not available for API models)
- Custom training loop needed
- More compute-intensive

---

### Option C: DPO (Direct Preference Optimization)

Train on teacher preference rankings.

```
1. Generate multiple student responses
2. Teacher ranks: response_A > response_B
3. DPO loss optimizes for teacher preferences
```

**Pros:**
- Aligns with teacher's judgment, not just outputs
- More sample-efficient than SFT

**Cons:**
- Requires ranking data (more API calls)
- DPO training is less stable than SFT

---

## Recommended Implementation Path

**Phase 1: Output Distillation (SFT)**
- Start with simple (instruction, teacher_response) pairs
- Use existing LoRA training pipeline
- Prove the concept with minimal complexity

**Phase 2: Add DPO Option**
- Extend pipeline to collect preference data
- Add DPO training mode (TRL supports this)
- Compare SFT vs. DPO distilled adapters

**Phase 3: Hybrid Approach**
- SFT on teacher outputs for base capability
- DPO for alignment/refinement
- Optional hidden state distillation for local teachers

---

## Training Data Format

### SFT Format (Option A)

```jsonl
{
  "instruction": "How do I authenticate WebSocket connections with bearer tokens?",
  "input": "",
  "output": "To authenticate WebSocket connections using bearer tokens, you should...",
  "metadata": {
    "teacher_model": "claude-opus-4-5-20251101",
    "source": "agent_research",
    "session_id": "session_12345",
    "domain": "api_research",
    "quality_score": 0.92
  }
}
```

### DPO Format (Option B)

```jsonl
{
  "prompt": "How do I authenticate WebSocket connections with bearer tokens?",
  "chosen": "To authenticate WebSocket connections using bearer tokens...",
  "rejected": "You can auth WebSockets by adding headers...",
  "metadata": {
    "ranker": "claude-opus-4-5-20251101",
    "domain": "api_research"
  }
}
```

---

## Technical Requirements

### Dependencies (already in primary pipeline)

| Package | Purpose |
|---------|---------|
| `transformers` | Model loading, tokenization |
| `peft` | LoRA adapter training |
| `trl` | `SFTTrainer` and `DPOTrainer` |
| `accelerate` | Device management, mixed precision |
| `bitsandbytes` | 4-bit quantization (optional) |

### Additional Requirements

| Component | Needed For |
|-----------|------------|
| Teacher API integration | Claude/GPT-4 API access for generating training data |
| Response caching | Avoid re-generating teacher responses |
| Quality filtering | Filter low-quality teacher responses |
| Cost tracking | Monitor API costs for distillation runs |

---

## Integration with Primary LoRA Pipeline

The distillation pipeline reuses most of the primary LoRA infrastructure:

| Component | Reused? | Notes |
|-----------|---------|-------|
| Dataset capture | ✅ Yes | Same JSONL format (different source) |
| Dataset storage | ✅ Yes | Same `~/.meept/learning/datasets/` structure |
| Training script | ✅ 80% | Same PEFT/TRL stack, different loss |
| Adapter storage | ✅ Yes | Same `~/.meept/adapters/{domain}/` structure |
| Adapter loading | ✅ Yes | Same runtime loading mechanism |
| Config generation | ✅ Yes | Same auto-generation script |

**New components needed:**
1. `scripts/generate_distillation_data.py` - Query teacher API, store responses
2. `scripts/train_distillation_lora.py` - SFT/DPO training wrapper
3. Teacher API client (Python) - Claude/GPT-4 integration

---

## Cost Estimation

### Example: Distilling 1000 examples from Claude

| Item | Cost |
|------|------|
| Claude API (1000 prompts × ~$0.015/prompt) | ~$15 |
| Training (local GPU, 3 hours) | ~$0 (electricity only) |
| **Total** | **~$15 per domain adapter** |

### Cost Reduction Strategies

1. **Batch queries**: Send multiple examples per API call
2. **Caching**: Reuse teacher responses for common queries
3. **Selective distillation**: Only distill high-value examples (quality > 0.8)
4. **Model tiering**: Use cheaper models (Claude Haiku) for simple examples

---

## Success Metrics

| Metric | Target |
|--------|--------|
| Student matches teacher quality | >85% on eval set |
| Training convergence | <6 hours per adapter |
| Adapter performance gain | +20% over base LFM2.5 |
| Cost per adapter | <$20 |

---

## Deferred Implementation Notes

### Why Deferred?

1. **Primary pipeline first**: Need working LoRA capture/training before extending
2. **API dependency**: Requires teacher model API access
3. **Cost consideration**: Distillation has ongoing API costs
4. **Complexity**: Adds another training mode to support

### Trigger Conditions for Implementation

Implement when:
- [ ] Primary LoRA pipeline is stable and producing useful adapters
- [ ] Have 500+ high-quality captured research examples
- [ ] Budget allocated for teacher API costs
- [ ] Clear performance gap between LFM2.5 and teacher models

---

## References

- Primary LoRA Pipeline: `.github/ISSUES/lora-learning-pipeline.md` (to be created)
- TRL Documentation: https://huggingface.co/docs/trl
- PEFT LoRA Guide: https://huggingface.co/docs/peft
- Knowledge Distillation Paper: Hinton et al. (2015) https://arxiv.org/abs/1503.02531
- DPO Paper: Rafailov et al. (2023) https://arxiv.org/abs/2305.18290

---

## Related Issues

- #0000 - LoRA Learning Pipeline (primary implementation)
- #0000 - Adapter Config Auto-Generation
- #0000 - Research Data Retention System
