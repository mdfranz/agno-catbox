#!/usr/bin/env python3
import sys
import os
import io
import logging

import yaml
from pathlib import Path
from agno.agent import Agent
from agno.models.google import Gemini
from agno.tools.shell import ShellTools
from agno.tools.python import PythonTools


class TeeWriter:
    """Writes to both a file and a file object (e.g., stdout/stderr)."""
    def __init__(self, file_obj, log_file):
        self.file_obj = file_obj
        self.log_file = log_file

    def write(self, data):
        self.file_obj.write(data)
        self.log_file.write(data)
        self.log_file.flush()

    def flush(self):
        self.file_obj.flush()
        self.log_file.flush()

    def isatty(self):
        return self.file_obj.isatty()

def load_skill_config(skill_dir):
    """Loads the skill configuration from the specified skill directory."""
    skill_path = Path(skill_dir)
    yaml_path = skill_path / "skill.yaml"
    md_path = skill_path / "SKILL.md"

    config = {}
    if yaml_path.exists():
        with open(yaml_path, "r") as f:
            config = yaml.safe_load(f)

    if md_path.exists():
        with open(md_path, "r") as f:
            config["instructions_md"] = f.read()

    return config

def main():
    # Set up logging to runner.log
    log_file = open("runner.log", "a")
    sys.stdout = TeeWriter(sys.stdout, log_file)
    sys.stderr = TeeWriter(sys.stderr, log_file)

    # Configure Python logging to capture all library logs
    logging.basicConfig(
        level=logging.DEBUG,
        format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
        handlers=[
            logging.FileHandler("runner.log", mode="a"),
            logging.StreamHandler(sys.stderr),
        ]
    )

    try:
        if len(sys.argv) < 4:
            print("Usage: runner.py <skill_name> <prompt> <skill_dir> [--model <model>] [--debug]", file=sys.stderr)
            sys.exit(1)

        # Map GEMINI_API_KEY to GOOGLE_API_KEY for Agno compatibility
        if os.environ.get("GEMINI_API_KEY") and not os.environ.get("GOOGLE_API_KEY"):
            os.environ["GOOGLE_API_KEY"] = os.environ["GEMINI_API_KEY"]

        skill_name = sys.argv[1]
        prompt = sys.argv[2]
        skill_dir = sys.argv[3]

        # Parse optional arguments
        model_id = "gemini-3.1-flash-lite-preview"
        debug = False
        if "--model" in sys.argv:
            idx = sys.argv.index("--model")
            if idx + 1 < len(sys.argv):
                model_id = sys.argv[idx + 1]
        if "--debug" in sys.argv:
            debug = True

        config = load_skill_config(skill_dir)

        if not config:
            print(f"Error: Skill '{skill_name}' not found or empty config.", file=sys.stderr)
            sys.exit(1)

        # Use instructions from SKILL.md if available
        instructions = config.get("instructions_md", config.get("description", "You are a security analyst."))

        # Initialize the Agno Agent
        agent = Agent(
            model=Gemini(id=model_id, thinking_budget=16000, include_thoughts=True),
            instructions=[instructions],
            tools=[
                PythonTools(base_dir=Path.cwd()),
                ShellTools(base_dir=Path.cwd())
            ],
            markdown=True,
            reasoning=True,
        )

        # Run the agent
        try:
            from agno.run.agent import RunContentEvent, ReasoningContentDeltaEvent, ToolCallStartedEvent, ToolCallCompletedEvent, RunEvent
            import json

            print("\n--- Agent Execution ---", file=sys.stderr)
            for event in agent.run(prompt, stream=True):
                # Handle streaming reasoning/thoughts
                if isinstance(event, ReasoningContentDeltaEvent):
                    if event.reasoning_content:
                        print(event.reasoning_content, end="", flush=True)

                # Handle streaming content or chunks
                elif isinstance(event, RunContentEvent):
                    if event.reasoning_content:
                        print(event.reasoning_content, end="", flush=True)
                    if event.content:
                        print(event.content, end="", flush=True)

                # Handle tool calls to show generated code
                elif isinstance(event, ToolCallStartedEvent):
                    if not event.tool:
                        continue
                    tool_name = event.tool.tool_name
                    tool_args = event.tool.tool_args or {}

                    print(f"\n\n[Action: {tool_name}]", flush=True)

                    # Specifically extract code for Python tools
                    if tool_name in ["run_python_code", "save_to_file_and_run"]:
                        code = tool_args.get("code") or tool_args.get("python_code")
                        if code:
                            print(f"--- Generated Code ---\n{code}\n----------------------", flush=True)
                        else:
                            print(f"Args: {json.dumps(tool_args)}", flush=True)
                    else:
                        print(f"Args: {json.dumps(tool_args)}", flush=True)

                # Handle tool completion to show results
                elif isinstance(event, ToolCallCompletedEvent):
                    if event.content:
                        # Truncate very long results for readability
                        result = str(event.content)
                        if len(result) > 500:
                            result = result[:500] + "... (truncated)"
                        print(f"[Result: {result}]\n", flush=True)

            print("\n--- Execution Finished ---", file=sys.stderr)

        except Exception as e:
            print(f"Error executing agent: {e}", file=sys.stderr)
            if "API_KEY" in str(e).upper():
                print("\nNote: This runner requires a valid GOOGLE_API_KEY or ANTHROPIC_API_KEY.", file=sys.stderr)
                print("The sandbox filters environment variables for security.", file=sys.stderr)
            sys.exit(1)
    finally:
        log_file.close()

if __name__ == "__main__":
    main()
