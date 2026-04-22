# Quick Start

This guide covers the current repository workflow for both shipped runners:

- `skill-runner`: namespace-based execution
- `skill-runner-oci`: OCI-based execution

## 1. Build

```bash
make build-all
```

If you only need one binary:

```bash
make build
make build-oci
```

## 2. Set credentials

Export the credential that matches your model family:

```bash
export GEMINI_API_KEY="your-key"
# or export GOOGLE_API_KEY="your-key"
# or export OPENAI_API_KEY="your-key"
# or export ANTHROPIC_API_KEY="your-key"
```

For Ollama:

```bash
export OLLAMA_HOST="http://localhost:11434"
```

## 3. Prepare input data

Place your analysis inputs in a directory you can pass with `--data`. The runner copies that directory into the run workspace before execution.

Example:

```bash
mkdir -p ./data
cp /path/to/eve.json ./data/
```

## 4. Namespace runner setup

The namespace path expects a Python environment in the **base workspace**. The repository helper script prepares that for `./runs`.

```bash
./prep.sh
```

That creates:

- `./runs/.venv`
- Python dependencies required by [`runner.py`](/home/mfranz/github/agno-catbox/runner.py)

Run the bundled skill:

```bash
./skill-runner \
  --skill suricata-analyst \
  --prompt "Analyze eve.json for suspicious DNS and TLS traffic" \
  --workspace ./runs \
  --data ./data
```

Useful notes:

- Default model: `gemini-3.1-flash-lite-preview`
- Skill discovery checks `./skills/<name>` first, then a direct path
- `runner.py` resolves from `--runner`, then `SKILL_RUNNER_PY`, then next to the binary

## 5. OCI runner setup

Build the local OCI image and validate prerequisites:

```bash
make image
./skill-runner-oci doctor
```

Run the same skill with the OCI path:

```bash
./skill-runner-oci \
  --skill suricata-analyst \
  --prompt "Analyze eve.json for suspicious DNS and TLS traffic" \
  --workspace ./runs \
  --data ./data
```

To disable outbound networking in OCI mode:

```bash
./skill-runner-oci \
  --skill suricata-analyst \
  --prompt "Analyze eve.json" \
  --workspace ./runs \
  --data ./data \
  --network-isolated
```

## 6. Reuse a workspace

Use `--run-name` when you want deterministic workspace paths across reruns:

```bash
./skill-runner \
  --skill suricata-analyst \
  --prompt "Summarize rare SNI activity" \
  --workspace ./runs \
  --data ./data \
  --run-name suricata-baseline
```

The same flag works with `skill-runner-oci`.

## 7. Inspect results

Current logs and artifacts show up in three places:

- Base workspace:
  - `skill-runner.log`
  - `skill-runner-oci.log`
- Run workspace:
  - `runner.log`
  - generated scripts, outputs, and copied data

Default workspace names:

- Namespace: `run-<skill>-<timestamp>`
- OCI: `run-oci-<skill>-<timestamp>`

## 8. Current behavior to be aware of

- Namespace isolation is best-effort. If unprivileged namespaces are unavailable, `skill-runner` falls back to PATH restriction and environment filtering.
- `allowed_commands` is only applied by the namespace runner today.
- The sandbox forwards provider credentials plus `OPENAI_REASONING_EFFORT`, `OPENAI_MAX_COMPLETION_TOKENS`, `OPENAI_TEMPERATURE`, `AGENT_REASONING`, `OLLAMA_HOST`, and `PATH`.
