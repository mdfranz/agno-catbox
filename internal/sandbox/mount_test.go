package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mdfranz/skill-runner/internal/skill"
)

func TestSetupEnvironment_FiltersVars(t *testing.T) {
	// Set some vars that should pass through
	os.Setenv("GOOGLE_API_KEY", "test-google-key")
	os.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")
	os.Setenv("OPENAI_REASONING_EFFORT", "high")
	os.Setenv("OPENAI_MAX_COMPLETION_TOKENS", "8000")
	os.Setenv("OPENAI_TEMPERATURE", "0.7")
	os.Setenv("AGENT_REASONING", "false")
	// Set some vars that should NOT pass through
	os.Setenv("HOME", "/home/testuser")
	os.Setenv("SECRET_TOKEN", "should-not-appear")

	defer func() {
		os.Unsetenv("GOOGLE_API_KEY")
		os.Unsetenv("ANTHROPIC_API_KEY")
		os.Unsetenv("OPENAI_REASONING_EFFORT")
		os.Unsetenv("OPENAI_MAX_COMPLETION_TOKENS")
		os.Unsetenv("OPENAI_TEMPERATURE")
		os.Unsetenv("AGENT_REASONING")
		os.Unsetenv("SECRET_TOKEN")
	}()

	env := SetupEnvironment()

	if env.Values["GOOGLE_API_KEY"] != "test-google-key" {
		t.Errorf("expected GOOGLE_API_KEY=test-google-key, got %q", env.Values["GOOGLE_API_KEY"])
	}
	if env.Values["ANTHROPIC_API_KEY"] != "test-anthropic-key" {
		t.Errorf("expected ANTHROPIC_API_KEY=test-anthropic-key, got %q", env.Values["ANTHROPIC_API_KEY"])
	}
	if env.Values["OPENAI_REASONING_EFFORT"] != "high" {
		t.Errorf("expected OPENAI_REASONING_EFFORT=high, got %q", env.Values["OPENAI_REASONING_EFFORT"])
	}
	if env.Values["OPENAI_MAX_COMPLETION_TOKENS"] != "8000" {
		t.Errorf("expected OPENAI_MAX_COMPLETION_TOKENS=8000, got %q", env.Values["OPENAI_MAX_COMPLETION_TOKENS"])
	}
	if env.Values["OPENAI_TEMPERATURE"] != "0.7" {
		t.Errorf("expected OPENAI_TEMPERATURE=0.7, got %q", env.Values["OPENAI_TEMPERATURE"])
	}
	if env.Values["AGENT_REASONING"] != "false" {
		t.Errorf("expected AGENT_REASONING=false, got %q", env.Values["AGENT_REASONING"])
	}
	if _, ok := env.Values["HOME"]; ok {
		t.Error("HOME should not be in filtered environment")
	}
	if _, ok := env.Values["SECRET_TOKEN"]; ok {
		t.Error("SECRET_TOKEN should not be in filtered environment")
	}
}

func TestSetupEnvironment_ToEnv(t *testing.T) {
	os.Setenv("GOOGLE_API_KEY", "key123")
	os.Setenv("OPENAI_REASONING_EFFORT", "medium")
	defer os.Unsetenv("GOOGLE_API_KEY")
	defer os.Unsetenv("OPENAI_REASONING_EFFORT")

	env := SetupEnvironment()
	envList := env.ToEnv()

	foundGoogle := false
	foundOpenAIReasoning := false
	for _, e := range envList {
		if e == "GOOGLE_API_KEY=key123" {
			foundGoogle = true
		}
		if e == "OPENAI_REASONING_EFFORT=medium" {
			foundOpenAIReasoning = true
		}
		// Should never contain HOME, USER, etc.
		if len(e) > 5 && e[:5] == "HOME=" {
			t.Error("HOME should not appear in env list")
		}
	}
	if !foundGoogle {
		t.Error("GOOGLE_API_KEY=key123 not found in env list")
	}
	if !foundOpenAIReasoning {
		t.Error("OPENAI_REASONING_EFFORT=medium not found in env list")
	}
}

func TestCreateCommandDir_CreatesSymlinks(t *testing.T) {
	// "ls" and "echo" should exist on any system
	config := &skill.SkillConfig{
		AllowedCommands: []string{"ls", "echo"},
	}

	dir, err := CreateCommandDir(config)
	if err != nil {
		t.Fatalf("CreateCommandDir failed: %v", err)
	}
	defer os.RemoveAll(dir)

	// Check that symlinks were created
	for _, cmd := range config.AllowedCommands {
		linkPath := filepath.Join(dir, cmd)
		info, err := os.Lstat(linkPath)
		if err != nil {
			t.Errorf("expected symlink for %s at %s: %v", cmd, linkPath, err)
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("expected %s to be a symlink", linkPath)
		}
		// Verify symlink target exists
		target, err := os.Readlink(linkPath)
		if err != nil {
			t.Errorf("failed to read symlink %s: %v", linkPath, err)
			continue
		}
		if _, err := os.Stat(target); err != nil {
			t.Errorf("symlink target %s does not exist: %v", target, err)
		}
	}
}

func TestCreateCommandDir_OnlyAllowedCommands(t *testing.T) {
	config := &skill.SkillConfig{
		AllowedCommands: []string{"ls"},
	}

	dir, err := CreateCommandDir(config)
	if err != nil {
		t.Fatalf("CreateCommandDir failed: %v", err)
	}
	defer os.RemoveAll(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
	if len(entries) > 0 && entries[0].Name() != "ls" {
		t.Errorf("expected entry 'ls', got %q", entries[0].Name())
	}
}

func TestCreateCommandDir_NonexistentCommand(t *testing.T) {
	config := &skill.SkillConfig{
		AllowedCommands: []string{"ls", "totally_nonexistent_command_xyz"},
	}

	dir, err := CreateCommandDir(config)
	if err != nil {
		t.Fatalf("CreateCommandDir failed: %v", err)
	}
	defer os.RemoveAll(dir)

	// Should have "ls" but not the nonexistent command
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("expected 1 entry (skipping nonexistent), got %d", len(entries))
	}
}

func TestCreateCommandDir_AllNonexistent(t *testing.T) {
	config := &skill.SkillConfig{
		AllowedCommands: []string{"nonexistent_a", "nonexistent_b"},
	}

	_, err := CreateCommandDir(config)
	if err == nil {
		t.Fatal("expected error when all commands are nonexistent")
	}
}

func TestConfigurePath_SetsToDir(t *testing.T) {
	env := SetupEnvironment()
	env.ConfigurePath("/tmp/test-cmd-dir")

	if env.Values["PATH"] != "/tmp/test-cmd-dir" {
		t.Errorf("expected PATH=/tmp/test-cmd-dir, got %q", env.Values["PATH"])
	}
}

func TestCreateWorkspaceStructure(t *testing.T) {
	dir, err := os.MkdirTemp("", "test-workspace-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	if err := CreateWorkspaceStructure(dir); err != nil {
		t.Fatalf("CreateWorkspaceStructure failed: %v", err)
	}

	for _, sub := range []string{"data", "code", "skills", "output"} {
		path := filepath.Join(dir, sub)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected %s to exist: %v", sub, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %s to be a directory", sub)
		}
	}
}
