# Architecture

Agno Skill Runner currently has **two runtime implementations** behind a similar CLI and shared skill format:

- `skill-runner`: host-side namespace sandbox
- `skill-runner-oci`: OCI bundle execution using `crun` or `runc`

Both paths load the same `skill.yaml`, the same optional `SKILL.md`, and the same Python agent entrypoint in [`runner.py`](/home/mfranz/github/agno-catbox/runner.py).

## High-level flow

```text
CLI
  -> resolve skill directory
  -> create or reuse run workspace
  -> optionally copy --data into the workspace
  -> load skill.yaml
  -> choose runtime path
      namespace runner:
        -> resolve runner.py on host
        -> attempt namespace sandbox
        -> fallback to PATH restriction if namespaces fail
      OCI runner:
        -> resolve OCI image layout
        -> create or reuse OCI bundle
        -> launch via crun/runc
  -> stream agent output to stdout/stderr
  -> persist logs under the workspace
```

## Shared components

### Skill loading

[`internal/skill/loader.go`](/home/mfranz/github/agno-catbox/internal/skill/loader.go) looks for:

1. `./skills/<name>`
2. `<name>` as a direct directory path

[`internal/skill/types.go`](/home/mfranz/github/agno-catbox/internal/skill/types.go) parses:

- `name`
- `description`
- `allowed_commands`
- `max_memory`
- `timeout`
- `max_file_size_bytes`

### Run workspace

Both runners call [`CreateWorkspaceStructure()`](/home/mfranz/github/agno-catbox/internal/sandbox/mount.go#L98), which creates:

- `data/`
- `code/`
- `skills/`
- `output/`

If `--data` is set, the directory is copied into the run workspace before agent execution.

### Python layer

[`runner.py`](/home/mfranz/github/agno-catbox/runner.py) is the shared agent entrypoint. It:

- Loads `skill.yaml` and `SKILL.md`
- Chooses a backend from the model ID prefix
- Instantiates Agno `ShellTools` and `PythonTools`
- Streams output and tool activity
- Writes Python-side logs to `runner.log`

## Namespace runner path

Primary entrypoints:

- [`cmd/skill-runner/main.go`](/home/mfranz/github/agno-catbox/cmd/skill-runner/main.go)
- [`internal/runner/exec.go`](/home/mfranz/github/agno-catbox/internal/runner/exec.go)
- [`internal/sandbox/sandbox.go`](/home/mfranz/github/agno-catbox/internal/sandbox/sandbox.go)

### Execution model

The namespace runner:

1. Resolves `runner.py` from `--runner`, `SKILL_RUNNER_PY`, or next to the binary
2. Prepares a minimal rootfs with symlinked allowed commands
3. Copies `runner.py` and the skill directory into the rootfs
4. Attempts a re-exec into user, mount, and PID namespaces
5. Bind-mounts the run workspace into the sandbox
6. Executes Python from the base workspace virtualenv if `<base-workspace>/.venv/bin/python3` exists

If namespace bootstrap fails, it falls back to:

- environment filtering
- temporary symlink-based `PATH`
- host execution without filesystem isolation

### Isolation properties

Best case:

- filtered environment
- isolated PID view
- pivoted rootfs
- run workspace bind-mounted in
- best-effort cgroup limits

Known limitations:

- host `/usr` is still exposed in namespace mode for shared libraries
- `allowed_commands` is not a hard boundary because absolute host paths remain reachable
- outbound networking is not blocked
- cgroup enforcement depends on host support
- fallback mode leaves the host filesystem visible

## OCI runner path

Primary entrypoints:

- [`cmd/skill-runner-oci/main.go`](/home/mfranz/github/agno-catbox/cmd/skill-runner-oci/main.go)
- [`internal/ocirunner/exec.go`](/home/mfranz/github/agno-catbox/internal/ocirunner/exec.go)
- [`internal/oci/runner.go`](/home/mfranz/github/agno-catbox/internal/oci/runner.go)

### Execution model

The OCI runner:

1. Resolves the OCI image layout from `--oci-image-dir`, `SKILL_RUNNER_IMAGE_DIR`, or `<exe>/image`
2. Verifies the image layout and discovers `crun` or `runc`
3. Creates or reuses a bundle under `<run-workspace>/.bundle/<run-id>`
4. Unpacks the OCI image rootfs on first use
5. Generates an OCI `config.json`
6. Runs `python3 /runner.py <skill> <prompt> /.skill` inside the bundle

The OCI image contains the Python runtime and baked dependencies, so there is no host-side `--runner` resolution step.

### Isolation properties

The OCI path:

- mounts the run workspace at `/workspace`
- mounts the skill directory read-only at `/.skill`
- uses OCI namespaces for PID, mount, IPC, UTS, cgroup, and user isolation
- can add a network namespace with `--network-isolated`

Known limitations:

- `allowed_commands` from `skill.yaml` is not currently enforced
- the security profile is intentionally lightweight and not hardened
- behavior still depends on the host runtime and kernel

## Environment handling

Both runtime paths use the same filtered environment from [`SetupEnvironment()`](/home/mfranz/github/agno-catbox/internal/sandbox/mount.go#L17).

Current pass-through:

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

## Logging

Current logging is split across layers:

- Go layer:
  - `skill-runner.log`
  - `skill-runner-oci.log`
- Python layer:
  - `runner.log` in the run workspace

When `LOG_CURRENT` is set, the Go logs are written next to the executable instead of inside the base workspace.

## Repository map

```text
cmd/
  skill-runner/
  skill-runner-oci/
internal/
  runner/
  ocirunner/
  sandbox/
  oci/
  skill/
skills/
runner.py
Containerfile
requirements.txt
```
