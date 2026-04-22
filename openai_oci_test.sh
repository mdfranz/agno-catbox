#!/bin/bash
# Wrapper for OpenAI tests using OCI runner
export RUNNER_BIN="./skill-runner-oci"
./openai_test.sh "$@"
