package oci

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrImageMissing is returned when the OCI layout directory is absent or empty.
var ErrImageMissing = errors.New("OCI image layout not found")

// EnsureImage verifies that dir is a usable OCI layout directory.
// A valid layout must contain both index.json and oci-layout.
func EnsureImage(dir string) error {
	if dir == "" {
		return fmt.Errorf("%w: image directory not configured", ErrImageMissing)
	}
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s does not exist (run 'make image' or 'skill-runner-oci image build')", ErrImageMissing, dir)
		}
		return fmt.Errorf("failed to stat image dir %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrImageMissing, dir)
	}
	for _, f := range []string{"index.json", "oci-layout"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			return fmt.Errorf("%w: %s/%s missing (run 'make image')", ErrImageMissing, dir, f)
		}
	}
	return nil
}

// DefaultImageDir resolves the OCI layout directory from an explicit path,
// the SKILL_RUNNER_IMAGE_DIR env var, or <exeDir>/image in that order.
func DefaultImageDir(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	if v := os.Getenv("SKILL_RUNNER_IMAGE_DIR"); v != "" {
		return filepath.Abs(v)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "image"), nil
}
