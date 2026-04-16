package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
}

const childEnvKey = "_SKILL_RUNNER_SANDBOX_CHILD"

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
		fmt.Fprintf(os.Stderr, "sandbox child: missing config\n")
		os.Exit(126)
	}

	var config ChildConfig
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		fmt.Fprintf(os.Stderr, "sandbox child: invalid config: %v\n", err)
		os.Exit(126)
	}

	if err := setupMountsAndPivot(config); err != nil {
		fmt.Fprintf(os.Stderr, "sandbox child: mount setup failed: %v\n", err)
		os.Exit(126)
	}

	// exec the real command inside the new root
	if err := syscall.Exec(config.Command, append([]string{config.Command}, config.Args...), config.Env); err != nil {
		fmt.Fprintf(os.Stderr, "sandbox child: exec failed: %v\n", err)
		os.Exit(126)
	}
}

func setupMountsAndPivot(config ChildConfig) error {
	rootfs := config.RootFSPath

	// Make the rootfs a mount point (required for pivot_root).
	// Bind-mount it onto itself.
	if err := syscall.Mount(rootfs, rootfs, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("bind mount rootfs onto itself: %w", err)
	}

	// Perform all bind mounts
	for _, m := range config.Mounts {
		// Ensure target exists. If it's a file mount, create the parent dir and then the file.
		sourceInfo, err := os.Stat(m.Source)
		if err != nil && m.Flags != mountProc && m.Flags != mountTmpfs {
			fmt.Fprintf(os.Stderr, "sandbox child: warning: stat source %s: %v\n", m.Source, err)
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
			if err := syscall.Mount(m.Source, m.Target, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
				// Non-fatal: the source might not exist on this system
				fmt.Fprintf(os.Stderr, "sandbox child: warning: bind mount %s -> %s: %v\n", m.Source, m.Target, err)
				continue
			}
			// Remount read-only
			if err := syscall.Mount("", m.Target, "", syscall.MS_BIND|syscall.MS_REMOUNT|syscall.MS_RDONLY|syscall.MS_REC, ""); err != nil {
				fmt.Fprintf(os.Stderr, "sandbox child: warning: remount ro %s: %v\n", m.Target, err)
			}

		case bindReadWrite:
			if err := syscall.Mount(m.Source, m.Target, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
				return fmt.Errorf("bind mount %s -> %s: %w", m.Source, m.Target, err)
			}

		case mountProc:
			if err := syscall.Mount(m.Source, m.Target, m.FSType, 0, ""); err != nil {
				fmt.Fprintf(os.Stderr, "sandbox child: warning: mount proc: %v\n", err)
			}

		case mountTmpfs:
			if err := syscall.Mount(m.Source, m.Target, m.FSType, syscall.MS_NOSUID|syscall.MS_NODEV, "size=64m"); err != nil {
				fmt.Fprintf(os.Stderr, "sandbox child: warning: mount tmpfs: %v\n", err)
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

	if err := syscall.PivotRoot(rootfs, oldRoot); err != nil {
		return fmt.Errorf("pivot_root: %w", err)
	}

	// chdir to new root
	if err := syscall.Chdir("/"); err != nil {
		return fmt.Errorf("chdir /: %w", err)
	}

	// Unmount old root and remove the mount point
	if err := syscall.Unmount("/.old_root", syscall.MNT_DETACH); err != nil {
		return fmt.Errorf("unmount old root: %w", err)
	}
	os.RemoveAll("/.old_root")

	// chdir to workspace
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
	_ = syscall.Mount(hostPath, targetPath, "", syscall.MS_BIND, "")
}
