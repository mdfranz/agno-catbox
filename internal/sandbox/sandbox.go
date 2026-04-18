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
	"strings"
	"syscall"
	"time"

	"github.com/mdfranz/skill-runner/internal/skill"
)

// Runner manages the execution of a skill in a sandbox
type Runner struct {
	RunID           string
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
        start := time.Now()
        slog.Info("starting sandbox run", "skill", r.SkillConfig.Name, "workspace", r.WorkspacePath)

        var err error
        if NamespacesAvailable() {
                slog.Info("sandbox mode: namespace isolation (user+mount+pid)")
                err = r.runWithNamespaces(ctx)
                if err != nil && errors.Is(err, errNamespaceBootstrap) {
                        slog.Warn("namespace bootstrap failed, falling back to PATH restriction only", "error", err)
                        slog.Info("sandbox mode: fallback (PATH restriction only, host filesystem accessible)")
                        err = r.runWithoutNamespaces(ctx)
                }
        } else {
                slog.Warn("namespace isolation unavailable, falling back to PATH restriction only")
                slog.Info("sandbox mode: fallback (PATH restriction only, host filesystem accessible)")
                err = r.runWithoutNamespaces(ctx)
        }

        duration := time.Since(start)
        if err == nil {
                slog.Info("sandbox run completed successfully", "duration", duration.String())
        } else {
                slog.Error("sandbox run failed", "error", err, "duration", duration.String())
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
	slog.Info("rootfs prepared", "path", rootfs.Path)

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
	        RunID:         r.RunID,
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
	cmd.Stdout = r.childStdout()
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

	slog.Info("starting namespaced child process", "exe", selfExe, "python", pythonBin)
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
	cmd.Stdout = r.childStdout()
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
	                        return fmt.Errorf("skill execution failed with exit code %d", exitErr.ExitCode())
	                } else if errors.Is(err, syscall.EINVAL) || strings.Contains(err.Error(), "invalid argument") {
	                        // EINVAL (invalid argument) can occur on some Linux kernels when waiting for a process
	                        // that was PID 1 in a namespace that has already been torn down.
	                        // If we got here, it means the wait is over and the process likely finished successfully.
	                        slog.Warn("cmd.Wait() returned EINVAL/invalid argument; assuming process finished", "pid", cmd.Process.Pid, "error", err)
	                        return nil
	                } else {
	                        slog.Error("skill execution failed", "error", err)
	                        return fmt.Errorf("skill execution failed: %w", err)
	                }
	        }
	        slog.Info("skill execution successful", "pid", cmd.Process.Pid)
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

// childStdout returns the writer to use for the child process's stdout.
func (r *Runner) childStdout() io.Writer {
	if r.ChildLogWriter == nil {
		return os.Stdout
	}
	return io.MultiWriter(os.Stdout, r.ChildLogWriter)
}

// childStderr returns the writer to use for the child process's stderr.
// When a ChildLogWriter is set, stderr is always written to it.
// We only tee to the terminal if the output does NOT look like a structured JSON log,
// to avoid cluttering the terminal with sandbox internal logs.
func (r *Runner) childStderr() io.Writer {
	if r.ChildLogWriter == nil {
		return os.Stderr
	}

	return &filteredTeeWriter{
		terminal: os.Stderr,
		logFile:  r.ChildLogWriter,
	}
}

type filteredTeeWriter struct {
	terminal io.Writer
	logFile  io.Writer
	buf      []byte
}

func (w *filteredTeeWriter) Write(p []byte) (n int, err error) {
	// Always write everything to the log file
	if _, err := w.logFile.Write(p); err != nil {
		return 0, err
	}

	// For the terminal, we want to filter out lines that look like structured JSON logs
	// (which start with '{"time":' or simply '{' in our case).
	// This is a simple heuristic: if a chunk starts with '{', we don't write it to terminal.
	// We handle the buffer to check for the start of lines.

	w.buf = append(w.buf, p...)

	start := 0
	for i := 0; i < len(w.buf); i++ {
		if w.buf[i] == '\n' || i == len(w.buf)-1 {
			line := w.buf[start : i+1]
			trimmed := strings.TrimSpace(string(line))
			if !strings.HasPrefix(trimmed, "{") {
				_, _ = w.terminal.Write(line)
			}
			start = i + 1
		}
	}

	if start < len(w.buf) {
		w.buf = w.buf[start:]
	} else {
		w.buf = nil
	}

	return len(p), nil
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

// CopyDir copies a directory tree. It avoids recursion by skipping the destination directory
// if it is a subdirectory of the source. It handles symlinks by creating new symlinks,
// and uses streaming copy for files to avoid loading large files into memory.
func CopyDir(src, dst string) error {
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return err
	}

	return filepath.Walk(absSrc, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Avoid recursion: skip the destination dir itself, or any ancestor of it
		// that lives under src (e.g. the "runs/" parent when dst is "runs/run-xxx/").
		if path == absDst || strings.HasPrefix(absDst, path+string(os.PathSeparator)) {
			return filepath.SkipDir
		}

		rel, err := filepath.Rel(absSrc, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		target := filepath.Join(absDst, rel)

		switch mode := info.Mode(); {
		case mode.IsDir():
			return os.MkdirAll(target, mode.Perm())
		case mode&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case mode.IsRegular():
			return copyFileStreaming(path, target, mode.Perm())
		default:
			// Skip other special files (pipes, sockets, devices)
			return nil
		}
	})
}

// copyFileStreaming copies a single file using io.Copy.
func copyFileStreaming(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	d, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer d.Close()

	_, err = io.Copy(d, s)
	return err
}
