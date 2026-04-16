package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mdfranz/skill-runner/internal/skill"
)

// Environment defines what environment variables to pass through to the sandbox
type Environment struct {
	AllowedVars []string
	Values      map[string]string
}

// SetupEnvironment creates a filtered environment that only allows specific vars
func SetupEnvironment() Environment {
	env := Environment{
		AllowedVars: []string{
			"GOOGLE_API_KEY",
			"ANTHROPIC_API_KEY",
			"OPENAI_API_KEY",
			"GEMINI_API_KEY",
			"PATH",
		},
		Values: make(map[string]string),
	}

	// Copy only allowed vars from current environment
	for _, varName := range env.AllowedVars {
		if val, exists := os.LookupEnv(varName); exists {
			env.Values[varName] = val
		}
	}

	return env
}

// CreateCommandDir creates a temporary directory containing symlinks to only
// the allowed commands. This ensures PATH exposes exactly the whitelisted
// binaries and nothing else — unlike adding whole directories like /usr/bin.
// Returns the path to the temp dir (caller must clean up) and any error.
func CreateCommandDir(skillConfig *skill.SkillConfig) (string, error) {
	dir, err := os.MkdirTemp("", "skill-runner-cmds-*")
	if err != nil {
		return "", fmt.Errorf("failed to create command directory: %w", err)
	}

	linked := 0
	for _, cmd := range skillConfig.AllowedCommands {
		realPath, err := exec.LookPath(cmd)
		if err != nil {
			continue // command not installed on host, skip
		}
		// Resolve to absolute path (LookPath may return relative)
		realPath, err = filepath.Abs(realPath)
		if err != nil {
			continue
		}
		linkPath := filepath.Join(dir, cmd)
		if err := os.Symlink(realPath, linkPath); err != nil {
			// Clean up on failure
			os.RemoveAll(dir)
			return "", fmt.Errorf("failed to symlink %s -> %s: %w", cmd, realPath, err)
		}
		linked++
	}

	if linked == 0 {
		os.RemoveAll(dir)
		return "", fmt.Errorf("no allowed commands found in system PATH")
	}

	return dir, nil
}

// ConfigurePath sets PATH to the given command directory (from CreateCommandDir).
func (e *Environment) ConfigurePath(cmdDir string) {
	e.Values["PATH"] = cmdDir
}

// ToEnv converts the environment to an env list for exec.Cmd
func (e *Environment) ToEnv() []string {
	envList := make([]string, 0, len(e.Values))
	for k, v := range e.Values {
		if v != "" {
			envList = append(envList, k+"="+v)
		}
	}
	return envList
}

// CreateWorkspaceStructure creates the expected directory structure
func CreateWorkspaceStructure(workspacePath string) error {
	subdirs := []string{"data", "code", "skills", "output"}
	for _, dir := range subdirs {
		dirPath := filepath.Join(workspacePath, dir)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return fmt.Errorf("failed to create %s directory: %w", dir, err)
		}
	}
	return nil
}
