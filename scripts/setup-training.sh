#!/bin/bash
# Install training dependencies for LoRA adapter pipeline
# After creating this file, run: chmod +x scripts/setup-training.sh
set -euo pipefail

echo "=== Installing PyTorch and training dependencies ==="
pip install \
    torch torchvision torchaudio \
    transformers \
    peft \
    trl \
    accelerate \
    bitsandbytes \
    datasets

echo ""
echo "=== Verifying GPU availability ==="
# GPU is optional (CPU works but is very slow for training)
python -c "import torch; print(f'CUDA available: {torch.cuda.is_available()}')"

echo ""
echo "=== Training environment ready ==="
echo "Next steps:"
echo "  1. Inspect model architecture: python scripts/inspect_lfm_architecture.py"
echo "  2. Train an adapter:           python scripts/train_lora.py --model lfm2.5-8b --dataset <path> --output <path>"
echo "  3. Train all adapters:         bash scripts/train_all_adapters.sh"
