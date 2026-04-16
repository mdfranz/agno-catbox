package sandbox

import (
	"os"
	"syscall"
	"testing"
)

func TestNamespacesAvailable_ReturnsBoolean(t *testing.T) {
	// Just verify it doesn't panic and returns a bool
	result := NamespacesAvailable()
	t.Logf("NamespacesAvailable: %v", result)
}

func TestSetupNamespaces_SetsCloneflags(t *testing.T) {
	if !NamespacesAvailable() {
		t.Skip("user namespaces not available on this system")
	}

	attr := &syscall.SysProcAttr{}
	err := SetupNamespaces(attr)
	if err != nil {
		t.Fatalf("SetupNamespaces failed: %v", err)
	}

	if attr.Cloneflags&syscall.CLONE_NEWUSER == 0 {
		t.Error("expected CLONE_NEWUSER flag")
	}
	if attr.Cloneflags&syscall.CLONE_NEWNS == 0 {
		t.Error("expected CLONE_NEWNS flag")
	}
	if attr.Cloneflags&syscall.CLONE_NEWPID == 0 {
		t.Error("expected CLONE_NEWPID flag")
	}

	if len(attr.UidMappings) != 1 {
		t.Fatalf("expected 1 UID mapping, got %d", len(attr.UidMappings))
	}
	if attr.UidMappings[0].ContainerID != 0 {
		t.Error("expected container UID 0")
	}
	if attr.UidMappings[0].HostID != os.Getuid() {
		t.Errorf("expected host UID %d, got %d", os.Getuid(), attr.UidMappings[0].HostID)
	}

	if len(attr.GidMappings) != 1 {
		t.Fatalf("expected 1 GID mapping, got %d", len(attr.GidMappings))
	}
	if attr.GidMappings[0].HostID != os.Getgid() {
		t.Errorf("expected host GID %d, got %d", os.Getgid(), attr.GidMappings[0].HostID)
	}
}
