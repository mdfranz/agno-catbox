#!/bin/bash
# Wrapper for Gemini tests using OCI runner
export RUNNER_BIN="./skill-runner-oci"
./gemini_test.sh "$@"
