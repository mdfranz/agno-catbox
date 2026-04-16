# Implementation Summary

## Architecture

```
cmd/skill-runner/main.go
  ├─ Detects re-exec child mode (sandbox.IsChildProcess)
  └─ Normal CLI: parses flags (including optional runner path) → runner.RunSkill()

internal/runner/exec.go
  └─ Loads skill config → creates workspace → sandbox.Runner.Run()

internal/sandbox/
  ├─ sandbox.go      Orchestrates execution; resolves runner.py; two paths:
  │                    runWithNamespaces (re-exec + pivot_root)
  │                    runWithoutNamespaces (symlink PATH fallback)
  ├─ namespace.go    Checks namespace support, sets CLONE flags + UID/GID maps
  ├─ rootfs.go       Builds minimal rootfs temp dir; resolves library dependencies
  ├─ child.go        Re-exec child: bind mounts, pivot_root, exec real command
  ├─ mount.go        Environment filtering, symlink-based command directory
  └─ cgroup.go       cgroups v2 resource limits (fail-closed error handling)

internal/skill/
  ├─ types.go        SkillConfig struct + memory/duration parsing
  └─ loader.go       YAML loading + validation

runner.py             Python/Agno agent orchestrator (exits non-zero on failure)
```

## Key Implementation Decisions

### Re-exec pattern for mount namespace setup

Go's `exec.Cmd` doesn't provide a hook between `clone()` and `exec()`. To set up bind mounts and pivot_root inside the new namespace, the binary re-executes itself with `_SKILL_RUNNER_SANDBOX_CHILD` set in the environment. The child detects this in `main()`, calls `sandbox.RunChild()` which does the mount work, then `syscall.Exec`s the real Python command.

### Symlink-based command whitelisting

Instead of adding directories like `/usr/bin` to PATH (which exposes every binary in that directory), we create a temp directory with symlinks pointing to only the resolved paths of allowed commands. This is the only directory in PATH.

### Process group cleanup

The child runs with `Setpgid: true` and `Pdeathsig: SIGKILL`. On timeout, we `kill(-pgid, SIGKILL)` to kill the entire process group, preventing subprocess escape.

### Graceful degradation

The runner tries namespace isolation first. If the kernel doesn't support unprivileged user namespaces, it falls back to symlink PATH + environment filtering. Each layer is independent:

| Layer | Namespace mode | Fallback mode |
|-------|---------------|---------------|
| Filesystem isolation | pivot_root to minimal rootfs | Not available |
| Command whitelist | /bin symlinks in rootfs | Symlink temp dir in PATH |
| Environment filtering | Applied | Applied |
| Process group controls | Applied | Applied |
| cgroups | Applied if available | Applied if available |
| Timeout | Applied | Applied |

## Runtime Dependencies

**Go binary**: statically compiled.

**Python runner script**:
- Resolved from `-runner`, `SKILL_RUNNER_PY`, or `runner.py` next to the binary

**Python environment** (in workspace):
- `agno` — agent framework
- `pyyaml` — config loading
- `google-genai`, `google-generativeai` — Gemini model support
- Optional: `polars`, `orjson` — data processing (skill-specific)

The Go binary does not install or manage Python dependencies. The workspace must have them pre-installed (via `uv venv` + `uv pip install`).

## Building

```bash
go build -o skill-runner ./cmd/skill-runner
```

## Testing

```bash
go test ./...
```
