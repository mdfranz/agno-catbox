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

for MODEL in "${MODELS[@]}"; do
    echo "=============================================="
    echo "TESTING MODEL: $MODEL"
    echo "=============================================="
    
    while IFS= read -r PROMPT || [ -n "$PROMPT" ]; do
        if [ -z "$PROMPT" ]; then continue; fi
        
        echo "PROMPT: $PROMPT"
        echo "----------------------------------------------"
        "$RUNNER_BIN" --skill "$SKILL" --prompt "$PROMPT" --model "$MODEL" --data "$DATA_DIR" --workspace "$WORKSPACE"
        echo "----------------------------------------------"
        echo ""
    done < "$PROMPTS_FILE"
    
    echo ""
done

echo "All Tests Completed."
