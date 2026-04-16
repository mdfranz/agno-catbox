package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mdfranz/skill-runner/internal/runner"
	"github.com/mdfranz/skill-runner/internal/sandbox"
	"github.com/mdfranz/skill-runner/internal/skill"
)

func main() {
	// If we're a re-exec'd sandbox child, run the mount setup + exec path.
	// This never returns on success (it exec's the real command).
	if sandbox.IsChildProcess() {
		sandbox.RunChild()
		os.Exit(126) // only reached if RunChild fails without calling os.Exit
	}

	// Normal CLI entry point
	skillNameFlag := flag.String("skill", "", "Name of the skill to run (required)")
	promptFlag := flag.String("prompt", "", "The prompt/task for the skill (required)")
	modelFlag := flag.String("model", "gemini-2.5-flash", "LLM model to use")
	debugFlag := flag.Bool("debug", false, "Enable debug logging")
	workspaceFlag := flag.String("workspace", ".", "Path to the workspace directory")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: skill-runner [OPTIONS]

Run Agno skills in a sandboxed environment with namespace isolation.

Options:
`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  skill-runner -skill suricata-analyst -prompt "Analyze eve.json"
  skill-runner -skill suricata-analyst -prompt "..." -model gemini-2.5-flash
  skill-runner -skill my-skill -prompt "Run analysis" -debug -workspace /data/workspace

Environment Variables:
  GOOGLE_API_KEY       Google Gemini API key
  GEMINI_API_KEY       Alias for GOOGLE_API_KEY
  ANTHROPIC_API_KEY    Anthropic Claude API key
  OPENAI_API_KEY       OpenAI API key
`)
	}

	flag.Parse()

	if *skillNameFlag == "" {
		fmt.Fprintf(os.Stderr, "Error: -skill flag is required\n")
		flag.Usage()
		os.Exit(1)
	}
	if *promptFlag == "" {
		fmt.Fprintf(os.Stderr, "Error: -prompt flag is required\n")
		flag.Usage()
		os.Exit(1)
	}

	skillDir, err := skill.FindSkillDir(*skillNameFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	workspacePath, err := filepath.Abs(*workspaceFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to resolve workspace path: %v\n", err)
		os.Exit(1)
	}

	config := runner.Config{
		SkillName:     *skillNameFlag,
		SkillDir:      skillDir,
		Prompt:        *promptFlag,
		Model:         *modelFlag,
		Debug:         *debugFlag,
		WorkspacePath: workspacePath,
	}

	ctx := context.Background()
	if err := runner.RunSkill(ctx, config); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
