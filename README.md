# Agno Skill Runner

A Go-based tool that runs [Agno](https://docs.agno.com) AI agent skills in a sandboxed Linux environment. It uses unprivileged user namespaces and mount namespaces to restrict the agent's filesystem view to only allowed binaries and the workspace directory.

## Prerequisites

- Linux (kernel 4.18+ with unprivileged user namespaces enabled)
- Go 1.21+ (to build)
- Python 3 (in the workspace, with Agno and dependencies installed)
- API keys for the model you want to use

## Build

```bash
go build -o skill-runner ./cmd/skill-runner
```

Produces the Go CLI binary. At runtime, the Python runner script is resolved in this order:

1. `-runner /path/to/runner.py`
2. `SKILL_RUNNER_PY=/path/to/runner.py`
3. `runner.py` next to the `skill-runner` binary

## Setup

1. **Export your API key:**

   ```bash
   export GEMINI_API_KEY="your-key-here"
   # or
   export GOOGLE_API_KEY="your-key-here"
   ```

2. **Prepare a workspace** with your data and a Python environment:

   ```bash
   mkdir -p ~/workspace
   cp /path/to/eve.json ~/workspace/
   cd ~/workspace
   uv venv && source .venv/bin/activate
   uv pip install agno pyyaml google-genai google-generativeai
   ```

3. **Create a skill** (or use the included `suricata-analyst`):

   ```bash
   mkdir -p skills/my-skill
   ```

   `skills/my-skill/skill.yaml`:
   ```yaml
   name: my-skill
   description: Analyzes security events
   allowed_commands:
     - python3
     - jq
     - grep
     - cat
   max_memory: 512M
   timeout: 60s
   ```

4. **Run:**

   ```bash
   ./skill-runner -skill my-skill -prompt "Analyze the events" -workspace ~/workspace
   ```

## Usage

```
skill-runner -skill <name> -prompt "<task>" [options]

Options:
  -skill       Skill name or path (required)
  -prompt      Task for the agent (required)
  -model       Model ID (default: gemini-2.5-flash)
  -runner      Path to runner.py (default: SKILL_RUNNER_PY or runner.py next to the binary)
  -workspace   Workspace directory (default: .)
  -debug       Enable debug logging
```

## Security Model

See [DESIGN.md](DESIGN.md) for the full security architecture.

**With namespace isolation** (default on supported kernels):
- The agent process runs inside a separate user, mount, and PID namespace
- Filesystem is restricted to a minimal rootfs: only allowed command binaries, shared libraries, and the workspace are visible
- Host home directories, SSH keys, and system configs are not accessible
- The old root is unmounted after pivot_root

**Without namespace isolation** (fallback):
- PATH is restricted via a symlink directory containing only allowed binaries
- Environment variables are filtered to only API keys
- Process group isolation ensures timeout kills all subprocesses
- The agent can still read host filesystem paths if it uses absolute paths directly

Fallback is used both when namespaces are unavailable and when namespace bootstrap fails before the Python runner starts.

**Always applied:**
- Environment variable filtering (only API keys pass through)
- Process group controls (Setpgid + SIGKILL on timeout)
- Pdeathsig ensures child dies if parent exits
- cgroups v2 resource limits (memory, CPU) when available

## Project Structure

```
cmd/skill-runner/main.go          CLI entry point + sandbox child detection
internal/
  runner/exec.go                  Skill execution orchestrator
  sandbox/
    sandbox.go                    Main runner (namespace + fallback paths)
    namespace.go                  User/mount/PID namespace setup
    rootfs.go                     Minimal rootfs preparation
    child.go                      Re-exec child: mount setup + pivot_root
    mount.go                      Environment filtering, symlink PATH
    cgroup.go                     cgroups v2 resource limits
  skill/
    types.go                      SkillConfig struct
    loader.go                     YAML config loading
runner.py                         Python/Agno agent orchestrator
skills/                           Skill definitions
```

## License

See repository root.
