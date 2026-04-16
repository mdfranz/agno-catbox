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
	WorkspacePath string
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

	// Create and run the sandbox
	runner := &sandbox.Runner{
		SkillConfig:   skillConfig,
		WorkspacePath: config.WorkspacePath,
		Prompt:        config.Prompt,
		Model:         config.Model,
		Debug:         config.Debug,
	}

	return runner.Run(ctx)
}
