# Agno Skill Runner

A Go-based tool that runs [Agno](https://docs.agno.com) AI agent skills in a sandboxed Linux environment. **Two implementations ship side-by-side** for A/B comparison:

- **`skill-runner`** — hand-rolled user+mount+PID namespace sandbox with pivot_root and a symlinked command directory (the original).
- **`skill-runner-oci`** — daemonless OCI container. Uses a locally-built OCI image (via `buildah`) and an external OCI runtime (`crun` preferred, `runc` fallback). **No Docker or Podman daemon.**

Both binaries share the same `skills/`, `runner.py`, skill loader, and CLI flag surface. Run the same prompt through both and compare `runs/`.

## Prerequisites

- Linux (kernel 4.18+ with unprivileged user namespaces enabled)
- Go 1.21+ (to build)
- Python 3 and [uv](https://github.com/astral-sh/uv) (for workspace setup of the namespace binary)
- For `skill-runner-oci` only: `buildah` at build time, `crun` (or `runc`) at runtime
- API keys for the model you want to use

## Build

```bash
make build        # skill-runner (namespace)
make build-oci    # skill-runner-oci (OCI runtime)
make build-all    # both
make image        # OCI runtime image → ./image/ (OCI layout)
```

At runtime, the namespace binary resolves `runner.py` in this order:

1. `-runner /path/to/runner.py`
2. `SKILL_RUNNER_PY=/path/to/runner.py`
3. `runner.py` next to the `skill-runner` binary

The OCI binary bakes `runner.py` into the image; no host lookup happens.

## OCI binary quick start

```bash
make image                        # builds ./image/ OCI layout via buildah
./skill-runner-oci doctor         # sanity-check runtime, image, user-ns
./skill-runner-oci \
  -s suricata-analyst \
  -p "Analyze eve.json" \
  -w ./runs
```

Subcommands:
- `skill-runner-oci image build` — build the image via buildah (alternative to `make image`)
- `skill-runner-oci image path`  — print the resolved image layout dir
- `skill-runner-oci doctor`      — check `crun`/`runc`, image, user-ns, cgroup v2

### How the OCI path differs

| Concern | `skill-runner` | `skill-runner-oci` |
|---|---|---|
| Rootfs | symlinks + bind of host `/usr` | OCI image layers (no host binaries) |
| `allowed_commands` | symlinked into `/bin` (leaky via `/usr/bin/...`) | image contents; enforced |
| Timeout / memory / cpu | hand-rolled cgroup v2 writes | runtime-spec `linux.resources` |
| `/.skill` | copied into rootfs | ro bind mount |
| Network | host | host (toggle with `--network-isolated`) |
| External deps | none | `crun`/`runc` + `buildah` |
| Static binary | yes | no (shells out to runtime) |

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

See [ARCHITECTURE.md](ARCHITECTURE.md) and [DESIGN.md](DESIGN.md) for the full architecture and design details. The short version: this tool prevents the agent from accidentally reading host credentials; it does **not** provide strong containment.

**What namespace isolation actually gives you:**
- Host home directories, SSH keys, and `/etc/` are not visible after `pivot_root`
- Environment is filtered to API keys only — `HOME`, `USER`, `SSH_AUTH_SOCK`, etc. are stripped
- PID namespace — agent only sees its own processes

**What it does not prevent:**
- **Network**: No network namespace. The agent has full outbound access.
- **Binary access**: The full host `/usr` tree is bind-mounted read-only. Any binary in `/usr/bin` is reachable by absolute path regardless of `allowed_commands`.
- **`allowed_commands` enforcement**: This list controls symlinks in `/bin` but does not block access to the same binaries via `/usr/bin/...`. Treat it as documentation, not a security boundary.
- **Resource limits**: cgroups v2 limits require a writable `/sys/fs/cgroup`. This silently fails on many systems (containers, some VMs). The skill timeout is the only guaranteed bound.

**Fallback mode** (when namespaces are unavailable or bootstrap fails):
- PATH is restricted to allowed commands only
- The agent can still access the full host filesystem via absolute paths
- Environment is still filtered to API keys

## Project Structure

```
cmd/
  skill-runner/main.go            Namespace-sandbox CLI (re-exec child pattern)
  skill-runner-oci/main.go        OCI-runtime CLI + image/doctor subcommands
internal/
  runner/exec.go                  Orchestrator for the namespace path
  ocirunner/exec.go               Orchestrator for the OCI path
  sandbox/                        Hand-rolled namespace sandbox
    sandbox.go                      Main runner (namespace + fallback paths)
    namespace.go                    User/mount/PID namespace setup
    rootfs.go                       Minimal rootfs preparation
    child.go                        Re-exec child: mount setup + pivot_root
    mount.go                        Environment filtering, symlink PATH
    cgroup.go                       cgroups v2 resource limits
  oci/                            Daemonless OCI runtime
    image.go                        Image-layout discovery/validation
    bundle.go                       Per-run bundle dir
    rootfs.go                       OCI layer → rootfs extraction (pure Go)
    spec.go                         runtime-spec config.json builder
    runtime.go                      crun/runc exec wrapper
    runner.go                       Top-level Run()
  skill/
    types.go                        SkillConfig struct
    loader.go                       YAML config loading
runner.py                         Python/Agno agent orchestrator (baked into OCI image)
Containerfile                     OCI image definition
requirements.txt                  Pinned Python deps baked into the image
skills/                           Skill definitions
```

# Usage

```
 ./skill-runner -m gemini-3.1-pro-preview  -w ./runs -s suricata-analyst -p "Find interesting hosts based on TLS traffic" -d data --debug
```


## License

See repository root.
