#!/bin/bash
# Switch the classifier model configuration

set -e

MODEL="$1"
CONFIG_FILE="$HOME/.meept/models.json5"

if [ -z "$MODEL" ]; then
    echo "Usage: $0 <model-name>"
    echo "Available models:"
    echo "  - combined-sft"
    echo "  - thinking-claude"
    exit 1
fi

case "$MODEL" in
    "combined-sft")
        MODEL_REF="local/lfm-combined-sft"
        ;;
    "thinking-claude")
        MODEL_REF="local/lfm-thinking-claude"
        ;;
    *)
        echo "Unknown model: $MODEL"
        exit 1
        ;;
esac

# Update the small_model and classifier_model settings
sed -i '' "s/\"small_model\":.*/\"small_model\": \"$MODEL_REF\",/" "$CONFIG_FILE"
sed -i '' "s/\"classifier_model\":.*/\"classifier_model\": \"$MODEL_REF\",/" "$CONFIG_FILE"

echo "Switched classifier model to: $MODEL_REF"
