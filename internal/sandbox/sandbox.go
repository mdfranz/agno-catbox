package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mdfranz/skill-runner/internal/skill"
)

// Runner manages the execution of a skill in a sandbox
type Runner struct {
	SkillConfig     *skill.SkillConfig
	WorkspacePath   string
	BaseWorkspace   string
	Prompt          string
	Model           string
	Debug           bool
	RunnerScript    string
	ChildLogWriter  io.Writer // when set, child stderr is tee'd here in addition to os.Stderr
}

var errNamespaceBootstrap = errors.New("namespace sandbox bootstrap failed")

// Run executes the skill inside a sandbox.
// It attempts full namespace isolation (user + mount + PID namespaces).
// If namespaces are unavailable, it falls back to symlink-based PATH
// restriction with process group controls.
func (r *Runner) Run(ctx context.Context) error {
	slog.Info("starting sandbox run", "skill", r.SkillConfig.Name, "workspace", r.WorkspacePath)
	if NamespacesAvailable() {
		slog.Info("sandbox mode: namespace isolation (user+mount+pid)")
		if err := r.runWithNamespaces(ctx); err == nil {
			slog.Info("sandbox run completed successfully")
			return nil
		} else if errors.Is(err, errNamespaceBootstrap) {
			slog.Warn("namespace bootstrap failed, falling back to PATH restriction only", "error", err)
			slog.Info("sandbox mode: fallback (PATH restriction only, host filesystem accessible)")
			return r.runWithoutNamespaces(ctx)
		} else {
			return err
		}
	}
	slog.Warn("namespace isolation unavailable, falling back to PATH restriction only")
	slog.Info("sandbox mode: fallback (PATH restriction only, host filesystem accessible)")
	err := r.runWithoutNamespaces(ctx)
	if err == nil {
		slog.Info("sandbox run completed successfully (fallback)")
	}
	return err
}

// runWithNamespaces uses the re-exec pattern with CLONE_NEWUSER + CLONE_NEWNS + CLONE_NEWPID
// to run the command inside a minimal rootfs with only allowed binaries visible.
func (r *Runner) runWithNamespaces(ctx context.Context) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	runnerPyPath, err := resolveRunnerScriptPath(r.RunnerScript, execPath)
	if err != nil {
		return err
	}

	absSkillDir, err := filepath.Abs(r.SkillConfig.Dir)
	if err != nil {
		absSkillDir = r.SkillConfig.Dir
	}

	// Prepare a minimal rootfs with only allowed binaries
	slog.Info("preparing rootfs")
	rootfs, err := PrepareRootFS(r.SkillConfig)
	if err != nil {
		return fmt.Errorf("failed to prepare rootfs: %w", err)
	}
	defer rootfs.Cleanup()
	slog.Debug("rootfs prepared", "path", rootfs.Path)

	// Copy runner.py into the rootfs so it's accessible after pivot_root
	rootfsRunnerPy := filepath.Join(rootfs.Path, "runner.py")
	if err := copyFile(runnerPyPath, rootfsRunnerPy); err != nil {
		return fmt.Errorf("failed to copy runner.py to rootfs: %w", err)
	}
	slog.Debug("copied runner script to rootfs", "src", runnerPyPath, "dst", rootfsRunnerPy)

	// Copy skill directory into rootfs
	rootfsSkillDir := filepath.Join(rootfs.Path, ".skill")
	if err := CopyDir(absSkillDir, rootfsSkillDir); err != nil {
		return fmt.Errorf("failed to copy skill dir to rootfs: %w", err)
	}
	slog.Debug("copied skill directory to rootfs", "src", absSkillDir, "dst", rootfsSkillDir)

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
		slog.Debug("using base workspace venv python", "path", venvPython)
	}
	envList := env.ToEnv()
	slog.Debug("prepared environment for child", "count", len(envList))

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
	slog.Debug("prepared child arguments", "args", childArgs)

	// Build the mount list for the child
	mounts := BindMountList(rootfs.Path, r.WorkspacePath, r.SkillConfig)
	slog.Debug("prepared mount list", "count", len(mounts))
	if r.Debug {
		for i, m := range mounts {
			slog.Debug("mount entry", "i", i, "source", m.Source, "target", m.Target, "type", m.Flags.String())
		}
	}

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
		Debug:         r.Debug,
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

	slog.Debug("os: Pipe")
	readyR, readyW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("failed to create readiness pipe: %w", err)
	}

	cmd := exec.CommandContext(ctx, selfExe)
	cmd.Env = append(
		envList,
		childEnvKey+"="+string(configJSON),
		childReadyEnvKey+"=3",
	)
	cmd.ExtraFiles = []*os.File{readyW}
	cmd.Stdout = os.Stdout
	cmd.Stderr = r.childStderr()
	cmd.Dir = r.WorkspacePath

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
	if err := SetupNamespaces(cmd.SysProcAttr); err != nil {
		readyW.Close()
		return fmt.Errorf("%w: failed to setup namespaces: %w", errNamespaceBootstrap, err)
	}

	slog.Info("starting namespaced child process", "exe", selfExe)
	if err := cmd.Start(); err != nil {
		readyW.Close()
		return fmt.Errorf("%w: failed to start namespaced process: %w", errNamespaceBootstrap, err)
	}
	readyW.Close()

	cleanup := r.attachCgroup(cmd)
	defer cleanup()

	if err := waitForNamespaceReady(ctx, cmd, readyR, r.SkillConfig.ParsedTimeout); err != nil {
		return err
	}

	return r.waitForCompletion(ctx, cmd)
}

