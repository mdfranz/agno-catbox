#!/bin/bash
# Test different Ollama models with multiple prompts from a file

export OLLAMA_HOST="http://100.74.199.13:11434"
MODELS=("ollama/cogito:14b" "ollama/llama3.1:8b" "ollama/qwen2.5:14b" "ollama/gpt-oss:20b")
PROMPTS_FILE="ollama-prompts.txt"
SKILL="suricata-analyst"
DATA_DIR="data"
WORKSPACE="runs"

if [ ! -f "$PROMPTS_FILE" ]; then
    echo "Error: $PROMPTS_FILE not found."
    exit 1
fi

echo "Starting Multi-Prompt Ollama Model Comparison..."
echo "----------------------------------------------"

for MODEL in "${MODELS[@]}"; do
    echo "=============================================="
    echo "TESTING MODEL: $MODEL"
    echo "=============================================="
    
    while IFS= read -r PROMPT || [ -n "$PROMPT" ]; do
        if [ -z "$PROMPT" ]; then continue; fi
        
        echo "PROMPT: $PROMPT"
        echo "----------------------------------------------"
        ./skill-runner --skill "$SKILL" --prompt "$PROMPT" --model "$MODEL" --data "$DATA_DIR" --workspace "$WORKSPACE"
        echo "----------------------------------------------"
        echo ""
    done < "$PROMPTS_FILE"
    
    echo ""
done

echo "All Tests Completed."
