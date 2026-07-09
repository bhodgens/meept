#!/bin/bash
# Train adapters for all domains and both model sizes.
# Skips domains where no dataset file exists.
# Usage: bash scripts/train_all_adapters.sh
set -euo pipefail

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

        # Write training metadata and regenerate registry
        if [ -x hooks/on_adapter_trained.sh ]; then
            bash hooks/on_adapter_trained.sh "$domain" "$model" "$OUTPUT"
        fi
    done
done

echo "=== All applicable adapters trained ==="
