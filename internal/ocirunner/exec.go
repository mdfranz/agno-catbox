package ocirunner

import (
	"context"
	"fmt"
	"io"

	"github.com/mdfranz/skill-runner/internal/oci"
	"github.com/mdfranz/skill-runner/internal/sandbox"
	"github.com/mdfranz/skill-runner/internal/skill"
)

// Config mirrors internal/runner.Config for the OCI-based path.
// Extra fields compared to the namespace runner: ImageDir, RuntimeOverride,
// NetworkIsolated. RunnerScript is absent because runner.py is baked into
// the container image.
type Config struct {
	RunID         string
	SkillName     string
	SkillDir      string
	Prompt        string
	Model         string
	Debug         bool
	WorkspacePath string
	BaseWorkspace string
	DataDir       string

	ImageDir        string
	RuntimeOverride string
	NetworkIsolated bool

	ChildLogWriter io.Writer
}

// RunSkill loads the skill, prepares the workspace, and invokes the OCI runtime.
// Shared fs helpers from internal/sandbox are reused: SetupEnvironment,
// CreateWorkspaceStructure, CopyDir are pure utilities with no namespace side
// effects.
func RunSkill(ctx context.Context, config Config) error {
	skillConfig, err := skill.LoadSkill(config.SkillDir)
	if err != nil {
		return fmt.Errorf("failed to load skill: %w", err)
	}

	if err := sandbox.CreateWorkspaceStructure(config.WorkspacePath); err != nil {
		return fmt.Errorf("failed to create workspace: %w", err)
	}

	if config.DataDir != "" {
		if err := sandbox.CopyDir(config.DataDir, config.WorkspacePath); err != nil {
			return fmt.Errorf("failed to copy data directory: %w", err)
		}
	}

	env := sandbox.SetupEnvironment()
	env.Values["PATH"] = "/usr/local/bin:/usr/bin:/bin"
	envList := env.ToEnv()

	ociCfg := oci.Config{
		RunID:           config.RunID,
		SkillConfig:     skillConfig,
		WorkspacePath:   config.WorkspacePath,
		Prompt:          config.Prompt,
		Model:           config.Model,
		Debug:           config.Debug,
		ImageDir:        config.ImageDir,
		RuntimeOverride: config.RuntimeOverride,
		NetworkIsolated: config.NetworkIsolated,
		Env:             envList,
	}

	return oci.Run(ctx, ociCfg)
}
