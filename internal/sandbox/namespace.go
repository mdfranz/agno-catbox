package sandbox

import (
	"fmt"
	"os"
	"syscall"
)

// NamespacesAvailable checks whether unprivileged user namespaces are supported.
// Returns true if the kernel allows the current user to create user namespaces.
func NamespacesAvailable() bool {
	// Check the sysctl that controls unprivileged user namespace creation.
	// If the file doesn't exist, the kernel doesn't gate it (older kernels allow it).
	// If it exists and contains "0", user namespaces are disabled for unprivileged users.
	data, err := os.ReadFile("/proc/sys/kernel/unprivileged_userns_clone")
	if err != nil {
		// File doesn't exist — either the kernel doesn't have this sysctl (namespaces
		// likely available) or /proc isn't mounted (bigger problems). Try optimistically.
		return true
	}
	if len(data) > 0 && data[0] == '0' {
		return false
	}
	return true
}

// SetupNamespaces configures SysProcAttr for process isolation.
// If user namespaces are available, it enables:
//   - CLONE_NEWUSER: separate UID/GID mappings (enables mount namespace without root)
//   - CLONE_NEWNS: separate mount namespace (for pivot_root)
//   - CLONE_NEWPID: separate PID namespace (clean process view, helps cleanup)
//
// If namespaces are unavailable, it returns an error — the caller decides whether
// to treat that as fatal or continue with reduced isolation.
func SetupNamespaces(sysProcAttr *syscall.SysProcAttr) error {
	if !NamespacesAvailable() {
		return fmt.Errorf("unprivileged user namespaces not available (check /proc/sys/kernel/unprivileged_userns_clone)")
	}

	uid := os.Getuid()
	gid := os.Getgid()

	sysProcAttr.Cloneflags |= syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS | syscall.CLONE_NEWPID

	// Map the current user to root (UID 0) inside the namespace.
	// This gives us permission to mount/pivot_root inside the namespace
	// without actual root on the host.
	sysProcAttr.UidMappings = []syscall.SysProcIDMap{
		{ContainerID: 0, HostID: uid, Size: 1},
	}
	sysProcAttr.GidMappings = []syscall.SysProcIDMap{
		{ContainerID: 0, HostID: gid, Size: 1},
	}

	// GidMappingsEnableSetgroups must be false when writing gid_map
	// without first writing "deny" to /proc/pid/setgroups (which Go handles
	// internally when this is false).
	sysProcAttr.GidMappingsEnableSetgroups = false

	return nil
}
