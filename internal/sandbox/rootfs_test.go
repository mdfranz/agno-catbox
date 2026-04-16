package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mdfranz/skill-runner/internal/skill"
)

func TestPrepareRootFS_CreatesStructure(t *testing.T) {
	config := &skill.SkillConfig{
		AllowedCommands: []string{"ls", "echo"},
	}

	rootfs, err := PrepareRootFS(config)
	if err != nil {
		t.Fatalf("PrepareRootFS failed: %v", err)
	}
	defer rootfs.Cleanup()

	// Verify directory structure
	expectedDirs := []string{"bin", "lib", "lib64", "usr", "proc", "dev", "tmp", "workspace", "etc"}
	for _, d := range expectedDirs {
		path := filepath.Join(rootfs.Path, d)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected dir %s to exist: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %s to be a directory", d)
		}
	}
}

func TestPrepareRootFS_SymlinksCommands(t *testing.T) {
	config := &skill.SkillConfig{
		AllowedCommands: []string{"ls"},
	}

	rootfs, err := PrepareRootFS(config)
	if err != nil {
		t.Fatalf("PrepareRootFS failed: %v", err)
	}
	defer rootfs.Cleanup()

	lsLink := filepath.Join(rootfs.Path, "bin", "ls")
	info, err := os.Lstat(lsLink)
	if err != nil {
		t.Fatalf("expected symlink for ls: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("expected ls to be a symlink")
	}
}

func TestPrepareRootFS_WritesEtcFiles(t *testing.T) {
	config := &skill.SkillConfig{
		AllowedCommands: []string{"ls"},
	}

	rootfs, err := PrepareRootFS(config)
	if err != nil {
		t.Fatalf("PrepareRootFS failed: %v", err)
	}
	defer rootfs.Cleanup()

	// Check /etc/passwd exists
	data, err := os.ReadFile(filepath.Join(rootfs.Path, "etc", "passwd"))
	if err != nil {
		t.Fatalf("failed to read etc/passwd: %v", err)
	}
	if len(data) == 0 {
		t.Error("etc/passwd is empty")
	}

	// Check /etc/group exists
	data, err = os.ReadFile(filepath.Join(rootfs.Path, "etc", "group"))
	if err != nil {
		t.Fatalf("failed to read etc/group: %v", err)
	}
	if len(data) == 0 {
		t.Error("etc/group is empty")
	}
}

func TestPrepareRootFS_Cleanup(t *testing.T) {
	config := &skill.SkillConfig{
		AllowedCommands: []string{"ls"},
	}

	rootfs, err := PrepareRootFS(config)
	if err != nil {
		t.Fatalf("PrepareRootFS failed: %v", err)
	}

	path := rootfs.Path
	rootfs.Cleanup()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected rootfs dir to be removed after cleanup, but it still exists")
	}
}

func TestResolveLibraryDirs_IncludesStandardPaths(t *testing.T) {
	config := &skill.SkillConfig{
		AllowedCommands: []string{"ls"},
	}

	dirs := ResolveLibraryDirs(config)
	if len(dirs) == 0 {
		t.Fatal("expected at least some library directories")
	}

	// At least one of /lib, /lib64, /usr/lib should be present
	hasStdLib := false
	for _, d := range dirs {
		if d == "/lib" || d == "/lib64" || d == "/usr/lib" {
			hasStdLib = true
			break
		}
	}
	if !hasStdLib {
		t.Errorf("expected at least one standard lib dir, got: %v", dirs)
	}
}

func TestBindMountList_ContainsWorkspace(t *testing.T) {
	config := &skill.SkillConfig{
		AllowedCommands: []string{"ls"},
	}

	mounts := BindMountList("/tmp/test-rootfs", "/home/user/workspace", config)

	hasWorkspace := false
	for _, m := range mounts {
		if m.Source == "/home/user/workspace" && m.Flags == bindReadWrite {
			hasWorkspace = true
			break
		}
	}
	if !hasWorkspace {
		t.Error("expected workspace bind mount in mount list")
	}
}

func TestBindMountList_ContainsProc(t *testing.T) {
	config := &skill.SkillConfig{
		AllowedCommands: []string{"ls"},
	}

	mounts := BindMountList("/tmp/test-rootfs", "/home/user/workspace", config)

	hasProc := false
	for _, m := range mounts {
		if m.FSType == "proc" {
			hasProc = true
			break
		}
	}
	if !hasProc {
		t.Error("expected proc mount in mount list")
	}
}
