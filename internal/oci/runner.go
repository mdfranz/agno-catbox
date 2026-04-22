package oci

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/mdfranz/skill-runner/internal/skill"
)

// Config is the user-level description of a single skill run for the
// OCI-runtime path. It mirrors the fields of the namespace runner's Config
// so callers can swap implementations without restructuring their code.
type Config struct {
	RunID         string
	SkillConfig   *skill.SkillConfig
	WorkspacePath string // host workspace (bind-mounted rw at /workspace)
	Prompt        string
	Model         string
	Debug         bool

	// ImageDir is the OCI layout directory (built by `make image`).
	ImageDir string
	// RuntimeOverride picks a specific runtime binary; empty = auto (crun→runc).
	RuntimeOverride string
	// NetworkIsolated enables CLONE_NEWNET; default (false) keeps host network.
	NetworkIsolated bool

	// Env is the caller-assembled process env (after filtering).
	Env []string

	// Stdout/Stderr sinks for the container process.
	Stdout io.Writer
	Stderr io.Writer
}

// Run executes a single skill inside a freshly-unpacked OCI runtime bundle.
// Steps: verify image → pick runtime → build bundle → unpack rootfs →
// generate config.json → runtime run → cleanup.
func Run(ctx context.Context, cfg Config) error {
	start := time.Now()
	slog.Info("oci: starting run", "skill", cfg.SkillConfig.Name, "workspace", cfg.WorkspacePath)

	if err := EnsureImage(cfg.ImageDir); err != nil {
		return err
	}
	rt, err := DiscoverRuntime(cfg.RuntimeOverride)
	if err != nil {
		return err
	}
	slog.Info("oci: runtime located", "runtime", rt.Name, "path", rt.Path)

	absSkillDir, err := filepath.Abs(cfg.SkillConfig.Dir)
	if err != nil {
		return fmt.Errorf("resolve skill dir: %w", err)
	}
	absWorkspace, err := filepath.Abs(cfg.WorkspacePath)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	bundleParent := filepath.Join(absWorkspace, ".bundle")
	// Use cfg.RunID as the bundle name to allow reuse if RunID is fixed.
	bundle, err := NewBundle(bundleParent, cfg.RunID, cfg.Debug)
	if err != nil {
		return err
	}
	defer bundle.Close()

	// Only unpack if rootfs looks empty to allow reuse
	if _, err := os.Stat(filepath.Join(bundle.RootFSPath, "bin")); os.IsNotExist(err) {
		if err := UnpackRootFS(cfg.ImageDir, bundle.RootFSPath); err != nil {
			return fmt.Errorf("unpack rootfs: %w", err)
		}
	} else {
		slog.Info("oci: reusing existing rootfs", "path", bundle.RootFSPath)
	}

	args := []string{"python3", "/runner.py", cfg.SkillConfig.Name, cfg.Prompt, "/.skill"}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	if cfg.Debug {
		args = append(args, "--debug")
	}

	containerID := "skill-runner-oci-" + cfg.RunID
	if containerID == "skill-runner-oci-" {
		containerID = fmt.Sprintf("skill-runner-oci-%d", time.Now().UnixNano())
	}

	spec := BuildSpec(SpecConfig{
		Args:             args,
		Env:              cfg.Env,
		Cwd:              "/workspace",
		WorkspaceHost:    absWorkspace,
		SkillDirHost:     absSkillDir,
		MemoryLimitBytes: cfg.SkillConfig.ParsedMemory,
		CgroupsPath:      "skill-runner-oci/" + containerID,
		NetworkIsolated:  cfg.NetworkIsolated,
		HostUID:          os.Getuid(),
		HostGID:          os.Getgid(),
	})
	if err := WriteSpec(bundle.ConfigPath(), spec); err != nil {
		return err
	}

	err = rt.Run(ctx, RunArgs{
		ContainerID: containerID,
		BundleDir:   bundle.Dir,
		Timeout:     cfg.SkillConfig.ParsedTimeout,
		Stdout:      cfg.Stdout,
		Stderr:      cfg.Stderr,
	})
	dur := time.Since(start)
	if err != nil {
		slog.Error("oci: run failed", "error", err, "duration", dur.String())
		return err
	}
	slog.Info("oci: run completed", "duration", dur.String())
	return nil
}
