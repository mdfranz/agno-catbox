package runner

import (
	"context"
	"fmt"

	"github.com/mdfranz/skill-runner/internal/sandbox"
	"github.com/mdfranz/skill-runner/internal/skill"
)

// Config holds the configuration for running a skill
type Config struct {
	SkillName     string
	SkillDir      string
	Prompt        string
	Model         string
	Debug         bool
	RunnerScript  string
	WorkspacePath string
	BaseWorkspace string
	DataDir       string
}

// RunSkill executes a skill with the given configuration
func RunSkill(ctx context.Context, config Config) error {
	// Load skill configuration
	skillConfig, err := skill.LoadSkill(config.SkillDir)
	if err != nil {
		return fmt.Errorf("failed to load skill: %w", err)
	}

	// Ensure workspace structure exists
	if err := sandbox.CreateWorkspaceStructure(config.WorkspacePath); err != nil {
		return fmt.Errorf("failed to create workspace: %w", err)
	}

	// Copy data if provided
	if config.DataDir != "" {
		if err := sandbox.CopyDir(config.DataDir, config.WorkspacePath); err != nil {
			return fmt.Errorf("failed to copy data directory: %w", err)
		}
	}

	// Create and run the sandbox
	runner := &sandbox.Runner{
		SkillConfig:   skillConfig,
		WorkspacePath: config.WorkspacePath,
		BaseWorkspace: config.BaseWorkspace,
		Prompt:        config.Prompt,
		Model:         config.Model,
		Debug:         config.Debug,
		RunnerScript:  config.RunnerScript,
	}

	return runner.Run(ctx)
}
