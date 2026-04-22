package oci

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// Bundle is a per-run OCI runtime bundle directory.
// The on-disk layout is:
//
//	<Dir>/
//	  config.json
//	  rootfs/...
type Bundle struct {
	Dir        string
	RootFSPath string
	Retain     bool
}

// NewBundle creates or reuses a bundle directory.
// If name is provided, the directory is fixed; otherwise MkdirTemp is used.
// retain=true suppresses cleanup on Close (useful for --debug or reuse).
func NewBundle(parent, name string, retain bool) (*Bundle, error) {
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create bundle parent %s: %w", parent, err)
	}

	var dir string
	if name != "" {
		dir = filepath.Join(parent, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create bundle dir: %w", err)
		}
	} else {
		var err error
		dir, err = os.MkdirTemp(parent, "bundle-*")
		if err != nil {
			return nil, fmt.Errorf("failed to create bundle dir: %w", err)
		}
	}

	rootfs := filepath.Join(dir, "rootfs")
	if err := os.MkdirAll(rootfs, 0o755); err != nil {
		if name == "" {
			os.RemoveAll(dir)
		}
		return nil, fmt.Errorf("failed to create rootfs dir: %w", err)
	}
	slog.Debug("oci: bundle initialized", "dir", dir, "fixed", name != "")
	return &Bundle{Dir: dir, RootFSPath: rootfs, Retain: retain || name != ""}, nil
}

// ConfigPath returns the absolute path to config.json within the bundle.
func (b *Bundle) ConfigPath() string {
	return filepath.Join(b.Dir, "config.json")
}

// Close removes the bundle unless Retain is true.
func (b *Bundle) Close() error {
	if b == nil {
		return nil
	}
	if b.Retain {
		slog.Info("oci: bundle retained", "dir", b.Dir)
		return nil
	}
	slog.Debug("oci: bundle cleanup", "dir", b.Dir)
	return os.RemoveAll(b.Dir)
}
