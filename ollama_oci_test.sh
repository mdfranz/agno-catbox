#!/bin/bash
# Wrapper for Ollama tests using OCI runner
export RUNNER_BIN="./skill-runner-oci"
./ollama_test.sh "$@"
