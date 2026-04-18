package main

import (
	"context"
	"fmt"
	"log/slog"
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
	runName    string
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
  skill-runner --skill suricata-analyst --prompt "..." --model gemini-3.1-flash-lite-preview
  skill-runner --skill my-skill --prompt "Run analysis" --debug --workspace /data/workspace
  skill-runner --skill suricata-analyst --prompt "Analyze data" --data ./my-data
  skill-runner --skill suricata-analyst --prompt "Analyze again" --run-name my-analysis`,
		RunE: func(cmd *cobra.Command, args []string) error {
			baseWorkspace, err := filepath.Abs(workspace)
			if err != nil {
				return fmt.Errorf("failed to resolve workspace path: %w", err)
			}

			// Initialize structured logging
			logPath := filepath.Join(baseWorkspace, "skill-runner.log")
			logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("failed to open log file %s: %w", logPath, err)
			}
			defer logFile.Close()

			logLevel := slog.LevelInfo
			if debug {
				logLevel = slog.LevelDebug
			}

			logger := slog.New(slog.NewJSONHandler(logFile, &slog.HandlerOptions{Level: logLevel}))
			var loggerRunID string
			if runName != "" {
				loggerRunID = runName
			} else {
				loggerRunID = time.Now().Format("20060102-150405.000")
			}
			logger = logger.With("run_id", loggerRunID)
			slog.SetDefault(logger)

			skillDir, err := skill.FindSkillDir(skillName)
			if err != nil {
				return fmt.Errorf("skill directory not found: %w", err)
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

			// Determine workspace directory name
			var workspacePath string
			if runName != "" {
				workspacePath = filepath.Join(baseWorkspace, runName)
			} else {
				workspacePath = filepath.Join(baseWorkspace, fmt.Sprintf("run-%s-%s", skillName, time.Now().Format("20060102-150405")))
			}

			config := runner.Config{
				RunID:          loggerRunID,
				SkillName:      skillName,
				SkillDir:       skillDir,
				Prompt:         prompt,
				Model:          model,
				Debug:          debug,
				RunnerScript:   runnerScript,
				WorkspacePath:  workspacePath,
				BaseWorkspace:  baseWorkspace,
				DataDir:        dataDir,
				ChildLogWriter: logFile,
			}

			slog.Info("using workspace", "path", workspacePath)

			ctx := context.Background()
			err = runner.RunSkill(ctx, config)
			if err != nil {
				slog.Error("run failed", "error", err)
				return err
			}
			return nil
		},
	}
	rootCmd.Flags().StringVarP(&skillName, "skill", "s", "", "Name of the skill to run (required)")
	rootCmd.Flags().StringVarP(&prompt, "prompt", "p", "", "The prompt/task for the skill (required)")
	rootCmd.Flags().StringVarP(&model, "model", "m", "gemini-3.1-flash-lite-preview", "LLM model to use")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "Enable debug logging")
	rootCmd.Flags().StringVarP(&runnerPath, "runner", "r", "", "Path to runner.py (default: SKILL_RUNNER_PY or runner.py next to the binary)")
	rootCmd.Flags().StringVarP(&workspace, "workspace", "w", ".", "Path to the base workspace directory")
	rootCmd.Flags().StringVar(&runName, "run-name", "", "Human-readable name for the run workspace (allows reuse)")
	rootCmd.Flags().StringVarP(&dataDir, "data", "d", "", "Path to a data directory to copy into the workspace")

	_ = rootCmd.MarkFlagRequired("skill")
	_ = rootCmd.MarkFlagRequired("prompt")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
