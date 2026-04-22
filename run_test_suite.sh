#!/bin/bash
# Generic test suite runner for skill-runner

RUNNER_BIN="${RUNNER_BIN:-./skill-runner}"
# MODELS should be passed as arguments if not set as an array
if [ ${#MODELS[@]} -eq 0 ]; then
    MODELS=("$@")
fi

PROMPTS_FILE="${PROMPTS_FILE:-ollama-prompts.txt}"
SKILL="${SKILL:-suricata-analyst}"
DATA_DIR="${DATA_DIR:-data}"
WORKSPACE="${WORKSPACE:-runs}"
TITLE="${TITLE:-Model Comparison}"

sanitize_name() {
    echo "$1" | sed -E 's/[^A-Za-z0-9._-]+/-/g; s/^-+//; s/-+$//'
}

model_family() {
    local model="$1"

    if [[ "$model" == ollama/* ]]; then
        echo "ollama"
    elif [[ "$model" =~ ^(gpt-|o1|o3) ]]; then
        echo "openai"
    elif [[ "$model" == claude-* ]]; then
        echo "anthropic"
    elif [[ "$model" == gemini-* ]]; then
        echo "gemini"
    else
        echo "unknown"
    fi
}

if [ ! -f "$PROMPTS_FILE" ]; then
    echo "Error: $PROMPTS_FILE not found."
    exit 1
fi

if [ ${#MODELS[@]} -eq 0 ]; then
    echo "Error: No models specified."
    exit 1
fi

echo "Starting Multi-Prompt $TITLE..."
echo "----------------------------------------------"
echo "Using runner: $RUNNER_BIN"

SUITE_ID="$(date +%Y%m%d-%H%M%S)"
BASE_RUN_NAME="${RUN_NAME:-test-suite-$SUITE_ID}"
BASE_RUN_NAME="$(sanitize_name "$BASE_RUN_NAME")"

for MODEL in "${MODELS[@]}"; do
    MODEL_TAG="$(sanitize_name "$MODEL")"
    MODEL_FAMILY="$(model_family "$MODEL")"
    MODEL_RUN_NAME="${BASE_RUN_NAME}-${MODEL_TAG}"

    echo "=============================================="
    echo "TESTING MODEL: $MODEL"
    echo "MODEL FAMILY: $MODEL_FAMILY"
    echo "=============================================="
    echo "Workspace run name: $MODEL_RUN_NAME"
    
    while IFS= read -r PROMPT || [ -n "$PROMPT" ]; do
        if [ -z "$PROMPT" ]; then continue; fi
        
        echo "PROMPT: $PROMPT"
        echo "----------------------------------------------"
        START_NS="$(date +%s%N)"
        $RUNNER_BIN --skill "$SKILL" --prompt "$PROMPT" --model "$MODEL" --data "$DATA_DIR" --workspace "$WORKSPACE" --run-name "$MODEL_RUN_NAME"
        EXIT_CODE=$?
        END_NS="$(date +%s%N)"

        if [[ "$START_NS" =~ ^[0-9]+$ && "$END_NS" =~ ^[0-9]+$ ]]; then
            ELAPSED="$(awk -v s="$START_NS" -v e="$END_NS" 'BEGIN { printf "%.2fs", (e-s)/1000000000 }')"
        else
            ELAPSED="unknown"
        fi

        echo "Run time: $ELAPSED (family: $MODEL_FAMILY)"
        if [ "$EXIT_CODE" -ne 0 ]; then
            echo "Run exited with status: $EXIT_CODE"
        fi
        echo "----------------------------------------------"
        echo ""
    done < "$PROMPTS_FILE"
    
    echo ""
done

echo "All Tests Completed."
