# Design & Architecture

## Overview

The Agno Skill Runner is a Go binary that executes Python-based Agno AI agent skills inside a sandboxed Linux process. It uses unprivileged Linux namespaces for filesystem isolation, symlink-based PATH restriction for command whitelisting, cgroups v2 for resource limits, and process group controls for reliable cleanup.

## Design Goals

1. **Real isolation**: The agent process should not be able to see or access host files outside the workspace
2. **No root required**: Uses unprivileged user namespaces — no Docker, no setuid, no capabilities
3. **Defense in depth**: Multiple independent layers so failure of one doesn't compromise all security
4. **Graceful degradation**: Falls back to reduced isolation on systems without namespace support

## Process Flow

```
User: ./skill-runner -skill X -prompt "..."
  │
  ├─ Load skill.yaml (allowed_commands, limits)
  ├─ Prepare minimal rootfs (temp dir with command symlinks)
  ├─ Re-exec self with CLONE_NEWUSER | CLONE_NEWNS | CLONE_NEWPID
  │
  └─ Child process (in new namespaces):
       ├─ Bind-mount: /usr (ro), /lib (ro), /lib64 (ro), workspace (rw)
       ├─ Mount: /proc, /dev/{null,zero,urandom}, /tmp (tmpfs)
       ├─ pivot_root → new rootfs
       ├─ Unmount old root
       └─ exec python3 runner.py → Agno agent runs
```

If namespaces are unavailable, the fallback path:

```
User: ./skill-runner -skill X -prompt "..."
  │
  ├─ Load skill.yaml
  ├─ Create temp dir with symlinks to allowed commands only
  ├─ Filter environment to API keys + restricted PATH
  └─ exec python3 runner.py (in restricted environment)
```

## Security Layers

### Layer 1: Filesystem Isolation (namespace mode)

With user namespaces, the agent runs inside a pivot_root'd minimal rootfs:

| Visible inside sandbox | Source |
|------------------------|--------|
| `/bin/python3`, `/bin/jq`, etc. | Symlinks to host binaries (only those in `allowed_commands`) |
| `/usr/`, `/lib/`, `/lib64/` | Read-only bind mounts (needed for shared libraries) |
| `/workspace/` | Read-write bind mount of the actual workspace |
| `/proc/` | Fresh procfs (PID namespace — only sees own processes) |
| `/dev/null`, `/dev/zero`, `/dev/urandom` | Bind mounts from host |
| `/tmp/` | 64MB tmpfs |

**Not visible**: `/home`, `/root`, `/etc` (except minimal passwd/group), `/var`, `/run`, SSH keys, other users' files, the host root filesystem.

### Layer 2: Command Whitelisting (both modes)

Commands available to the agent are defined in `skill.yaml`:

```yaml
allowed_commands:
  - python3
  - jq
  - grep
  - cat
```

**How it works**: A temporary directory is created containing symlinks to only the resolved paths of allowed commands. PATH is set to only this directory.

In namespace mode, the rootfs `/bin/` directory contains only these symlinks. In fallback mode, PATH points to the temp symlink directory.

This is strictly better than the previous approach of adding directories (like `/usr/bin`) to PATH, which exposed every binary in those directories.

**Limitation (fallback mode only)**: The agent can still invoke binaries by absolute path (e.g., `/usr/bin/curl`) if it knows they exist. Namespace mode prevents this because the binary literally doesn't exist in the rootfs.

### Layer 3: Environment Variable Filtering

Only these variables cross the sandbox boundary:

- `GOOGLE_API_KEY`
- `GEMINI_API_KEY`
- `ANTHROPIC_API_KEY`
- `OPENAI_API_KEY`
- `PATH` (restricted as above)

Everything else (`HOME`, `USER`, `SHELL`, `SSH_AUTH_SOCK`, etc.) is stripped.

### Layer 4: Resource Limits (cgroups v2)

When cgroups v2 is writable (requires appropriate permissions):

- **Memory**: Configurable per-skill (default 512MB)
- **CPU**: Configurable (default 1 CPU)
- Process is OOM-killed if memory limit is exceeded

If cgroup setup fails, a warning is printed but execution continues. The timeout (Layer 5) still applies.

### Layer 5: Timeout + Process Group Cleanup

- The child runs in its own process group (`Setpgid: true`)
- On timeout, `SIGKILL` is sent to the entire process group (negative PID), killing all subprocesses
- `Pdeathsig: SIGKILL` ensures the child dies if the parent exits unexpectedly
- PID namespace (in namespace mode) means the agent only sees its own processes

## What This Does NOT Prevent

- **Network access**: The agent can make outbound network connections (no network namespace isolation yet). This is the biggest remaining gap — the agent could exfiltrate data if it has a command that supports network I/O (like `python3` with the `requests` library).
- **Side channels**: Timing, cache, and other hardware side channels
- **Kernel exploits**: A kernel vulnerability could break namespace isolation
- **Shared library abuse**: `/usr` is mounted read-only but is the full host `/usr` — a determined attacker could find and exec binaries there (namespace mode partially mitigates this since PATH only includes `/bin` symlinks, but the files are technically accessible)

## Skill Configuration

```yaml
name: suricata-analyst
description: Analyzes Suricata EVE JSON logs
allowed_commands:
  - python3
  - uv
  - jq
  - grep
  - cat
  - head
  - tail
  - wc
max_memory: 1G
timeout: 300s
```

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `name` | string | Skill name (required) | — |
| `description` | string | What the skill does | — |
| `allowed_commands` | []string | Commands the skill can use | [] |
| `max_memory` | string | Memory limit (K/M/G) | 512M |
| `timeout` | string | Execution timeout | 60s |

## Error Handling Philosophy

- **Namespace setup**: If unavailable, fall back to PATH restriction with a warning
- **cgroup setup**: If it fails, warn but continue (timeout still applies)
- **Command resolution**: Skip commands that don't exist on the host
- **Agent errors**: runner.py exits non-zero on failure so the Go layer reports the real error
- The pattern is: hard-fail on things we control (rootfs prep, config parsing), soft-fail on system capabilities (cgroups, namespaces)

## Future Enhancements

1. **Network namespace** (`CLONE_NEWNET`): Block all network access by default, optionally allow specific endpoints
2. **seccomp filtering**: Restrict available syscalls to further limit escape vectors
3. **Selective /usr mounting**: Resolve and bind-mount only the specific shared libraries needed, rather than all of `/usr`
4. **Skill signing**: Cryptographic verification of skill.yaml + SKILL.md
5. **Audit logging**: Structured logging of all skill executions, resource usage, and exit codes