// runWithoutNamespaces is the fallback when user namespaces are unavailable.
// Uses symlink-based PATH restriction + process group isolation.
func (r *Runner) runWithoutNamespaces(ctx context.Context) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	runnerPyPath, err := resolveRunnerScriptPath(r.RunnerScript, execPath)
	if err != nil {
		return err
	}

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
		slog.Debug("using base workspace venv python (fallback)", "path", pythonBin)
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
	cmd.Stderr = r.childStderr()

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}

	slog.Info("starting fallback process", "python", pythonBin)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	cleanup := r.attachCgroup(cmd)
	defer cleanup()

	return r.waitForCompletion(ctx, cmd)
}

func (r *Runner) attachCgroup(cmd *exec.Cmd) func() {
	if cmd.Process == nil {
		return func() {}
	}

	cgroupConfig := GetDefaultCgroupConfig(
		r.SkillConfig.ParsedMemory,
		int(r.SkillConfig.ParsedTimeout.Seconds()),
	)
	cgroupPath, err := cgroupConfig.CreateCgroupV2(cmd.Process.Pid)
	if err != nil {
		slog.Warn("cgroup resource limits not enforced", "error", err)
		return func() {}
	}

	slog.Debug("attached to cgroup", "path", cgroupPath)

	return func() {
		if cgroupPath != "" {
			_ = CleanupCgroup(cgroupPath)
		}
	}
}

func (r *Runner) waitForCompletion(ctx context.Context, cmd *exec.Cmd) error {
	doneChan := make(chan error, 1)
	go func() {
		doneChan <- cmd.Wait()
	}()

	select {
	case err := <-doneChan:
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				slog.Error("skill execution failed", "exit_code", exitErr.ExitCode())
			} else {
				slog.Error("skill execution failed", "error", err)
			}
			return fmt.Errorf("skill execution failed: %w", err)
		}
		slog.Info("skill execution successful")
		return nil
	case <-time.After(r.SkillConfig.ParsedTimeout):
		slog.Error("skill execution timed out", "timeout", r.SkillConfig.ParsedTimeout)
		killProcessGroup(cmd.Process.Pid)
		<-doneChan
		return fmt.Errorf("skill execution timed out after %s", r.SkillConfig.ParsedTimeout)
	case <-ctx.Done():
		slog.Error("sandbox run cancelled", "error", ctx.Err())
		killProcessGroup(cmd.Process.Pid)
		<-doneChan
		return ctx.Err()
	}
}

func waitForNamespaceReady(ctx context.Context, cmd *exec.Cmd, readyR *os.File, timeout time.Duration) error {
	defer readyR.Close()

	readyChan := make(chan error, 1)
	go func() {
		buf := make([]byte, 1)
		n, err := readyR.Read(buf)
		switch {
		case err == nil && n == 1 && buf[0] == '1':
			readyChan <- nil
		case err == io.EOF:
			readyChan <- errNamespaceBootstrap
		case err != nil:
			readyChan <- fmt.Errorf("failed waiting for namespace readiness: %w", err)
		default:
			readyChan <- fmt.Errorf("invalid readiness signal from namespace child")
		}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case err := <-readyChan:
		if err == nil {
			return nil
		}
		waitErr := cmd.Wait()
		if errors.Is(err, errNamespaceBootstrap) {
			return fmt.Errorf("%w: %v", errNamespaceBootstrap, waitErr)
		}
		return fmt.Errorf("namespace readiness failed: %w", err)
	case <-timer.C:
		killProcessGroup(cmd.Process.Pid)
		<-waitForExit(cmd)
		return fmt.Errorf("skill execution timed out after %s", timeout)
	case <-ctx.Done():
		killProcessGroup(cmd.Process.Pid)
		<-waitForExit(cmd)
		return ctx.Err()
	}
}

func waitForExit(cmd *exec.Cmd) <-chan error {
	doneChan := make(chan error, 1)
	go func() {
		doneChan <- cmd.Wait()
	}()
	return doneChan
}

func resolveRunnerScriptPath(configuredPath, executablePath string) (string, error) {
	if configuredPath != "" {
		info, err := os.Stat(configuredPath)
		if err != nil {
			return "", fmt.Errorf("failed to access runner script %q: %w", configuredPath, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("runner script %q is a directory", configuredPath)
		}
		return configuredPath, nil
	}

	adjacentPath := filepath.Join(filepath.Dir(executablePath), "runner.py")
	info, err := os.Stat(adjacentPath)
	if err != nil {
		return "", fmt.Errorf("runner.py not found next to the binary; pass -runner or set SKILL_RUNNER_PY")
	}
	if info.IsDir() {
		return "", fmt.Errorf("runner.py next to the binary is a directory; pass -runner or set SKILL_RUNNER_PY")
	}
	return adjacentPath, nil
}

// killProcessGroup sends SIGKILL to the entire process group.
func killProcessGroup(pid int) {
	slog.Debug("syscall: kill", "pid", -pid, "sig", "SIGKILL")
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	slog.Debug("syscall: kill", "pid", pid, "sig", "SIGKILL")
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

// childStderr returns the writer to use for the child process's stderr.
// When a ChildLogWriter is set, stderr is tee'd to both the terminal and the log.
func (r *Runner) childStderr() io.Writer {
	if r.ChildLogWriter != nil {
		return io.MultiWriter(os.Stderr, r.ChildLogWriter)
	}
	return os.Stderr
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
