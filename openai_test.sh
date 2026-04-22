#!/bin/bash
# Test different OpenAI models with multiple prompts from a file

# Ensure OPENAI_API_KEY is set in your environment before running
MODELS=("gpt-5-mini" "gpt-5-nano" "gpt-5.2")
export TITLE="OpenAI Model Comparison"

./run_test_suite.sh "${MODELS[@]}"
