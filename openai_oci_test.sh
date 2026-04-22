#!/bin/bash
# Wrapper for OpenAI tests using OCI runner
export RUNNER_BIN="./skill-runner-oci"

# Set a base RUN_NAME so each model gets its own workspace, while prompts
# for that model reuse the same workspace directory and container bundle.
export RUN_NAME="openai-oci-test-$(date +%Y%m%d-%H%M)"

echo "Using shared OCI run directory: $RUN_NAME"
./openai_test.sh "$@"
