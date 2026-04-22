# Implementation Summary

This repository now contains two runnable implementations that share the same skill model and Python agent layer.

## Executables

### `skill-runner`

Namespace-based path:

```text
cmd/skill-runner/main.go
  -> parses flags
  -> resolves run workspace and logging
  -> runner.RunSkill()

internal/runner/exec.go
  -> loads skill config
  -> creates workspace structure
  -> copies --data into the run workspace
  -> sandbox.Runner.Run()

internal/sandbox/
  sandbox.go
    -> runWithNamespaces()
    -> runWithoutNamespaces()
  namespace.go
    -> namespace availability and clone setup
  rootfs.go
    -> minimal rootfs prep and bind plan
  child.go
    -> re-exec child mount setup and exec
  mount.go
    -> environment filtering and PATH helpers
  cgroup.go
    -> best-effort cgroup v2 limits
```

### `skill-runner-oci`

OCI-based path:

```text
cmd/skill-runner-oci/main.go
  -> parses flags
  -> resolves image path, runtime, logging, workspace
  -> ocirunner.RunSkill()

internal/ocirunner/exec.go
  -> loads skill config
  -> creates workspace structure
  -> copies --data into the run workspace
  -> assembles filtered env
  -> oci.Run()

internal/oci/
  image.go
    -> image-layout discovery and validation
  bundle.go
    -> reusable per-run bundle directory
  rootfs.go
    -> image unpack into rootfs
  spec.go
    -> OCI config.json generation
  runtime.go
    -> crun/runc invocation, timeout, cleanup
  runner.go
    -> top-level OCI orchestration
```

## Shared pieces

```text
internal/skill/
  loader.go  -> skill discovery and YAML load
  types.go   -> parsed config values

runner.py
  -> loads SKILL.md + skill.yaml
  -> picks model backend from model ID prefix
  -> creates Agno Agent with ShellTools + PythonTools
  -> streams output and writes runner.log
```

## Runtime behavior that matters

- Skill lookup checks `./skills/<name>` first, then a direct path.
- Workspace setup always creates `data/`, `code/`, `skills/`, and `output/`.
- `--run-name` switches from timestamped workspace names to a fixed reusable workspace.
- Namespace mode will use `<base-workspace>/.venv/bin/python3` if it exists.
- OCI mode reuses `<run-workspace>/.bundle/<run-id>` when the run ID is stable.

## Known implementation gaps

- `allowed_commands` is only applied in the namespace path.
- Even in namespace mode, `allowed_commands` is not a hard boundary because `/usr` remains reachable.
- The sandbox environment filter forwards credentials, `OPENAI_REASONING_EFFORT`, `OPENAI_MAX_COMPLETION_TOKENS`, `OPENAI_TEMPERATURE`, `AGENT_REASONING`, `OLLAMA_HOST`, and `PATH`.

## Verification entrypoints

```bash
go test ./...
./skill-runner --help
./skill-runner-oci --help
./skill-runner-oci doctor
```
