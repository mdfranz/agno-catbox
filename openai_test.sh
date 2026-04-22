#!/bin/bash
# Test different OpenAI models with optimized configurations

# Ensure OPENAI_API_KEY is set in your environment before running
MODELS=("gpt-5-mini" "gpt-5-nano" "gpt-5.2")
export TITLE="OpenAI Model Comparison"

for MODEL in "${MODELS[@]}"; do
    echo "--- Configuring for $MODEL ---"
    
    # 1. Reset/Set defaults
    unset OPENAI_REASONING_EFFORT
    unset OPENAI_MAX_COMPLETION_TOKENS
    export AGENT_REASONING=true
    export OPENAI_TEMPERATURE=0.7

    # 2. Apply model-specific optimizations
    if [[ "$MODEL" == *"nano"* ]]; then
        # Nano: Ultra-fast, simple tasks. Disable reasoning overhead.
        export AGENT_REASONING=false
        export OPENAI_TEMPERATURE=0.0
    elif [[ "$MODEL" == *"mini"* ]]; then
        # Mini: Balanced. Use Agno's manual reasoning for better logic.
        export AGENT_REASONING=true
    elif [[ "$MODEL" == "gpt-5.2" ]]; then
        # GPT-5.2: Native reasoning. 
        # Disable Agno's manual loop and use native reasoning features.
        export AGENT_REASONING=false
        export OPENAI_REASONING_EFFORT=high
        export OPENAI_MAX_COMPLETION_TOKENS=8000
    fi

    ./run_test_suite.sh "$MODEL"
done
