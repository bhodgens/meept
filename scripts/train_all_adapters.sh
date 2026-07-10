#!/bin/bash
# Train adapters for all domains and both model sizes.
# Skips domains where no dataset file exists.
# Versions adapters as {model}-vN so re-runs never overwrite prior adapters.
# Usage: bash scripts/train_all_adapters.sh
set -euo pipefail

DOMAINS=("code" "debugging" "api_research" "security" "meept_internal" "personal")
MODELS=("lfm2.5-8b" "lfm2.5-1.2b")
ADAPTERS_ROOT="${MEEPT_ADAPTERS_DIR:-$HOME/.meept/adapters}"
DATASETS_ROOT="${MEEPT_DATASETS_DIR:-$HOME/.meept/learning/datasets}"

next_version() {
    local domain="$1"
    local model="$2"
    local domain_dir="$ADAPTERS_ROOT/$domain"
    local max=0
    if [ ! -d "$domain_dir" ]; then
        echo 1
        return
    fi
    local prefix="${model}-v"
    for d in "$domain_dir"/${prefix}*; do
        [ -d "$d" ] || continue
        local base
        base="$(basename "$d")"
        local num="${base#${prefix}}"
        if [[ "$num" =~ ^[0-9]+$ ]] && [ "$num" -gt "$max" ]; then
            max=$num
        fi
    done
    echo $((max + 1))
}

for domain in "${DOMAINS[@]}"; do
    DATASET="$DATASETS_ROOT/${domain}.jsonl"
    if [ ! -f "$DATASET" ]; then
        echo "Skipping $domain: dataset not found"
        continue
    fi

    for model in "${MODELS[@]}"; do
        ver="$(next_version "$domain" "$model")"
        OUTPUT="$ADAPTERS_ROOT/${domain}/${model}-v${ver}"
        mkdir -p "$OUTPUT"
        echo "Training $domain for $model (v${ver})..."
        python scripts/train_lora.py \
            --model "$model" \
            --dataset "$DATASET" \
            --output "$OUTPUT" \
            --epochs 3

        # Write training metadata and regenerate registry
        if [ -x hooks/on_adapter_trained.sh ] || [ -f hooks/on_adapter_trained.sh ]; then
            bash hooks/on_adapter_trained.sh "$domain" "$model" "$OUTPUT" "$ADAPTERS_ROOT" "$DATASETS_ROOT"
        fi
    done
done

echo "=== All applicable adapters trained ==="
