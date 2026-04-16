#!/bin/bash

set -e

WORKSPACE_DIR="./runs"

echo "Preparing workspace in $WORKSPACE_DIR..."
mkdir -p "$WORKSPACE_DIR"

VENV_DIR="$WORKSPACE_DIR/.venv"

if [ -d "$VENV_DIR" ]; then
    echo "Removing existing virtual environment..."
    rm -rf "$VENV_DIR"
fi

echo "Creating virtual environment using system python3..."
uv venv -p /usr/bin/python3 "$VENV_DIR"

echo "Installing dependencies..."
VIRTUAL_ENV="$VENV_DIR" uv pip install agno pyyaml google-genai google-generativeai polars orjson

echo "Environment prepared successfully in $VENV_DIR"
