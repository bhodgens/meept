#!/bin/bash
# Post-training hook: writes training_meta.json and regenerates adapter registry.
# Called after each successful adapter training run.
#
# Usage: bash hooks/on_adapter_trained.sh <DOMAIN> <MODEL> <OUTPUT_PATH> [ADAPTERS_DIR] [DATASETS_DIR]
# Example: bash hooks/on_adapter_trained.sh code lfm2.5-8b ~/.meept/adapters/code/lfm2.5-8b-v1
set -euo pipefail

if [ $# -lt 3 ]; then
    echo "Usage: $0 <DOMAIN> <MODEL> <OUTPUT_PATH> [ADAPTERS_DIR] [DATASETS_DIR]" >&2
    echo "" >&2
    echo "Arguments:" >&2
    echo "  DOMAIN       Training domain (e.g. code, debugging, api_research)" >&2
    echo "  MODEL        Model identifier (e.g. lfm2.5-8b, lfm2.5-1.2b)" >&2
    echo "  OUTPUT_PATH  Directory where the adapter was saved" >&2
    echo "  ADAPTERS_DIR Optional adapters root (default: ~/.meept/adapters)" >&2
    echo "  DATASETS_DIR Optional datasets dir for MD5 (default: ~/.meept/learning/datasets)" >&2
    exit 1
fi

DOMAIN="$1"
MODEL="$2"
OUTPUT_PATH="$3"
ADAPTERS_DIR="${4:-$HOME/.meept/adapters}"
DATASETS_DIR="${5:-$HOME/.meept/learning/datasets}"

if [ ! -d "$OUTPUT_PATH" ]; then
    echo "Error: output path does not exist: $OUTPUT_PATH" >&2
    exit 1
fi

# Save training metadata
cat > "$OUTPUT_PATH/training_meta.json" << EOF
{
  "domain": "$DOMAIN",
  "model": "$MODEL",
  "dataset": "${DOMAIN}.jsonl",
  "trained_at": "$(date -Iseconds)"
}
EOF

echo "Wrote training_meta.json to $OUTPUT_PATH"

# Regenerate adapter registry next to adapters parent (daemon load path).
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
GEN_SCRIPT="$PROJECT_ROOT/scripts/generate_adapter_config.py"
if [ -f "$GEN_SCRIPT" ]; then
    REGISTRY_OUT="$(dirname "$ADAPTERS_DIR")/adapter_registry.json"
    python "$GEN_SCRIPT" \
        --adapters-dir "$ADAPTERS_DIR" \
        --output "$REGISTRY_OUT" \
        --datasets-dir "$DATASETS_DIR"
else
    echo "Warning: generate_adapter_config.py not found, skipping registry regeneration" >&2
fi
