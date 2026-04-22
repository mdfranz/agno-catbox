# Agno Skill Runner

Agno Skill Runner is a Linux-first tool for running Agno skills with lightweight isolation. The repository currently ships **two execution paths** that share the same skill format, Python runner, and CLI shape:

- `skill-runner`: namespace-based sandboxing with a copied `runner.py` and a minimal rootfs assembled on the host.
- `skill-runner-oci`: daemonless OCI execution using a locally built image plus `crun` or `runc`.

This project is best treated as an **execution guardrail for trusted workflows**, not a hardened container boundary. It helps reduce accidental exposure of host credentials and filesystem state, but it does not claim strong containment against adversarial code or prompts.

## Current capabilities

- Skill loading from `./skills/<name>` or a direct skill directory path
- Per-run workspaces under a base workspace directory
- Optional data staging via `--data`
- Model selection for Gemini, OpenAI, Anthropic, and Ollama IDs
- Per-skill timeout and memory settings from `skill.yaml`
- Shared Python agent runner in [`runner.py`](/home/mfranz/github/agno-catbox/runner.py)

## Prerequisites

### Common

- Linux
- Go 1.21+
- One provider credential, depending on the model you plan to use:
  - `GEMINI_API_KEY` or `GOOGLE_API_KEY`
  - `OPENAI_API_KEY`
  - `ANTHROPIC_API_KEY`
  - `OLLAMA_HOST` for Ollama

### Namespace runner

- Unprivileged user namespaces enabled if you want namespace isolation
- Python 3
- `uv` for preparing the workspace venv used by `runner.py`

### OCI runner

- `buildah` to build the OCI image
- `crun` preferred, or `runc`

## Build

```bash
make build        # skill-runner
make build-oci    # skill-runner-oci
make build-all    # both binaries
make image        # build ./image OCI layout for skill-runner-oci
```

The namespace binary resolves `runner.py` in this order:

1. `--runner /path/to/runner.py`
2. `SKILL_RUNNER_PY=/path/to/runner.py`
3. `runner.py` next to the `skill-runner` binary

The OCI binary does not resolve a host-side runner script. It always uses the `runner.py` baked into the OCI image.

## Quick start

### Namespace runner

```bash
export GEMINI_API_KEY="your-key"
make build
./prep.sh

./skill-runner \
  --skill suricata-analyst \
  --prompt "Analyze eve.json for suspicious DNS and TLS traffic" \
  --workspace ./runs \
  --data ./data
```

`./prep.sh` creates `./runs/.venv` and installs the Python packages expected by [`runner.py`](/home/mfranz/github/agno-catbox/runner.py). The namespace runner will use that base-workspace virtualenv automatically when it exists.

### OCI runner

```bash
export GEMINI_API_KEY="your-key"
make build-oci
make image

./skill-runner-oci doctor
./skill-runner-oci \
  --skill suricata-analyst \
  --prompt "Analyze eve.json for suspicious DNS and TLS traffic" \
  --workspace ./runs \
  --data ./data
```

Use `--network-isolated` to add a dedicated network namespace for the OCI path when you want outbound network disabled.

## CLI notes

Both binaries expose the same core flags:

- `--skill`, `-s`: skill name or direct path
- `--prompt`, `-p`: task for the agent
- `--model`, `-m`: model ID, default `gemini-3.1-flash-lite-preview`
- `--workspace`, `-w`: base workspace directory
- `--data`, `-d`: directory copied into the run workspace before execution
- `--run-name`: fixed workspace name for reruns or comparison runs
- `--debug`: verbose logging

OCI-only flags:

- `--oci-image-dir`: override the OCI image layout location
- `--runtime`: force `crun` or `runc`
- `--network-isolated`: create a network namespace instead of using host networking

## Workspace and logs

Each run creates or reuses a subdirectory under the base workspace:

- Namespace runner default: `run-<skill>-<timestamp>`
- OCI runner default: `run-oci-<skill>-<timestamp>`
- With `--run-name`, both runners reuse `<workspace>/<run-name>`

