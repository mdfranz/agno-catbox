package sandbox

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// CgroupConfig manages resource limits for the sandboxed process
type CgroupConfig struct {
	MaxMemoryBytes int64
	MaxCPUPercent  int
	TimeoutSeconds int
}

// CreateCgroupV2 sets up cgroup v2 limits for a process.
// Returns the cgroup path for cleanup and any error.
// Errors are real errors — the caller decides whether to treat them as fatal.
func (c *CgroupConfig) CreateCgroupV2(pid int) (string, error) {
	cgroupPath := filepath.Join("/sys/fs/cgroup", fmt.Sprintf("skill-runner-%d", pid))

	// Create cgroup directory
	slog.Debug("os: MkdirAll", "path", cgroupPath)
	if err := os.MkdirAll(cgroupPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create cgroup directory %s: %w", cgroupPath, err)
	}

	// Set memory limit
	if c.MaxMemoryBytes > 0 {
		if err := writeCgroupFile(filepath.Join(cgroupPath, "memory.max"),
			strconv.FormatInt(c.MaxMemoryBytes, 10)); err != nil {
			// Clean up the directory we created
			os.Remove(cgroupPath)
			return "", fmt.Errorf("failed to set memory limit: %w", err)
		}
	}

	// Set CPU limits (cgroup v2 uses cpu.max in format "quota period")
	if c.MaxCPUPercent > 0 {
		cpuMax := fmt.Sprintf("%d 100000", c.MaxCPUPercent*1000)
		if err := writeCgroupFile(filepath.Join(cgroupPath, "cpu.max"), cpuMax); err != nil {
			os.Remove(cgroupPath)
			return "", fmt.Errorf("failed to set CPU limit: %w", err)
		}
	}

	// Add process to the cgroup
	if err := writeCgroupFile(filepath.Join(cgroupPath, "cgroup.procs"),
		strconv.Itoa(pid)); err != nil {
		os.Remove(cgroupPath)
		return "", fmt.Errorf("failed to add pid %d to cgroup: %w", pid, err)
	}

	return cgroupPath, nil
}

// CleanupCgroup removes the cgroup directory
func CleanupCgroup(cgroupPath string) error {
	if cgroupPath == "" {
		return nil
	}

	// Wait briefly for processes to exit
	time.Sleep(100 * time.Millisecond)

	slog.Debug("os: Remove", "path", cgroupPath)
	if err := os.Remove(cgroupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove cgroup %s: %w", cgroupPath, err)
	}

	return nil
}

// writeCgroupFile writes a value to a cgroup control file
func writeCgroupFile(path, value string) error {
	slog.Debug("cgroup: writing file", "path", path, "value", value)
	f, err := os.OpenFile(path, os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.WriteString(f, value); err != nil {
		return err
	}

	return nil
}

// GetDefaultCgroupConfig returns default cgroup configuration from a skill
func GetDefaultCgroupConfig(maxMemoryBytes int64, timeoutSec int) *CgroupConfig {
	if maxMemoryBytes == 0 {
		maxMemoryBytes = 512 * 1024 * 1024 // 512MB default
	}
	if timeoutSec == 0 {
		timeoutSec = 60 // 60s default
	}

	return &CgroupConfig{
		MaxMemoryBytes: maxMemoryBytes,
		MaxCPUPercent:  100, // 1 CPU
		TimeoutSeconds: timeoutSec,
	}
}
