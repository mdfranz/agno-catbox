#!/usr/bin/env python3
import sys
import os

import yaml
from pathlib import Path
from agno.agent import Agent
from agno.models.google import Gemini
from agno.tools.shell import ShellTools
from agno.tools.python import PythonTools

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
    model_id = "gemini-3-flash-preview"
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
        model=Gemini(id=model_id),
        instructions=[instructions],
        tools=[
            PythonTools(),
            ShellTools()
        ],
        markdown=True,
    )

    # Run the agent
    try:
        agent.print_response(prompt)
    except Exception as e:
        print(f"Error executing agent: {e}", file=sys.stderr)
        if "API_KEY" in str(e).upper():
            print("\nNote: This runner requires a valid GOOGLE_API_KEY or ANTHROPIC_API_KEY.", file=sys.stderr)
            print("The sandbox filters environment variables for security.", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
