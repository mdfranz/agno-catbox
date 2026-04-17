package sandbox

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// ChildConfig is serialized to an env var and passed to the re-exec'd child.
type ChildConfig struct {
	RootFSPath    string       `json:"rootfs_path"`
	WorkspacePath string       `json:"workspace_path"`
	Command       string       `json:"command"`
	Args          []string     `json:"args"`
	Env           []string     `json:"env"`
	Mounts        []MountEntry `json:"mounts"`
	Debug         bool         `json:"debug"`
}

const childEnvKey = "_SKILL_RUNNER_SANDBOX_CHILD"
const childReadyEnvKey = "_SKILL_RUNNER_CHILD_READY_FD"
const namespaceBootstrapExitCode = 125

// IsChildProcess returns true if this process is a re-exec'd sandbox child.
func IsChildProcess() bool {
	return os.Getenv(childEnvKey) != ""
}

// RunChild is called when the process detects it is a sandbox child.
// It sets up the mount namespace (bind mounts, pivot_root) and then
// exec's the real command. This function does not return on success.
func RunChild() {
	configJSON := os.Getenv(childEnvKey)
	if configJSON == "" {
		slog.Error("sandbox child: missing config")
		os.Exit(namespaceBootstrapExitCode)
	}

	var config ChildConfig
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		slog.Error("sandbox child: invalid config", "error", err)
		os.Exit(namespaceBootstrapExitCode)
	}

	// Initialize child logger to write structured logs to stderr.
	// The parent process captures this stderr.
	logLevel := slog.LevelInfo
	if config.Debug {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	if err := setupMountsAndPivot(config); err != nil {
		slog.Error("sandbox child: mount setup failed", "error", err)
		os.Exit(namespaceBootstrapExitCode)
	}
	slog.Debug("sandbox child: mounts and pivot_root successful")

	if err := signalChildReady(); err != nil {
		slog.Error("sandbox child: failed to signal readiness", "error", err)
		os.Exit(namespaceBootstrapExitCode)
	}
	slog.Debug("sandbox child: signaled readiness to parent")

	// exec the real command inside the new root
	slog.Info("sandbox child: executing command", "cmd", config.Command, "args", config.Args)
	if err := syscall.Exec(config.Command, append([]string{config.Command}, config.Args...), config.Env); err != nil {
		slog.Error("sandbox child: exec failed", "error", err)
		os.Exit(namespaceBootstrapExitCode)
	}
}

func signalChildReady() error {
	fdStr := os.Getenv(childReadyEnvKey)
	if fdStr == "" {
		return nil
	}

	fd, err := strconv.Atoi(fdStr)
	if err != nil {
		return fmt.Errorf("invalid ready fd %q: %w", fdStr, err)
	}

	readyFile := os.NewFile(uintptr(fd), "sandbox-child-ready")
	if readyFile == nil {
		return fmt.Errorf("failed to open ready fd %d", fd)
	}
	defer readyFile.Close()

	if _, err := readyFile.Write([]byte{'1'}); err != nil {
		return err
	}

	return nil
}

