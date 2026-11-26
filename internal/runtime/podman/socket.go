package podman

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/pixambi/sev1situations/internal/runtime"
)

// commandRunner runs a command and returns its standard output.
type commandRunner interface {
	LookPath(name string) (string, error)
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

type hostCommands struct{}

func (hostCommands) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (hostCommands) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// DiscoverSocket asks Podman where the default machine's API socket is. The
// path lives under $TMPDIR, which differs per user and session, so it is never
// hard-coded (spike S1). Failures match runtime.ErrUnavailable and say what to run.
func DiscoverSocket(ctx context.Context) (string, error) {
	return discoverSocket(ctx, hostCommands{})
}

func discoverSocket(ctx context.Context, cmds commandRunner) (string, error) {
	if _, err := cmds.LookPath("podman"); err != nil {
		return "", fmt.Errorf("%w: podman is not installed. Install it from https://podman.io/docs/installation", runtime.ErrUnavailable)
	}
	out, err := cmds.Output(ctx, "podman", "machine", "inspect", "--format", "{{.ConnectionInfo.PodmanSocket.Path}}")
	if err != nil {
		return "", fmt.Errorf("%w: no Podman machine found (%w). Run: podman machine init && podman machine start", runtime.ErrUnavailable, err)
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", fmt.Errorf("%w: the Podman machine reports no socket. Run: podman machine start", runtime.ErrUnavailable)
	}
	return path, nil
}
