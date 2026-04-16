package sandbox

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

func TestIsChildProcess_False(t *testing.T) {
	os.Unsetenv(childEnvKey)
	if IsChildProcess() {
		t.Error("expected IsChildProcess to return false when env var is not set")
	}
}

func TestIsChildProcess_True(t *testing.T) {
	os.Setenv(childEnvKey, "{}")
	defer os.Unsetenv(childEnvKey)

	if !IsChildProcess() {
		t.Error("expected IsChildProcess to return true when env var is set")
	}
}

func TestChildConfig_Serialization(t *testing.T) {
	config := ChildConfig{
		RootFSPath:    "/tmp/rootfs",
		WorkspacePath: "/home/user/workspace",
		Command:       "/bin/python3",
		Args:          []string{"runner.py", "skill", "prompt", "/skills"},
		Env:           []string{"PATH=/bin", "GOOGLE_API_KEY=key123"},
		Mounts: []MountEntry{
			{Source: "/usr", Target: "/tmp/rootfs/usr", FSType: "", Flags: bindReadOnly},
			{Source: "/home/user/workspace", Target: "/tmp/rootfs/workspace", FSType: "", Flags: bindReadWrite},
		},
	}

	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ChildConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.RootFSPath != config.RootFSPath {
		t.Errorf("RootFSPath mismatch: %q vs %q", decoded.RootFSPath, config.RootFSPath)
	}
	if decoded.Command != config.Command {
		t.Errorf("Command mismatch: %q vs %q", decoded.Command, config.Command)
	}
	if len(decoded.Args) != len(config.Args) {
		t.Errorf("Args length mismatch: %d vs %d", len(decoded.Args), len(config.Args))
	}
	if len(decoded.Mounts) != len(config.Mounts) {
		t.Errorf("Mounts length mismatch: %d vs %d", len(decoded.Mounts), len(config.Mounts))
	}
}

func TestSignalChildReady_WritesReadinessByte(t *testing.T) {
	readyR, readyW, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create readiness pipe: %v", err)
	}
	defer readyR.Close()

	t.Setenv(childReadyEnvKey, strconv.Itoa(int(readyW.Fd())))

	if err := signalChildReady(); err != nil {
		t.Fatalf("signalChildReady returned error: %v", err)
	}

	buf := make([]byte, 1)
	n, err := readyR.Read(buf)
	if err != nil {
		t.Fatalf("failed to read readiness byte: %v", err)
	}
	if n != 1 || buf[0] != '1' {
		t.Fatalf("expected readiness byte '1', got %q", buf[:n])
	}
}
