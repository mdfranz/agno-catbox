package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mdfranz/skill-runner/internal/skill"
)

// Runner manages the execution of a skill in a sandbox
type Runner struct {
	SkillConfig   *skill.SkillConfig
	WorkspacePath string
	BaseWorkspace string
	Prompt        string
	Model         string
	Debug         bool
}

// Run executes the skill inside a sandbox.
// It attempts full namespace isolation (user + mount + PID namespaces).
// If namespaces are unavailable, it falls back to symlink-based PATH
// restriction with process group controls.
func (r *Runner) Run(ctx context.Context) error {
	if NamespacesAvailable() {
		return r.runWithNamespaces(ctx)
	}
	fmt.Fprintf(os.Stderr, "Warning: namespace isolation unavailable, falling back to PATH restriction only\n")
	return r.runWithoutNamespaces(ctx)
}

// runWithNamespaces uses the re-exec pattern with CLONE_NEWUSER + CLONE_NEWNS + CLONE_NEWPID
// to run the command inside a minimal rootfs with only allowed binaries visible.
func (r *Runner) runWithNamespaces(ctx context.Context) error {
	// Find runner.py
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	execDir := filepath.Dir(execPath)
	runnerPyPath := filepath.Join(execDir, "runner.py")

	absSkillDir, err := filepath.Abs(r.SkillConfig.Dir)
	if err != nil {
		absSkillDir = r.SkillConfig.Dir
	}

	// Prepare a minimal rootfs with only allowed binaries
	rootfs, err := PrepareRootFS(r.SkillConfig)
	if err != nil {
		return fmt.Errorf("failed to prepare rootfs: %w", err)
	}
	defer rootfs.Cleanup()

	// Copy runner.py into the rootfs so it's accessible after pivot_root
	rootfsRunnerPy := filepath.Join(rootfs.Path, "runner.py")
	if err := copyFile(runnerPyPath, rootfsRunnerPy); err != nil {
		return fmt.Errorf("failed to copy runner.py to rootfs: %w", err)
	}

	// Copy skill directory into rootfs
	rootfsSkillDir := filepath.Join(rootfs.Path, ".skill")
	if err := CopyDir(absSkillDir, rootfsSkillDir); err != nil {
		return fmt.Errorf("failed to copy skill dir to rootfs: %w", err)
	}

	// Build the environment for the child
	env := SetupEnvironment()
	// Inside the namespace, PATH points to /bin (where we symlinked allowed commands)
	env.Values["PATH"] = "/bin:/usr/bin"
	
	pythonBin := "/bin/python3"
	
	// Check if the base workspace has a virtual environment.
	venvPath := filepath.Join(r.BaseWorkspace, ".venv")
	venvPython := filepath.Join(venvPath, "bin", "python3")
	hasVenv := false
	if _, err := os.Stat(venvPython); err == nil {
		hasVenv = true
		pythonBin = "/.venv/bin/python3"
		env.Values["VIRTUAL_ENV"] = "/.venv"
		if r.Debug {
			fmt.Fprintf(os.Stderr, "Debug: using base workspace venv python: %s\n", venvPython)
		}
	}
	envList := env.ToEnv()

	childArgs := []string{
		"/runner.py",
		r.SkillConfig.Name,
		r.Prompt,
		"/.skill",
	}
	if r.Model != "" {
		childArgs = append(childArgs, "--model", r.Model)
	}
	if r.Debug {
		childArgs = append(childArgs, "--debug")
	}

	// Build the mount list for the child
	mounts := BindMountList(rootfs.Path, r.WorkspacePath, r.SkillConfig)

	// Bind-mount the .venv if it exists
	if hasVenv {
		venvTarget := filepath.Join(rootfs.Path, ".venv")
		if err := os.MkdirAll(venvTarget, 0755); err != nil {
			return fmt.Errorf("failed to create .venv dir in rootfs: %w", err)
		}
		mounts = append(mounts, MountEntry{
			Source: venvPath,
			Target: venvTarget,
			FSType: "",
			Flags:  bindReadOnly,
		})
	}

	// Serialize the child config
	childConfig := ChildConfig{
		RootFSPath:    rootfs.Path,
		WorkspacePath: r.WorkspacePath,
		Command:       pythonBin,
		Args:          childArgs,
		Env:           envList,
		Mounts:        mounts,
	}
	configJSON, err := json.Marshal(childConfig)
	if err != nil {
		return fmt.Errorf("failed to serialize child config: %w", err)
	}

	// Re-exec ourselves with the child config in an env var.
	// The child process will detect IsChildProcess(), set up mounts,
	// pivot_root, and exec the real command.
	selfExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get self executable: %w", err)
	}

	cmd := exec.CommandContext(ctx, selfExe)
	cmd.Env = append(envList, childEnvKey+"="+string(configJSON))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = r.WorkspacePath

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
	if err := SetupNamespaces(cmd.SysProcAttr); err != nil {
		return fmt.Errorf("failed to setup namespaces: %w", err)
	}

	return r.startAndWait(ctx, cmd)
}

