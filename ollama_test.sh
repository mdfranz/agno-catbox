#!/bin/bash
# Test different Ollama models with multiple prompts from a file

export OLLAMA_HOST="http://100.74.199.13:11434"
MODELS=("ollama/gemma4-claude:latest" "ollama/cogito:14b" "ollama/qwen2.5:14b" "ollama/gpt-oss:20b")
export TITLE="Ollama Model Comparison"

./run_test_suite.sh "${MODELS[@]}"
