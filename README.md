# Agno Skill Runner

A Go-based tool that runs [Agno](https://docs.agno.com) AI agent skills in a sandboxed Linux environment. It uses unprivileged user namespaces and mount namespaces to restrict the agent's filesystem view to only allowed binaries and the workspace directory.

## Prerequisites

- Linux (kernel 4.18+ with unprivileged user namespaces enabled)
- Go 1.21+ (to build)
- Python 3 and [uv](https://github.com/astral-sh/uv) (for workspace setup)
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
   ```

2. **Prepare the workspace:**

   ```bash
   ./prep.sh
   ```
   This script creates a `./runs` directory and sets up a Python virtual environment with all necessary dependencies (Agno, Polars, orjson, etc.).

3. **Add your data:**

   ```bash
   cp /path/to/eve.json ./runs/
   ```

## Usage

```bash
./skill-runner -skill <name> -prompt "<task>" [options]
```

**Options:**
- `-skill`: Skill name or path (e.g., `suricata-analyst`)
- `-prompt`: Task for the agent (e.g., "Find top 10 DNS queries")
- `-workspace`: Workspace directory (default: `.`)
- `-model`: Model ID (default: `gemini-2.0-flash`)
- `-debug`: Enable debug logging

### Example: Suricata Threat Hunting

The following example uses the built-in `suricata-analyst` skill to investigate network logs:

```bash
./skill-runner \
  -skill suricata-analyst \
  -prompt "Analyze eve.json for suspicious mDNS traffic and rare SNIs" \
  -workspace ./runs
```

**What the agent does:**
1. **Discovers Schema**: Samples `eve.json` using `orjson` to understand the event structures.
2. **Optimizes Data**: Converts filtered JSON events to Parquet (e.g., `mdns.parquet`) for high-performance analysis with Polars.
3. **Executes Analysis**: Generates and runs Python scripts (e.g., `analyze_mdns.py`) to aggregate and pivot data.
4. **Reports Findings**: Identifies anomalies such as unauthorized mDNS queries or suspicious external TLS connections.

The generated analysis scripts and Parquet files are retained in the workspace for further inspection.


## Security Model

See [DESIGN.md](DESIGN.md) for the full security architecture.

**With namespace isolation** (default on supported kernels):
- The agent process runs inside a separate user, mount, and PID namespace
- Filesystem is restricted to a minimal rootfs, but the host's `/usr` directory is bind-mounted (read-only) along with the workspace (read-write).
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
