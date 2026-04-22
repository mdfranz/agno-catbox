#!/bin/bash
# Test different OpenAI models with multiple prompts from a file

# Ensure OPENAI_API_KEY is set in your environment before running
MODELS=("gpt-5-mini" "gpt-5-nano" "gpt-5.2")
PROMPTS_FILE="ollama-prompts.txt"
SKILL="suricata-analyst"
DATA_DIR="data"
WORKSPACE="runs"

if [ ! -f "$PROMPTS_FILE" ]; then
    echo "Error: $PROMPTS_FILE not found."
    exit 1
fi

echo "Starting Multi-Prompt OpenAI Model Comparison..."
echo "----------------------------------------------"

for MODEL in "${MODELS[@]}"; do
    echo "=============================================="
    echo "TESTING MODEL: $MODEL"
    echo "=============================================="
    
    while IFS= read -r PROMPT || [ -n "$PROMPT" ]; do
        if [ -z "$PROMPT" ]; then continue; fi
        
        echo "PROMPT: $PROMPT"
        echo "----------------------------------------------"
        # The runner.py logic handles gpt- prefixes by using OpenAIChat
        ./skill-runner --skill "$SKILL" --prompt "$PROMPT" --model "$MODEL" --data "$DATA_DIR" --workspace "$WORKSPACE"
        echo "----------------------------------------------"
        echo ""
    done < "$PROMPTS_FILE"
    
    echo ""
done

echo "All Tests Completed."
