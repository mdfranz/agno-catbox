# Architecture: Agno Skill Runner

The Agno Skill Runner is a secure execution environment for LLM-based agents. It combines a high-level orchestration layer in Go with a sandboxed Python execution environment using Linux namespaces.

## System Overview

The system is divided into three primary layers:
1.  **CLI/Orchestration (Go)**: Handles configuration, workspace preparation, and sandbox lifecycle.
2.  **Sandbox (Linux Namespaces)**: Provides filesystem, process, and user isolation.
3.  **Agent (Python/Agno)**: Executes the LLM logic and interacts with tools within the sandbox.

## Process Flow & Architecture

The following diagram illustrates the lifecycle of a skill execution, from the initial CLI call to the sandboxed agent execution.

```mermaid
graph TD
    User([User / CLI]) --> Main[Go Main Entry]
    
    subgraph "Go Orchestration Layer"
        Main --> LoadConfig[Load skill.yaml & SKILL.md]
        LoadConfig --> PrepWS[Prepare Workspace & Data]
        PrepWS --> PrepRootFS[Build minimal RootFS]
        PrepRootFS --> ReExec[Re-exec with Namespaces]
    end

    subgraph "Sandboxed Environment (Namespace Child)"
        ReExec --> SetupNS[Setup Mounts & pivot_root]
        SetupNS --> ExecPython[Exec runner.py]
    end

    subgraph "Agent Layer (Python)"
        ExecPython --> Agno[Agno Agent Initialization]
        Agno --> Gemini[Gemini LLM]
        Agno --> Tools[Shell & Python Tools]
        Tools --> Workspace[(Restricted Workspace)]
    end

    subgraph "Security Controls"
        Cgroups[Cgroups v2 Limits] -.-> ReExec
        NetFilter[Environment Filtering] -.-> ReExec
        Timeouts[Process Group Timeouts] -.-> Main
    end
```

## Core Components

### 1. The Go Orchestrator (`cmd/skill-runner`)
The orchestrator is responsible for:
-   **Skill Discovery**: Finding the requested skill configuration in the `skills/` directory.
-   **Workspace Management**: Creating isolated directories for each run (`runs/run-XXXX`).
-   **RootFS Construction**: Creating a temporary directory with symlinks to allowed binaries (whitelisting).
-   **Re-execution Pattern**: To safely enter Linux namespaces, the binary re-executes itself with `CLONE_NEWUSER`, `CLONE_NEWNS`, and `CLONE_NEWPID`.

### 2. The Sandbox Layer (`internal/sandbox`)
The sandbox provides several layers of defense:
-   **Mount Namespaces**: Uses `pivot_root` to change the root filesystem to a minimal environment. Only `/usr`, `/lib`, and `/lib64` are bind-mounted read-only. Host `/home`, `/root`, and `/etc` are hidden.
-   **User Namespaces**: Allows the runner to perform "root-like" operations (like mounting) without actual host root privileges.
-   **PID Namespaces**: The agent can only see its own processes, preventing it from inspecting the host process tree.
-   **Cgroups v2**: Enforces memory and CPU limits to prevent resource exhaustion.
-   **Process Groups**: All child processes are placed in a single group to ensure clean termination on timeout.

### 3. The Python Runner (`runner.py`)
The Python runner is the bridge between the sandbox and the Agno framework:
-   **Agno Integration**: Initializes an `agno.agent.Agent` with the `Gemini` model.
-   **Tool Provisioning**: Configures `ShellTools` and `PythonTools` for the agent.
-   **Instruction Injection**: Loads agent persona and system instructions from `SKILL.md`.

## Security Model

| Feature | Enforcement | Description |
| :--- | :--- | :--- |
| **Credential Protection** | Environment Filtering | Strips `HOME`, `SSH_AUTH_SOCK`, etc. Only whitelisted API keys are passed. |
| **Filesystem Isolation** | Mount Namespaces | `pivot_root` hides host sensitive directories. |
| **Command Whitelisting** | PATH Restriction | Only symlinked binaries in `/bin` are easily accessible. |
| **Resource Limits** | Cgroups v2 | Hard limits on RAM and CPU usage. |
| **Reliable Cleanup** | PGID Killing | `SIGKILL` sent to the entire process group on timeout or exit. |

## Data Flow
1.  **Input**: User prompt and skill name via CLI.
2.  **Preparation**: Go copies necessary data to a fresh workspace.
3.  **Execution**: Python runner executes inside the workspace.
4.  **Output**: Agent responses are streamed to `stdout`, and logs are captured to `skill-runner.log`.
5.  **Cleanup**: Temporary RootFS and sandbox resources are released.
