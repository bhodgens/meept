#!/bin/bash
# Start MLX server with specified model

MODEL_PATH="$1"
if [ -z "$MODEL_PATH" ]; then
    echo "Usage: $0 <model-path>"
    echo "Example: $0 /Volumes/LLMs/lfm2.5-1.2b-combined-serialized-sft"
    exit 1
fi

if [ ! -d "$MODEL_PATH" ]; then
    echo "Error: Model path does not exist: $MODEL_PATH"
    exit 1
fi

echo "Starting MLX server with model: $MODEL_PATH"
echo "Server will be available at http://127.0.0.1:8080"

# Run mlx_lm.server with the specified model
mlx_lm.server --model "$MODEL_PATH" --port 8080
