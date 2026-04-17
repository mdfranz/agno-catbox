# Agno Skill Runner: Architecture & Design

This document describes the architectural design of the Agno Skill Runner—a sandboxed execution environment for LLM-based Agno agents. It covers the core design decisions and their tradeoffs.

## Core Design

The Agno Skill Runner is designed as a **two-stage execution model**: a Go orchestrator manages a Python runner within a Linux namespace sandbox. The system is built around three key concepts:

1. **Workspace Lifecycle**: Each skill execution gets a timestamped, isolated workspace directory. External data can be staged into the workspace before execution, and a shared Python virtual environment is bind-mounted for dependency access.

2. **Pluggable Skill System**: Skills are defined via `skill.yaml` (configuration) and `SKILL.md` (instructions). The runner is generic—capable of executing any Agno-based agent without modification.

3. **Graceful Degradation**: The system attempts namespace isolation (user + mount + PID namespaces) but falls back to PATH restriction if the kernel doesn't support unprivileged namespaces or if bootstrap fails in containerized environments.

## Design Decisions & Tradeoffs

### 1. Unprivileged Namespaces (No Root Required)
The runner uses unprivileged Linux namespaces (`CLONE_NEWUSER`) rather than requiring root. This makes the tool usable in standard CI/CD environments and on machines where the user lacks `sudo` access.

**Tradeoff**: Unprivileged namespaces require bind-mounting the host `/usr` and `/lib` trees for shared libraries. This weakens isolation—an agent can still reach host binaries via absolute paths like `/usr/bin/curl`, defeating the symlink-based PATH restriction.

### 2. Symlink-Based PATH Control
Allowed commands are exposed via symlinks in a temporary `/bin` directory, preventing accidental execution of system tools.

**What it is**: A usability feature and accidental exposure control—clearly communicates which commands are available.

**What it isn't**: A hard security boundary. An agent can still invoke `/usr/bin/...` binaries by absolute path.

### 3. Go Orchestrator + Python Runner
The system is split: Go handles low-level system calls (namespaces, cgroups, pivot_root), while Python runs the Agno skill and has access to rich data processing libraries (polars, orjson).

**Benefit**: Single-file Go binary for distribution; access to Python ecosystems.

**Cost**: Requires a pre-configured Python runtime on the host and adds development complexity across two languages.

## Implementation Details

### Fallback Behavior
If a kernel does not support unprivileged namespaces or if namespace setup fails (common in containers), the runner degrades gracefully to environment filtering and PATH restriction. The agent still runs but with access to the full host filesystem via absolute paths.

### Process Management
To prevent runaway processes:
- All child processes are placed in a single process group so `SIGKILL` reaches all sub-tools on timeout.
- `Pdeathsig` is used to ensure the agent dies if the Go orchestrator crashes.

### Structured Logging
The runner outputs structured logs (JSON via `slog`) and supports configurable run names for easier integration with monitoring and audit systems.

## Current Status

This is version 1.0—a working implementation of the core design. The project is fresh (initial implementation completed Apr 2026) and has not yet been tested against real-world edge cases or in production. Key areas for future refinement:

- Testing across diverse Linux distributions and container runtimes
- Performance optimization of the namespace bootstrap and rootfs setup
- Extended testing of the fallback mode
- Evaluation of actual isolation strength under adversarial conditions
