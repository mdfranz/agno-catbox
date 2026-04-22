package oci

import (
	"encoding/json"
	"fmt"
	"os"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// SpecConfig is the minimal information needed to build a runtime-spec
// config.json for a skill run.
type SpecConfig struct {
	// Process
	Args []string
	Env  []string
	Cwd  string

	// Mounts
	WorkspaceHost string // host dir, bind-mounted rw at /workspace
	SkillDirHost  string // host dir, bind-mounted ro at /.skill

	// Resources
	MemoryLimitBytes int64
	CgroupsPath      string

	// Network
	// When false (default), the container shares the host network namespace
	// by omitting the "network" entry from linux.namespaces.
	NetworkIsolated bool

	// UID/GID mapping: map container root (0) to the invoking uid/gid on host.
	HostUID int
	HostGID int
}

// BuildSpec produces a runtime-spec Spec populated for running the Agno
// Python runner inside the container image rootfs.
func BuildSpec(cfg SpecConfig) *specs.Spec {
	var memLimit *int64
	if cfg.MemoryLimitBytes > 0 {
		m := cfg.MemoryLimitBytes
		memLimit = &m
	}

	namespaces := []specs.LinuxNamespace{
		{Type: specs.PIDNamespace},
		{Type: specs.MountNamespace},
		{Type: specs.IPCNamespace},
		{Type: specs.UTSNamespace},
		{Type: specs.CgroupNamespace},
		{Type: specs.UserNamespace},
	}
	if cfg.NetworkIsolated {
		namespaces = append(namespaces, specs.LinuxNamespace{Type: specs.NetworkNamespace})
	}

	s := &specs.Spec{
		Version: specs.Version,
		Process: &specs.Process{
			Terminal: false,
			User:     specs.User{UID: 0, GID: 0},
			Args:     cfg.Args,
			Env:      cfg.Env,
			Cwd:      cfg.Cwd,
			Capabilities: &specs.LinuxCapabilities{
				Bounding:  defaultCaps(),
				Effective: defaultCaps(),
				Permitted: defaultCaps(),
				Ambient:   defaultCaps(),
			},
			NoNewPrivileges: true,
		},
		Root: &specs.Root{
			Path:     "rootfs",
			Readonly: true,
		},
		Hostname: "skill-runner",
		Mounts: []specs.Mount{
			{Destination: "/proc", Type: "proc", Source: "proc", Options: []string{"nosuid", "noexec", "nodev"}},
			{Destination: "/dev", Type: "tmpfs", Source: "tmpfs", Options: []string{"nosuid", "strictatime", "mode=755", "size=65536k"}},
			{Destination: "/dev/pts", Type: "devpts", Source: "devpts", Options: []string{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620"}},
			{Destination: "/dev/shm", Type: "tmpfs", Source: "shm", Options: []string{"nosuid", "noexec", "nodev", "mode=1777", "size=65536k"}},
			{Destination: "/dev/mqueue", Type: "mqueue", Source: "mqueue", Options: []string{"nosuid", "noexec", "nodev"}},
			{Destination: "/sys", Type: "none", Source: "/sys", Options: []string{"rbind", "nosuid", "noexec", "nodev", "ro"}},
			{Destination: "/tmp", Type: "tmpfs", Source: "tmpfs", Options: []string{"nosuid", "nodev", "mode=1777", "size=134217728"}},
			{Destination: "/workspace", Type: "bind", Source: cfg.WorkspaceHost, Options: []string{"rbind", "rw"}},
			{Destination: "/.skill", Type: "bind", Source: cfg.SkillDirHost, Options: []string{"rbind", "ro"}},
		},
		Linux: &specs.Linux{
			Namespaces: namespaces,
			UIDMappings: []specs.LinuxIDMapping{
				{ContainerID: 0, HostID: uint32(cfg.HostUID), Size: 1},
			},
			GIDMappings: []specs.LinuxIDMapping{
				{ContainerID: 0, HostID: uint32(cfg.HostGID), Size: 1},
			},
			CgroupsPath: cfg.CgroupsPath,
			Resources: &specs.LinuxResources{
				Memory: &specs.LinuxMemory{
					Limit: memLimit,
				},
			},
			MaskedPaths: []string{
				"/proc/acpi",
				"/proc/asound",
				"/proc/kcore",
				"/proc/keys",
				"/proc/latency_stats",
				"/proc/timer_list",
				"/proc/timer_stats",
				"/proc/sched_debug",
				"/proc/scsi",
				"/sys/firmware",
			},
			ReadonlyPaths: []string{
				"/proc/bus",
				"/proc/fs",
				"/proc/irq",
				"/proc/sys",
				"/proc/sysrq-trigger",
			},
		},
	}
	return s
}

// WriteSpec marshals the spec to the given path as indented JSON.
func WriteSpec(path string, s *specs.Spec) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal spec: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

func defaultCaps() []string {
	return []string{
		"CAP_CHOWN",
		"CAP_DAC_OVERRIDE",
		"CAP_FSETID",
		"CAP_FOWNER",
		"CAP_MKNOD",
		"CAP_NET_RAW",
		"CAP_SETGID",
		"CAP_SETUID",
		"CAP_SETFCAP",
		"CAP_SETPCAP",
		"CAP_NET_BIND_SERVICE",
		"CAP_SYS_CHROOT",
		"CAP_KILL",
		"CAP_AUDIT_WRITE",
	}
}