func setupMountsAndPivot(config ChildConfig) error {
	// Make the mount namespace private. This ensures that any mounts done
	// inside this namespace do not propagate back to the host, even if
	// the host has shared mount points.
	if err := syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("make mount namespace private: %w", err)
	}

	rootfs := config.RootFSPath

	// Make the rootfs a mount point (required for pivot_root).
	// Bind-mount it onto itself.
	slog.Debug("syscall: mount", "source", rootfs, "target", rootfs, "flags", "MS_BIND|MS_REC")
	if err := syscall.Mount(rootfs, rootfs, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("bind mount rootfs onto itself: %w", err)
	}

	// Perform all bind mounts
	for i, m := range config.Mounts {
		slog.Debug("sandbox child: mounting", "i", i, "type", m.Flags.String(), "source", m.Source, "target", m.Target)

		// Ensure target exists. If it's a file mount, create the parent dir and then the file.
		sourceInfo, err := os.Stat(m.Source)
		if err != nil && m.Flags != mountProc && m.Flags != mountTmpfs {
			slog.Warn("sandbox child: stat source failed", "source", m.Source, "error", err)
			continue
		}

		if sourceInfo != nil && !sourceInfo.IsDir() {
			// It's a file. Create parent dir and empty file.
			if err := os.MkdirAll(filepath.Dir(m.Target), 0755); err != nil {
				return fmt.Errorf("mkdir parent %s: %w", filepath.Dir(m.Target), err)
			}
			f, err := os.OpenFile(m.Target, os.O_CREATE|os.O_RDWR, 0644)
			if err != nil {
				return fmt.Errorf("create target file %s: %w", m.Target, err)
			}
			f.Close()
		} else {
			// It's a directory (or proc/tmpfs). Create it.
			if err := os.MkdirAll(m.Target, 0755); err != nil {
				return fmt.Errorf("mkdir %s: %w", m.Target, err)
			}
		}

		switch m.Flags {
		case bindReadOnly:
			slog.Debug("syscall: mount", "source", m.Source, "target", m.Target, "flags", "MS_BIND|MS_REC")
			if err := syscall.Mount(m.Source, m.Target, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
				// Non-fatal: the source might not exist on this system
				slog.Warn("sandbox child: bind mount failed", "source", m.Source, "target", m.Target, "error", err)
				continue
			}
			// Remount read-only
			slog.Debug("syscall: mount", "source", "", "target", m.Target, "flags", "MS_BIND|MS_REMOUNT|MS_RDONLY|MS_REC")
			if err := syscall.Mount("", m.Target, "", syscall.MS_BIND|syscall.MS_REMOUNT|syscall.MS_RDONLY|syscall.MS_REC, ""); err != nil {
				slog.Warn("sandbox child: remount ro failed", "target", m.Target, "error", err)
			} else {
				slog.Debug("sandbox child: mounted ok", "target", m.Target)
			}

		case bindReadWrite:
			slog.Debug("syscall: mount", "source", m.Source, "target", m.Target, "flags", "MS_BIND|MS_REC")
			if err := syscall.Mount(m.Source, m.Target, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
				return fmt.Errorf("bind mount %s -> %s: %w", m.Source, m.Target, err)
			}
			slog.Debug("sandbox child: mounted ok", "target", m.Target)

		case mountProc:
			slog.Debug("syscall: mount", "source", m.Source, "target", m.Target, "fstype", m.FSType)
			if err := syscall.Mount(m.Source, m.Target, m.FSType, 0, ""); err != nil {
				slog.Warn("sandbox child: mount proc failed", "error", err)
			} else {
				slog.Debug("sandbox child: mounted ok", "target", m.Target)
			}

		case mountTmpfs:
			slog.Debug("syscall: mount", "source", m.Source, "target", m.Target, "fstype", m.FSType, "flags", "MS_NOSUID|MS_NODEV")
			if err := syscall.Mount(m.Source, m.Target, m.FSType, syscall.MS_NOSUID|syscall.MS_NODEV, "size=64m"); err != nil {
				slog.Warn("sandbox child: mount tmpfs failed", "error", err)
			} else {
				slog.Debug("sandbox child: mounted ok", "target", m.Target)
			}
		}
	}

	// Create minimal device nodes by bind-mounting from the host's /dev.
	// This must happen BEFORE pivot_root while host paths are still accessible.
	// Inside a user namespace, mknod is not allowed, so bind-mount is the only option.
	devDir := filepath.Join(rootfs, "dev")
	bindDevNode(devDir, "null")
	bindDevNode(devDir, "zero")
	bindDevNode(devDir, "urandom")

	// pivot_root: swap the root filesystem
	// We need an old_root directory inside the new root to pivot onto
	oldRoot := filepath.Join(rootfs, ".old_root")
	if err := os.MkdirAll(oldRoot, 0700); err != nil {
		return fmt.Errorf("mkdir old_root: %w", err)
	}

	slog.Debug("syscall: pivot_root", "new_root", rootfs, "old_root", oldRoot)
	if err := syscall.PivotRoot(rootfs, oldRoot); err != nil {
		return fmt.Errorf("pivot_root: %w", err)
	}

	// chdir to new root
	slog.Debug("syscall: chdir", "path", "/")
	if err := syscall.Chdir("/"); err != nil {
		return fmt.Errorf("chdir /: %w", err)
	}

	// Unmount old root and remove the mount point
	slog.Debug("syscall: unmount", "target", "/.old_root", "flags", "MNT_DETACH")
	if err := syscall.Unmount("/.old_root", syscall.MNT_DETACH); err != nil {
		return fmt.Errorf("unmount old root: %w", err)
	}
	os.RemoveAll("/.old_root")
	slog.Debug("sandbox child: old root unmounted, pivot complete")

	// chdir to workspace
	slog.Debug("syscall: chdir", "path", "/workspace")
	if err := syscall.Chdir("/workspace"); err != nil {
		return fmt.Errorf("chdir /workspace: %w", err)
	}

	return nil
}

// bindDevNode bind-mounts a device node from the host's /dev into the new rootfs.
// Must be called BEFORE pivot_root while host /dev is still at /dev.
// Inside a user namespace, mknod is not allowed, so bind-mount is the only option.
func bindDevNode(devDir, name string) {
	hostPath := filepath.Join("/dev", name)
	targetPath := filepath.Join(devDir, name)

	// Create the target file to mount onto
	f, err := os.Create(targetPath)
	if err != nil {
		return
	}
	f.Close()

	// Bind mount the host device
	slog.Debug("syscall: mount", "source", hostPath, "target", targetPath, "flags", "MS_BIND")
	_ = syscall.Mount(hostPath, targetPath, "", syscall.MS_BIND, "")
}
