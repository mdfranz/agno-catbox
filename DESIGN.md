# Design & Architecture

**Status**: Version 1.0 design document. This has not been audited by security professionals or tested at scale.

## Overview

The Agno Skill Runner is a Go binary that executes a Python-based Agno runner inside a Linux namespace sandbox. It uses unprivileged Linux namespaces for filesystem isolation, symlink-based PATH restriction, cgroups v2 for resource limits, and process group controls for process cleanup.

## Design Intentions

1. **Credential protection**: The agent process should not see host home directories, SSH keys, or system config files
2. **No root required**: Uses unprivileged user namespaces — no Docker, no setuid, no capabilities
3. **Layered approach**: Multiple independent controls so a failure in one mechanism doesn't break all isolation
4. **Graceful degradation**: Falls back to reduced isolation on systems without namespace support

**Status**: These intentions are implemented in v1.0, but the system has not been battle-tested. Layer 2 (command whitelisting) is easily bypassed via absolute paths; cgroups v2 silently fails on many systems; and the overall isolation strength has not been validated against real attacks.

> **What this is not**: A strong security container. Network access is unrestricted, the full host `/usr` tree is visible inside the sandbox, and `allowed_commands` cannot enforce command whitelisting in namespace mode (see Layer 2). The primary value is preventing accidental credential exposure, not containing an adversarial agent.

## Process Flow

```
User: ./skill-runner -skill X -prompt "..." [-runner /path/to/runner.py]
  │
  ├─ Resolve Python runner path:
  │    -runner → SKILL_RUNNER_PY → runner.py next to binary
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

If namespaces are unavailable, or namespace bootstrap fails before the Python runner starts, the fallback path is used:

```
User: ./skill-runner -skill X -prompt "..."
  │
  ├─ Resolve Python runner path
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

**How it works**: A temporary directory is created containing symlinks to only the resolved paths of allowed commands.

In namespace mode, the rootfs `/bin/` directory contains only these symlinks, and `PATH` is set to `/bin:/usr/bin`. In fallback mode, `PATH` is set to the temp symlink directory.

**This does not reliably restrict commands.** Because `/usr` is bind-mounted in full (read-only) in namespace mode, any binary under `/usr/bin` is reachable by absolute path regardless of `allowed_commands`. For example, an agent using `ShellTools` can run `/usr/bin/curl` or `/usr/bin/nc` even if neither is listed. In fallback mode, the host filesystem is fully accessible. Treat `allowed_commands` as documentation of intent, not enforcement.

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

- **Network access**: No network namespace (`CLONE_NEWNET`) is used. The agent has full outbound network access and can exfiltrate data, make API calls, or open reverse shells using any network-capable binary under `/usr`.
- **Arbitrary binary execution**: The full host `/usr` tree is bind-mounted read-only. Any binary in `/usr/bin`, `/usr/sbin`, `/usr/local/bin`, etc. is accessible by absolute path, bypassing `allowed_commands`.
- **Resource exhaustion**: cgroups v2 limits require a writable `/sys/fs/cgroup`. On many systems (containers, some VMs) this is read-only and limits silently don't apply. The timeout is the only guaranteed bound.
- **Fallback mode filesystem access**: If namespace setup fails, the fallback path only restricts `PATH` — the agent can access the full host filesystem via absolute paths.
- **Side channels**: Timing, cache, and other hardware side channels.
- **Kernel exploits**: A kernel vulnerability could break namespace isolation.

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

- **Namespace setup**: If unavailable or bootstrap fails before exec, fall back to PATH restriction with a warning
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