// runWithoutNamespaces is the fallback when user namespaces are unavailable.
// Uses symlink-based PATH restriction + process group isolation.
func (r *Runner) runWithoutNamespaces(ctx context.Context) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	execDir := filepath.Dir(execPath)
	runnerPyPath := filepath.Join(execDir, "runner.py")

	absSkillDir, err := filepath.Abs(r.SkillConfig.Dir)
	if err != nil {
		absSkillDir = r.SkillConfig.Dir
	}

	// Create symlink-based command directory (only allowed binaries)
	cmdDir, err := CreateCommandDir(r.SkillConfig)
	if err != nil {
		return fmt.Errorf("failed to create command directory: %w", err)
	}
	defer os.RemoveAll(cmdDir)

	pythonBin := "python3"
	venvPath := filepath.Join(r.BaseWorkspace, ".venv")
	venvPython := filepath.Join(venvPath, "bin", "python3")
	if _, err := os.Stat(venvPython); err == nil {
		pythonBin = venvPython
		if r.Debug {
			fmt.Fprintf(os.Stderr, "Debug: using base workspace venv python (fallback): %s\n", pythonBin)
		}
	}

	pythonArgs := []string{
		runnerPyPath,
		r.SkillConfig.Name,
		r.Prompt,
		absSkillDir,
	}
	if r.Model != "" {
		pythonArgs = append(pythonArgs, "--model", r.Model)
	}
	if r.Debug {
		pythonArgs = append(pythonArgs, "--debug")
	}

	cmd := exec.CommandContext(ctx, pythonBin, pythonArgs...)

	env := SetupEnvironment()
	env.ConfigurePath(cmdDir)
	// Add VIRTUAL_ENV if we're using a venv so Python sets up paths correctly
	if pythonBin == venvPython {
		env.Values["VIRTUAL_ENV"] = venvPath
	}
	cmd.Env = env.ToEnv()
	cmd.Dir = r.WorkspacePath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}

	return r.startAndWait(ctx, cmd)
}

// startAndWait starts a command, optionally applies cgroup limits, and
// waits for completion with timeout. Kills the process group on timeout.
func (r *Runner) startAndWait(ctx context.Context, cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	// Try to set cgroup limits after process starts
	var cgroupPath string
	if cmd.Process != nil {
		cgroupConfig := GetDefaultCgroupConfig(
			r.SkillConfig.ParsedMemory,
			int(r.SkillConfig.ParsedTimeout.Seconds()),
		)
		var err error
		cgroupPath, err = cgroupConfig.CreateCgroupV2(cmd.Process.Pid)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: cgroup resource limits not enforced: %v\n", err)
		}
		if cgroupPath != "" {
			defer CleanupCgroup(cgroupPath)
		}
	}

	doneChan := make(chan error, 1)
	go func() {
		doneChan <- cmd.Wait()
	}()

	select {
	case err := <-doneChan:
		if err != nil {
			return fmt.Errorf("skill execution failed: %w", err)
		}
		return nil
	case <-time.After(r.SkillConfig.ParsedTimeout):
		killProcessGroup(cmd.Process.Pid)
		<-doneChan
		return fmt.Errorf("skill execution timed out after %s", r.SkillConfig.ParsedTimeout)
	case <-ctx.Done():
		killProcessGroup(cmd.Process.Pid)
		<-doneChan
		return ctx.Err()
	}
}

// killProcessGroup sends SIGKILL to the entire process group.
func killProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

// copyFile copies a single file.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0755)
}

// CopyDir copies a directory tree.
func CopyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}
