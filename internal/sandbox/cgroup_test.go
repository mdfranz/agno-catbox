package sandbox

import (
	"testing"
)

func TestGetDefaultCgroupConfig_Defaults(t *testing.T) {
	cfg := GetDefaultCgroupConfig(0, 0)

	if cfg.MaxMemoryBytes != 512*1024*1024 {
		t.Errorf("expected default memory 512MB, got %d", cfg.MaxMemoryBytes)
	}
	if cfg.TimeoutSeconds != 60 {
		t.Errorf("expected default timeout 60s, got %d", cfg.TimeoutSeconds)
	}
	if cfg.MaxCPUPercent != 100 {
		t.Errorf("expected default CPU 100%%, got %d", cfg.MaxCPUPercent)
	}
}

func TestGetDefaultCgroupConfig_CustomValues(t *testing.T) {
	cfg := GetDefaultCgroupConfig(1024*1024*1024, 300) // 1GB, 300s

	if cfg.MaxMemoryBytes != 1024*1024*1024 {
		t.Errorf("expected 1GB memory, got %d", cfg.MaxMemoryBytes)
	}
	if cfg.TimeoutSeconds != 300 {
		t.Errorf("expected 300s timeout, got %d", cfg.TimeoutSeconds)
	}
}

func TestCreateCgroupV2_FailsGracefully(t *testing.T) {
	// On most test systems, writing to /sys/fs/cgroup will fail.
	// This test verifies the function returns an error rather than nil.
	cfg := GetDefaultCgroupConfig(512*1024*1024, 60)

	path, err := cfg.CreateCgroupV2(99999)
	if err == nil && path != "" {
		// If it succeeded (running as root on a cgroup-enabled system), clean up
		CleanupCgroup(path)
	}
	// The key assertion: if it failed, it should return an error, not ("", nil)
	if path == "" && err == nil {
		t.Error("CreateCgroupV2 returned empty path with nil error — should return an error on failure")
	}
}
