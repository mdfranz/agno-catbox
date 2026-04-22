# Project Overview

Agno Skill Runner is a small Linux-focused execution framework for Agno skills. The repository currently maintains **two parallel runtimes**:

- `skill-runner`: namespace-based execution with a host-prepared Python environment
- `skill-runner-oci`: OCI-based execution with a locally built image and external runtime

Both runtimes share:

- the same skill directory format
- the same Python agent entrypoint in [`runner.py`](/home/mfranz/github/agno-catbox/runner.py)
- the same skill loader
- nearly identical top-level CLI flags

## Current repository goals

1. Provide a repeatable way to run Agno skills against local data.
2. Reduce accidental host credential exposure during agent execution.
3. Compare a hand-rolled namespace runner against a daemonless OCI runner.
4. Keep the codebase small enough to inspect and experiment with directly.

## What the project does today

- Loads skills from `./skills/<name>` or a direct path
- Creates per-run workspaces under a base workspace
- Copies optional input data into the run workspace
- Supports Gemini, OpenAI, Anthropic, and Ollama model IDs
- Applies per-skill timeout and memory settings
- Streams tool usage and generated code from the Python runner

## Important limits

- This is not a hardened sandbox for adversarial code
- Namespace mode still exposes host `/usr` for shared libraries
- Namespace mode falls back to PATH restriction if user namespaces are unavailable
- OCI mode does not currently enforce `allowed_commands`
- The environment filter is intentionally narrow, but it now includes the OpenAI tuning vars used by the helper scripts

## Recommended reading order

1. [`README.md`](/home/mfranz/github/agno-catbox/README.md)
2. [`QUICKSTART.md`](/home/mfranz/github/agno-catbox/QUICKSTART.md)
3. [`ARCHITECTURE.md`](/home/mfranz/github/agno-catbox/ARCHITECTURE.md)
4. [`DESIGN.md`](/home/mfranz/github/agno-catbox/DESIGN.md)

## Near-term priorities

- Keep the namespace and OCI docs aligned with the code
- Decide whether OCI should enforce `allowed_commands` or document that omission permanently
- Decide whether provider tuning env vars should be forwarded through the sandbox
- Expand coverage around workspace reuse, OCI bundle reuse, and degraded/fallback execution
