package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mdfranz/skill-runner/internal/runner"
	"github.com/mdfranz/skill-runner/internal/sandbox"
	"github.com/mdfranz/skill-runner/internal/skill"
	"github.com/spf13/cobra"
)

var (
	skillName  string
	prompt     string
	model      string
	debug      bool
	runnerPath string
	workspace  string
	dataDir    string
)

func main() {
	// If we're a re-exec'd sandbox child, run the mount setup + exec path.
	// This never returns on success (it exec's the real command).
	if sandbox.IsChildProcess() {
		sandbox.RunChild()
		os.Exit(126) // only reached if RunChild fails without calling os.Exit
	}

	rootCmd := &cobra.Command{
		Use:   "skill-runner",
		Short: "Run Agno skills in a sandboxed environment with namespace isolation",
		Long: `Run Agno skills in a sandboxed environment with namespace isolation.
        
Environment Variables:
  SKILL_RUNNER_PY      Path to the Python runner script
  GOOGLE_API_KEY       Google Gemini API key
  GEMINI_API_KEY       Alias for GOOGLE_API_KEY
  ANTHROPIC_API_KEY    Anthropic Claude API key
  OPENAI_API_KEY       OpenAI API key`,
		Example: `  skill-runner --skill suricata-analyst --prompt "Analyze eve.json"
  skill-runner --skill suricata-analyst --prompt "..." --model gemini-2.5-flash
  skill-runner --skill my-skill --prompt "Run analysis" --debug --workspace /data/workspace
  skill-runner --skill suricata-analyst --prompt "Analyze data" --data ./my-data`,
		RunE: func(cmd *cobra.Command, args []string) error {
			skillDir, err := skill.FindSkillDir(skillName)
			if err != nil {
				return fmt.Errorf("skill directory not found: %w", err)
			}

			baseWorkspace, err := filepath.Abs(workspace)
			if err != nil {
				return fmt.Errorf("failed to resolve workspace path: %w", err)
			}

			runnerScript := runnerPath
			if runnerScript == "" {
				runnerScript = os.Getenv("SKILL_RUNNER_PY")
			}
			if runnerScript != "" {
				runnerScript, err = filepath.Abs(runnerScript)
				if err != nil {
					return fmt.Errorf("failed to resolve runner path: %w", err)
				}
			}

			// Create a timestamped workspace directory
			workspacePath := filepath.Join(baseWorkspace, fmt.Sprintf("run-%s-%s", skillName, time.Now().Format("20060102-150405")))

			config := runner.Config{
				SkillName:     skillName,
				SkillDir:      skillDir,
				Prompt:        prompt,
				Model:         model,
				Debug:         debug,
				RunnerScript:  runnerScript,
				WorkspacePath: workspacePath,
				BaseWorkspace: baseWorkspace,
				DataDir:       dataDir,
			}

			fmt.Printf("Using workspace: %s\n", workspacePath)

			ctx := context.Background()
			return runner.RunSkill(ctx, config)
		},
	}

	rootCmd.Flags().StringVarP(&skillName, "skill", "s", "", "Name of the skill to run (required)")
	rootCmd.Flags().StringVarP(&prompt, "prompt", "p", "", "The prompt/task for the skill (required)")
	rootCmd.Flags().StringVarP(&model, "model", "m", "gemini-2.5-flash", "LLM model to use")
	rootCmd.Flags().BoolVarP(&debug, "debug", "d", false, "Enable debug logging")
	rootCmd.Flags().StringVarP(&runnerPath, "runner", "r", "", "Path to runner.py (default: SKILL_RUNNER_PY or runner.py next to the binary)")
	rootCmd.Flags().StringVarP(&workspace, "workspace", "w", ".", "Path to the base workspace directory")
	rootCmd.Flags().StringVar(&dataDir, "data", "", "Path to a data directory to copy into the workspace")

	_ = rootCmd.MarkFlagRequired("skill")
	_ = rootCmd.MarkFlagRequired("prompt")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