The runners create these subdirectories in each run workspace:

- `data/`
- `code/`
- `skills/`
- `output/`

Logging currently lands in two places:

- Base workspace:
  - [`skill-runner.log`](/home/mfranz/github/agno-catbox/skill-runner.log) for the namespace path
  - [`skill-runner-oci.log`](/home/mfranz/github/agno-catbox/skill-runner-oci.log) for the OCI path
- Run workspace:
  - `runner.log` from the Python layer

In OCI mode, the reusable runtime bundle lives under `<run-workspace>/.bundle/`.

## Skill format

Minimal `skill.yaml`:

```yaml
name: suricata-analyst
description: Analyze Suricata EVE JSON logs
allowed_commands:
  - python3
  - uv
  - jq
  - grep
  - cat
max_memory: 1G
timeout: 300s
```

Optional `SKILL.md` content is loaded and passed to the Agno agent as the main instruction block.

`allowed_commands` currently matters only for the namespace-based runner. The OCI path does not yet apply command whitelisting from `skill.yaml`.

## Model support

[`runner.py`](/home/mfranz/github/agno-catbox/runner.py) chooses the model backend from the model ID prefix:

- `gpt-*`, `o1*`, `o3*` -> OpenAI
- `claude-*` -> Anthropic
- `ollama/*` -> Ollama
- anything else -> Gemini

Current environment pass-through is intentionally narrow. The sandbox forwards only:

- `GOOGLE_API_KEY`
- `GEMINI_API_KEY`
- `OPENAI_API_KEY`
- `OPENAI_REASONING_EFFORT`
- `OPENAI_MAX_COMPLETION_TOKENS`
- `OPENAI_TEMPERATURE`
- `ANTHROPIC_API_KEY`
- `AGENT_REASONING`
- `OLLAMA_HOST`
- `PATH`

## Security boundaries

### Namespace runner

What it does:

- Filters the environment before launching the agent
- Attempts user, mount, and PID namespace isolation
- Copies the skill into the sandbox rootfs
- Uses per-skill timeout and best-effort cgroup limits
- Restricts `PATH` to symlinked allowed commands in fallback mode

What it does not do:

- It does not block outbound network access
- It does not provide strong command whitelisting because host `/usr` is still exposed in namespace mode
- It does not guarantee cgroup enforcement on every host
- It falls back to PATH-only restriction if namespace setup is unavailable

### OCI runner

What it does:

- Runs inside a locally built OCI rootfs
- Binds the run workspace at `/workspace`
- Binds the skill read-only at `/.skill`
- Can optionally isolate networking with `--network-isolated`

What it does not do:

- It does not currently enforce `allowed_commands`
- It is not a hardened container profile
- It still depends on the local OCI runtime and host kernel behavior

## Project layout

```text
cmd/
  skill-runner/        namespace-runner CLI
  skill-runner-oci/    OCI-runner CLI
internal/
  runner/              namespace orchestration
  ocirunner/           OCI orchestration
  sandbox/             namespace sandbox implementation
  oci/                 OCI image, bundle, spec, and runtime helpers
  skill/               skill config loading
runner.py              shared Python/Agno runner
skills/                bundled skills
Containerfile          OCI image definition
requirements.txt       Python deps baked into the OCI image
```

## Helper scripts

The repo includes provider-specific wrapper scripts such as [`openai_test.sh`](/home/mfranz/github/agno-catbox/openai_test.sh), [`gemini_test.sh`](/home/mfranz/github/agno-catbox/gemini_test.sh), [`ollama_test.sh`](/home/mfranz/github/agno-catbox/ollama_test.sh), and [`run_test_suite.sh`](/home/mfranz/github/agno-catbox/run_test_suite.sh) for repeated prompt comparisons.

These scripts are useful for local benchmarking, but note that some environment-based model tuning shown in the wrappers is not currently passed through the sandbox.
