# Quick Start Guide

## 1. Build

```bash
go build -o skill-runner ./cmd/skill-runner
```

## 2. Prerequisites

- Linux with unprivileged user namespaces (check: `cat /proc/sys/kernel/unprivileged_userns_clone` should be `1`)
- Python 3 installed on the host
- An API key:
  ```bash
  export GEMINI_API_KEY="your-key-here"
  ```

## 3. Prepare a workspace

The workspace is where the agent operates. It needs Python dependencies installed:

```bash
mkdir -p ~/my-analysis
cd ~/my-analysis
cp /path/to/your/data.json .

# Create Python environment with Agno dependencies
uv venv && source .venv/bin/activate
uv pip install agno pyyaml google-genai google-generativeai polars orjson
```

## 4. Create a skill

```bash
mkdir -p skills/my-analyst
```

Create `skills/my-analyst/skill.yaml`:

```yaml
name: my-analyst
description: Analyzes security events
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

Optionally add a `skills/my-analyst/SKILL.md` with detailed instructions for the agent.

## 5. Run

```bash
# Run from the repo directory (where skill-runner binary is)
uv run ./skill-runner \
  -skill my-analyst \
  -prompt "Analyze the data.json file" \
  -workspace ~/my-analysis
```

The `uv run` prefix ensures the workspace's virtual environment is activated.

## 6. What happens

1. The Go binary loads the skill config
2. If user namespaces are available: creates a minimal rootfs with only allowed binaries, bind-mounts the workspace, and runs the agent inside an isolated mount namespace
3. If not: restricts PATH to a symlink directory with only allowed commands
4. The Agno agent (runner.py) loads the skill instructions and runs with the Gemini model
5. Output goes to stdout; generated scripts are retained in the workspace

## Verification

Check that namespace isolation is active:

```bash
# Should print "1"
cat /proc/sys/kernel/unprivileged_userns_clone
```

If it prints `0`, the runner will fall back to PATH-only restriction and print a warning. To enable namespace isolation:

```bash
sudo sysctl -w kernel.unprivileged_userns_clone=1
```

## Troubleshooting

**"no allowed commands found in system PATH"**
- The commands listed in `skill.yaml` must exist on the host system

**"namespace isolation unavailable"**
- Your kernel doesn't support unprivileged user namespaces
- The runner still works but with reduced isolation (PATH restriction only)

**"Error executing agent: API_KEY"**
- Export `GEMINI_API_KEY` or `GOOGLE_API_KEY` before running

**Python module errors**
- Make sure the workspace has a virtual environment with dependencies installed
- Use `uv run` to activate it when running skill-runner
