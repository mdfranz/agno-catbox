package oci

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"time"
)

// ErrRuntimeMissing indicates neither crun nor runc are on PATH.
var ErrRuntimeMissing = errors.New("no OCI runtime found; install 'crun' (apt install crun / dnf install crun) or 'runc'")

// Runtime is a located OCI runtime binary (crun or runc).
type Runtime struct {
	Name string // "crun" or "runc"
	Path string // absolute path
}

// DiscoverRuntime returns crun if present, else runc, else ErrRuntimeMissing.
// If explicit is non-empty, it's used verbatim (resolved via LookPath).
func DiscoverRuntime(explicit string) (*Runtime, error) {
	candidates := []string{}
	if explicit != "" {
		candidates = append(candidates, explicit)
	} else {
		candidates = append(candidates, "crun", "runc")
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return &Runtime{Name: c, Path: p}, nil
		}
	}
	return nil, ErrRuntimeMissing
}

// RunArgs holds invocation settings for a single container run.
type RunArgs struct {
	ContainerID string
	BundleDir   string
	Timeout     time.Duration
	Stdout      io.Writer
	Stderr      io.Writer
}

// Run executes `<runtime> run --bundle <dir> <id>` and waits for it to finish.
// On timeout or cancellation the container is killed (SIGKILL) then deleted.
// The returned error describes the exit failure if any.
func (rt *Runtime) Run(ctx context.Context, args RunArgs) error {
	if args.Stdout == nil {
		args.Stdout = os.Stdout
	}
	if args.Stderr == nil {
		args.Stderr = os.Stderr
	}

	// Always ensure the container is deleted on exit.
	// runc delete --force will remove it even if it's still running or already stopped.
	defer rt.deleteContainer(args.ContainerID)

	runCtx := ctx
	var cancel context.CancelFunc
	if args.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, args.Timeout)
		defer cancel()
	}

	cmd := exec.Command(rt.Path, "run", "--bundle", args.BundleDir, args.ContainerID)
	cmd.Stdout = args.Stdout
	cmd.Stderr = args.Stderr

	slog.Info("oci: starting container", "runtime", rt.Name, "id", args.ContainerID, "bundle", args.BundleDir)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s run: %w", rt.Name, err)
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	select {
	case err := <-waitCh:
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return fmt.Errorf("container exited with code %d", exitErr.ExitCode())
			}
			return fmt.Errorf("container wait: %w", err)
		}
		slog.Info("oci: container finished", "id", args.ContainerID)
		return nil

	case <-runCtx.Done():
		reason := runCtx.Err()
		slog.Warn("oci: cancelling container", "id", args.ContainerID, "reason", reason)
		rt.killContainer(args.ContainerID)
		select {
		case <-waitCh:
		case <-time.After(5 * time.Second):
			slog.Warn("oci: container did not exit after SIGKILL; killing runtime process", "id", args.ContainerID)
			_ = cmd.Process.Kill()
			<-waitCh
		}
		if errors.Is(reason, context.DeadlineExceeded) {
			return fmt.Errorf("skill execution timed out after %s", args.Timeout)
		}
		return reason
	}
}

func (rt *Runtime) killContainer(id string) {
	slog.Debug("oci: killing container", "id", id)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, rt.Path, "kill", id, "KILL")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	_ = cmd.Run()
}

func (rt *Runtime) deleteContainer(id string) {
	slog.Debug("oci: deleting container", "id", id)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, rt.Path, "delete", "--force", id)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	_ = cmd.Run()
}
