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

The workspace is where the agent operates. It needs Python dependencies installed. Choose one approach:

**Option A: Persistent venv (recommended)**
```bash
mkdir -p ~/my-analysis
cd ~/my-analysis
cp /path/to/your/data.json .

# Create Python environment with Agno dependencies
uv venv
uv pip install agno pyyaml google-genai google-generativeai polars orjson
source .venv/bin/activate
```

**Option B: uv run (temporary venv per execution)**
```bash
mkdir -p ~/my-analysis
cd ~/my-analysis
cp /path/to/your/data.json .
# Dependencies will be installed via uv run in step 5
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

**If you used Option A (persistent venv)**:
```bash
source ~/my-analysis/.venv/bin/activate
./skill-runner \
  -skill my-analyst \
  -prompt "Analyze the data.json file" \
  -runner ./runner.py \
  -workspace ~/my-analysis
```

**If you used Option B (uv run)**:
```bash
cd ~/my-analysis
uv run -p agno,pyyaml,google-genai,google-generativeai,polars,orjson \
  /path/to/skill-runner \
  -skill my-analyst \
  -prompt "Analyze the data.json file" \
  -runner /path/to/runner.py \
  -workspace .
```

If you install the binary elsewhere, keep passing `-runner /path/to/runner.py` or set `SKILL_RUNNER_PY`.

## 6. What happens

1. The Go binary loads the skill config from `skill.yaml`
2. Resolves the Python runner from `-runner`, `SKILL_RUNNER_PY`, or `runner.py` next to the binary
3. **Namespace mode** (if available): Creates a minimal rootfs with only allowed binaries, bind-mounts the workspace, and runs the agent inside isolated namespaces (user + mount + PID). The agent sees `/usr` (read-only), `/lib` (read-only), and `/workspace` (read-write).
4. **Fallback mode** (if namespaces unavailable or bootstrap fails): Restricts PATH to a symlink directory with only allowed commands. The agent has access to the full host filesystem via absolute paths, but API keys and home directories are still filtered.
5. The Agno agent (runner.py) loads skill instructions and runs with the LLM model
6. Output goes to stdout; generated scripts and logs are retained in the workspace

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
- Or namespace bootstrap failed before the Python runner started
- The runner still works but with reduced isolation (PATH restriction only)

**"Error executing agent: API_KEY"**
- Export `GEMINI_API_KEY` or `GOOGLE_API_KEY` before running

**Python module errors**
- Make sure the workspace has a virtual environment with dependencies installed
- Use `uv run` to activate it when running skill-runner
