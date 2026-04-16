package sandbox

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mdfranz/skill-runner/internal/skill"
)

// RootFS holds the path to a prepared minimal root filesystem.
type RootFS struct {
	Path string // temp dir containing the rootfs
}

// PrepareRootFS creates a minimal rootfs directory structure and resolves
// which host paths need to be bind-mounted into it. The actual mounting
// happens in the child process (inside the mount namespace).
//
// The rootfs layout:
//
//	/bin/           -> symlinks to allowed command binaries
//	/lib/           -> (marker; bind-mounted from host)
//	/lib64/         -> (marker; bind-mounted from host)
//	/usr/           -> (marker; bind-mounted from host /usr)
//	/proc/          -> (marker; mount proc)
//	/dev/           -> (marker; minimal device nodes)
//	/tmp/           -> (marker; tmpfs)
//	/workspace/     -> (marker; bind-mounted from host workspace)
//	/etc/           -> minimal files
func PrepareRootFS(skillConfig *skill.SkillConfig) (*RootFS, error) {
	rootDir, err := os.MkdirTemp("", "skill-runner-rootfs-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create rootfs temp dir: %w", err)
	}

	// Create the directory skeleton
	dirs := []string{
		"bin", "lib", "lib64", "usr",
		"proc", "dev", "tmp", "workspace", "etc",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(rootDir, d), 0755); err != nil {
			os.RemoveAll(rootDir)
			return nil, fmt.Errorf("failed to create rootfs dir %s: %w", d, err)
		}
	}

	// Resolve allowed commands and symlink them into /bin
	for _, cmd := range skillConfig.AllowedCommands {
		realPath, err := exec.LookPath(cmd)
		if err != nil {
			continue
		}
		realPath, err = filepath.Abs(realPath)
		if err != nil {
			continue
		}
		// Follow symlinks to get the true binary path
		resolved, err := filepath.EvalSymlinks(realPath)
		if err != nil {
			resolved = realPath
		}

		linkPath := filepath.Join(rootDir, "bin", cmd)

		// If the binary is in /usr or /lib, we can symlink it because those are bind-mounted.
		// Otherwise (e.g. /home or /usr/local), we MUST copy it into the rootfs.
		if strings.HasPrefix(resolved, "/usr") || strings.HasPrefix(resolved, "/lib") {
			if err := os.Symlink(resolved, linkPath); err != nil {
				os.RemoveAll(rootDir)
				return nil, fmt.Errorf("failed to symlink %s: %w", cmd, err)
			}
		} else {
			if err := copyFile(resolved, linkPath); err != nil {
				os.RemoveAll(rootDir)
				return nil, fmt.Errorf("failed to copy %s: %w", cmd, err)
			}
		}
	}

	// Write a minimal /etc/passwd so tools that need it don't fail
	passwdContent := "root:x:0:0:root:/workspace:/bin/sh\nnobody:x:65534:65534:nobody:/:/bin/false\n"
	if err := os.WriteFile(filepath.Join(rootDir, "etc", "passwd"), []byte(passwdContent), 0644); err != nil {
		os.RemoveAll(rootDir)
		return nil, fmt.Errorf("failed to write /etc/passwd: %w", err)
	}

	// Write a minimal /etc/group
	groupContent := "root:x:0:\nnobody:x:65534:\n"
	if err := os.WriteFile(filepath.Join(rootDir, "etc", "group"), []byte(groupContent), 0644); err != nil {
		os.RemoveAll(rootDir)
		return nil, fmt.Errorf("failed to write /etc/group: %w", err)
	}

	return &RootFS{Path: rootDir}, nil
}

// Cleanup removes the rootfs temp directory.
func (r *RootFS) Cleanup() {
	if r != nil && r.Path != "" {
		os.RemoveAll(r.Path)
	}
}

// ResolveLibraryDirs returns the set of host directories that contain shared
// libraries needed by the allowed commands. We parse ldd output to find them.
func ResolveLibraryDirs(skillConfig *skill.SkillConfig) []string {
	seen := make(map[string]bool)
	var dirs []string

	for _, cmd := range skillConfig.AllowedCommands {
		realPath, err := exec.LookPath(cmd)
		if err != nil {
			continue
		}
		for _, dir := range lddDirs(realPath) {
			if !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
	}

	// Always include the standard library paths as fallback
	for _, std := range []string{"/lib", "/lib64", "/usr/lib"} {
		if info, err := os.Stat(std); err == nil && info.IsDir() {
			if !seen[std] {
				seen[std] = true
				dirs = append(dirs, std)
			}
		}
	}

	return dirs
}

// lddDirs runs ldd on a binary and returns the unique directories containing
// its shared library dependencies.
func lddDirs(binaryPath string) []string {
	out, err := exec.Command("ldd", binaryPath).Output()
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var dirs []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// ldd output lines look like: libfoo.so.1 => /usr/lib/libfoo.so.1 (0x...)
		// or: /lib/ld-linux.so.2 (0x...)
		parts := strings.Fields(line)
		for _, p := range parts {
			if strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "(") {
				dir := filepath.Dir(p)
				if !seen[dir] {
					seen[dir] = true
					dirs = append(dirs, dir)
				}
			}
		}
	}
	return dirs
}

// BindMountList returns the list of (source, target) pairs that the child
// process needs to bind-mount inside the new mount namespace.
func BindMountList(rootfsPath, workspacePath string, skillConfig *skill.SkillConfig) []MountEntry {
	var mounts []MountEntry

	// Bind-mount library directories (read-only)
	libDirs := ResolveLibraryDirs(skillConfig)
	for _, dir := range libDirs {
		target := filepath.Join(rootfsPath, dir)
		mounts = append(mounts, MountEntry{
			Source: dir,
			Target: target,
			FSType: "",
			Flags:  bindReadOnly,
		})
	}

	// Bind-mount /usr for Python and other interpreters that need it
	if info, err := os.Stat("/usr"); err == nil && info.IsDir() {
		mounts = append(mounts, MountEntry{
			Source: "/usr",
			Target: filepath.Join(rootfsPath, "usr"),
			FSType: "",
			Flags:  bindReadOnly,
		})
	}

	// Bind-mount workspace (read-write)
	mounts = append(mounts, MountEntry{
		Source: workspacePath,
		Target: filepath.Join(rootfsPath, "workspace"),
		FSType: "",
		Flags:  bindReadWrite,
	})

	// Bind-mount /etc/resolv.conf for DNS resolution
	if _, err := os.Stat("/etc/resolv.conf"); err == nil {
		mounts = append(mounts, MountEntry{
			Source: "/etc/resolv.conf",
			Target: filepath.Join(rootfsPath, "etc", "resolv.conf"),
			FSType: "",
			Flags:  bindReadOnly,
		})
	}

	// Mount proc
	mounts = append(mounts, MountEntry{
		Source: "proc",
		Target: filepath.Join(rootfsPath, "proc"),
		FSType: "proc",
		Flags:  mountProc,
	})

	// Mount tmpfs on /tmp
	mounts = append(mounts, MountEntry{
		Source: "tmpfs",
		Target: filepath.Join(rootfsPath, "tmp"),
		FSType: "tmpfs",
		Flags:  mountTmpfs,
	})

	return mounts
}

// MountEntry describes a single mount operation for the child.
type MountEntry struct {
	Source string
	Target string
	FSType string
	Flags  mountFlags
}

type mountFlags int

const (
	bindReadOnly  mountFlags = iota
	bindReadWrite
	mountProc
	mountTmpfs
)
