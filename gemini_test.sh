#!/bin/bash
# Test different Gemini models with multiple prompts from a file

# Ensure GEMINI_API_KEY (or GOOGLE_API_KEY) is set in your environment before running
MODELS=("gemini-2.5-pro" "gemini-3.1-pro-preview" "gemini-3-flash-preview")
export TITLE="Gemini Model Comparison"

./run_test_suite.sh "${MODELS[@]}"
