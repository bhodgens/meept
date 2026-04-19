# Shadow Training

## Overview
Shadow training enables Meept to learn from its operations by executing tasks in parallel with a teacher model, filtering high-quality examples, and exporting training data for model improvement.

## Problem
Static agent behavior limits adaptability. Shadow training provides:
- Continuous learning from operations
- Quality-based training data selection
- Exportable training datasets
- Model improvement through experience

## Behavior

### Parallel Execution
- **Teacher Model**: High-quality model executes same task
- **Comparison**: Student and teacher outputs compared
- **Quality Scoring**: Outputs evaluated for training suitability
- **Filtering**: Only high-quality examples retained

### Training Data Export
- **JSONL Format**: Standard training data format
- **DPO Support**: Direct Preference Optimization format
- **Quality Thresholds**: Configurable minimum quality scores
- **Metadata**: Rich context for training examples

### Trajectory Learning
- **JUDGE Phase**: Evaluate trajectory quality
- **DISTILL Phase**: Extract reusable patterns
- **CONSOLIDATE Phase**: Merge into knowledge base
- **Adaptive Learning**: Patterns applied to future tasks

### Quality Filtering
- **High-Quality Threshold**: 0.85 (excellent examples)
- **Trainable Threshold**: 0.6 (usable for training)
- **Automatic Filtering**: Low-quality examples discarded
- **Manual Review**: Option for human validation

## Configuration

```toml
[shadow]
enabled = false
data_dir = "~/.meept/shadow"

[shadow.teacher]
model = "claude-opus-4-5-20251101"
max_daily_queries = 500
max_daily_cost = 10.0

[shadow.quality]
high_quality_threshold = 0.85
trainable_threshold = 0.6

[shadow.export]
output_dir = "~/.meept/shadow/exports"
formats = ["jsonl", "dpo"]
max_examples_per_file = 1000

[shadow.trajectory]
enabled = true
judge_model = "claude-opus-4-5-20251101"
distill_model = "claude-sonnet-4-5-20241022"
consolidate_batch_size = 100
```

## Observability

### Logging
- Shadow execution events
- Quality scoring results
- Export operations
- Trajectory learning phases

### Metrics
- Parallel execution success rate
- Quality score distribution
- Export file sizes
- Learning pattern effectiveness

### Debug Info
- Active teacher model
- Quality filter settings
- Export format availability
- Trajectory learning progress

## Edge Cases

### Teacher Model Unavailable
- Shadow training paused
- Quality degradation detection
- Alternative teacher models considered

### Quality Scoring Disagreement
- Multiple scoring mechanisms
- Consensus-based decision
- Manual review option

### Export File Size Limits
- Automatic file splitting
- Compression for large datasets
- Storage management

### Training Data Bias
- Diversity monitoring
- Bias detection algorithms
- Balanced dataset creation