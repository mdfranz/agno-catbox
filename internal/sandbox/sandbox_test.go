package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRunnerScriptPath_UsesConfiguredPath(t *testing.T) {
	dir := t.TempDir()
	runnerPath := filepath.Join(dir, "custom-runner.py")
	if err := os.WriteFile(runnerPath, []byte("print('ok')\n"), 0644); err != nil {
		t.Fatalf("failed to write runner script: %v", err)
	}

	resolved, err := resolveRunnerScriptPath(runnerPath, filepath.Join(dir, "skill-runner"))
	if err != nil {
		t.Fatalf("resolveRunnerScriptPath returned error: %v", err)
	}
	if resolved != runnerPath {
		t.Fatalf("expected %q, got %q", runnerPath, resolved)
	}
}

func TestResolveRunnerScriptPath_UsesAdjacentRunner(t *testing.T) {
	dir := t.TempDir()
	executablePath := filepath.Join(dir, "skill-runner")
	runnerPath := filepath.Join(dir, "runner.py")

	if err := os.WriteFile(runnerPath, []byte("print('ok')\n"), 0644); err != nil {
		t.Fatalf("failed to write adjacent runner script: %v", err)
	}

	resolved, err := resolveRunnerScriptPath("", executablePath)
	if err != nil {
		t.Fatalf("resolveRunnerScriptPath returned error: %v", err)
	}
	if resolved != runnerPath {
		t.Fatalf("expected %q, got %q", runnerPath, resolved)
	}
}

func TestResolveRunnerScriptPath_MissingRunnerReturnsError(t *testing.T) {
	dir := t.TempDir()

	_, err := resolveRunnerScriptPath("", filepath.Join(dir, "skill-runner"))
	if err == nil {
		t.Fatal("expected error when no configured or adjacent runner exists")
	}
}
