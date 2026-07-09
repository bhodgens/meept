#!/bin/bash
# Post-training hook: writes training_meta.json and regenerates adapter registry.
# Called after each successful adapter training run.
#
# Usage: bash hooks/on_adapter_trained.sh <DOMAIN> <MODEL> <OUTPUT_PATH>
# Example: bash hooks/on_adapter_trained.sh code lfm2.5-8b ~/.meept/adapters/code/lfm2.5-8b-v1
set -euo pipefail

if [ $# -ne 3 ]; then
    echo "Usage: $0 <DOMAIN> <MODEL> <OUTPUT_PATH>" >&2
    echo "" >&2
    echo "Arguments:" >&2
    echo "  DOMAIN      Training domain (e.g. code, debugging, api_research)" >&2
    echo "  MODEL       Model identifier (e.g. lfm2.5-8b, lfm2.5-1.2b)" >&2
    echo "  OUTPUT_PATH Directory where the adapter was saved" >&2
    exit 1
fi

DOMAIN="$1"
MODEL="$2"
OUTPUT_PATH="$3"

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

# Regenerate adapter registry
# Run from project root if scripts/ is available
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
if [ -f "$PROJECT_ROOT/scripts/generate_adapter_config.py" ]; then
    python "$PROJECT_ROOT/scripts/generate_adapter_config.py"
else
    echo "Warning: generate_adapter_config.py not found, skipping registry regeneration" >&2
fi
