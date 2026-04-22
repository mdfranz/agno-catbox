package oci

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// UnpackRootFS reads the OCI image layout at imageDir, picks the first
// manifest in index.json, and extracts its layers (in order) into destRoot.
//
// Whiteouts (.wh.* entries) are honored so upper-layer deletions apply.
// Symlinks, hard links, directories, and regular files are extracted.
// Device nodes and FIFOs are skipped — unprivileged extraction can't create them
// and the Python workload doesn't need them (the OCI runtime mounts /dev itself).
func UnpackRootFS(imageDir, destRoot string) error {
	lp, err := layout.FromPath(imageDir)
	if err != nil {
		return fmt.Errorf("open OCI layout %s: %w", imageDir, err)
	}
	idx, err := lp.ImageIndex()
	if err != nil {
		return fmt.Errorf("read image index: %w", err)
	}
	img, err := pickImage(idx)
	if err != nil {
		return err
	}
	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("list layers: %w", err)
	}
	slog.Info("oci: extracting rootfs", "image_dir", imageDir, "layers", len(layers), "dest", destRoot)
	for i, l := range layers {
		if err := extractLayer(l, destRoot); err != nil {
			return fmt.Errorf("extract layer %d: %w", i, err)
		}
	}
	return nil
}

// pickImage selects the first image-typed manifest from the index,
// resolving nested indexes by descending into their first entry.
func pickImage(idx v1.ImageIndex) (v1.Image, error) {
	manifest, err := idx.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("read index manifest: %w", err)
	}
	if len(manifest.Manifests) == 0 {
		return nil, errors.New("OCI index has no manifests")
	}
	for _, desc := range manifest.Manifests {
		switch desc.MediaType {
		case types.OCIManifestSchema1, types.DockerManifestSchema2:
			return idx.Image(desc.Digest)
		case types.OCIImageIndex, types.DockerManifestList:
			inner, err := idx.ImageIndex(desc.Digest)
			if err != nil {
				continue
			}
			return pickImage(inner)
		}
	}
	// Fall back to trying each descriptor as an image.
	for _, desc := range manifest.Manifests {
		if img, err := idx.Image(desc.Digest); err == nil {
			return img, nil
		}
	}
	return nil, errors.New("no usable image manifest in OCI index")
}

func extractLayer(l v1.Layer, destRoot string) error {
	rc, err := l.Uncompressed()
	if err != nil {
		return fmt.Errorf("open uncompressed layer: %w", err)
	}
	defer rc.Close()

	tr := tar.NewReader(rc)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}
		if err := applyEntry(tr, hdr, destRoot); err != nil {
			return err
		}
	}
}

// applyEntry writes a single tar entry, handling whiteouts and path safety.
func applyEntry(tr *tar.Reader, hdr *tar.Header, destRoot string) error {
	cleanName := filepath.Clean(hdr.Name)
	if strings.HasPrefix(cleanName, "..") || strings.Contains(cleanName, string(os.PathSeparator)+"..") {
		return fmt.Errorf("unsafe tar path: %s", hdr.Name)
	}

	base := filepath.Base(cleanName)
	parent := filepath.Dir(cleanName)

	// OCI/Docker whiteout markers.
	if strings.HasPrefix(base, ".wh..wh..opq") {
		// Opaque directory whiteout: remove everything currently in parent dir.
		target := filepath.Join(destRoot, parent)
		return clearDirContents(target)
	}
	if strings.HasPrefix(base, ".wh.") {
		// Regular whiteout: remove the matching path from destRoot.
		name := strings.TrimPrefix(base, ".wh.")
		target := filepath.Join(destRoot, parent, name)
		return os.RemoveAll(target)
	}

	target := filepath.Join(destRoot, cleanName)
	mode := hdr.FileInfo().Mode()

	switch hdr.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, mode.Perm())
	case tar.TypeReg, tar.TypeRegA:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		// Replace any existing entry so later layers win.
		_ = os.Remove(target)
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
		if err != nil {
			return fmt.Errorf("create %s: %w", target, err)
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return fmt.Errorf("write %s: %w", target, err)
		}
		return f.Close()
	case tar.TypeSymlink:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		_ = os.Remove(target)
		return os.Symlink(hdr.Linkname, target)
	case tar.TypeLink:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		_ = os.Remove(target)
		return os.Link(filepath.Join(destRoot, filepath.Clean(hdr.Linkname)), target)
	case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
		// Skip device/FIFO nodes — unprivileged extraction can't create them
		// and they're provided by the runtime's /dev mounts at container start.
		return nil
	default:
		// Unknown/unsupported types are silently skipped.
		return nil
	}
}

func clearDirContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
