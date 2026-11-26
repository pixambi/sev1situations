package primitive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pixambi/sev1situations/internal/runtime"
)

// container.exec: run a command in a node.
type containerExec struct{}

func (containerExec) Spec() Spec {
	return Spec{
		Name:    "container.exec",
		Summary: "Runs a command in a node and checks its exit code and output.",
		Timeout: 30 * time.Second,
		Changes: true, // safe to run again only if the command is
		Args: []Arg{
			argNode,
			{Name: "command", Type: StringList, Required: true, Help: "The command and its arguments, as a list. It is not run through a shell."},
			{Name: "expectExit", Type: Int, Default: 0, Help: "The exit code that counts as a pass."},
			{Name: "contains", Type: String, Help: "If set, the combined output must contain this text."},
			{Name: "user", Type: String, Help: "Run as this user."},
			{Name: "workDir", Type: String, Help: "The working directory."},
			{Name: "env", Type: Map, Help: "Extra environment variables for the command."},
			{Name: "save", Type: String, Help: "If set, standard output (trimmed) is kept as .Outputs.<name>."},
		},
	}
}

func (containerExec) Run(ctx context.Context, env Env, a Args) (Result, error) {
	n, err := env.node(a.String("node"))
	if err != nil {
		return Result{}, err
	}
	envVars := map[string]string{}
	for k, v := range a.Map("env") {
		envVars[k] = fmt.Sprint(v)
	}

	res, err := env.Runtime.Exec(ctx, n.ContainerID, a.List("command"), runtime.ExecOptions{
		User: a.String("user"), WorkDir: a.String("workDir"), Env: envVars,
	})
	if err != nil {
		return Result{}, fmt.Errorf("run in %s: %w", n.Name, err)
	}

	combined := res.Stdout + res.Stderr
	out := Result{Evidence: truncate(combined)}
	if name := a.String("save"); name != "" {
		out.Outputs = map[string]string{name: strings.TrimSpace(res.Stdout)}
	}
	switch want := a.Int("expectExit"); {
	case res.ExitCode != want:
		out.Status, out.Message = Fail, fmt.Sprintf("%s exited with %d, expected %d", n.Name, res.ExitCode, want)
	case a.String("contains") != "" && !strings.Contains(combined, a.String("contains")):
		out.Status, out.Message = Fail, fmt.Sprintf("the output of %s does not contain %q", n.Name, a.String("contains"))
	default:
		out.Status, out.Message = Pass, fmt.Sprintf("%s exited with %d", n.Name, res.ExitCode)
	}
	return out, nil
}

var argNodes = Arg{Name: "nodes", Type: StringList, Required: true, Help: "The nodes to act on, for example [node-2, node-3]."}

// actOnNodes applies one runtime operation to each named node and reports them
// all. Missing is whether a container that is already gone counts as done.
func actOnNodes(ctx context.Context, env Env, names []string, verb string, missingOK bool, op func(context.Context, string) error) (Result, error) {
	var errs []error
	for _, name := range names {
		n, err := env.node(name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		err = op(ctx, n.ContainerID)
		if err != nil && (!missingOK || !errors.Is(err, runtime.ErrNotFound)) {
			errs = append(errs, fmt.Errorf("%s %s: %w", verb, name, err))
		}
	}
	if err := joinErrors(errs); err != nil {
		return Result{}, err
	}
	return passed("%s %s", verb, strings.Join(names, ", ")), nil
}

type containerStop struct{}

func (containerStop) Spec() Spec {
	return Spec{
		Name: "container.stop", Summary: "Stops nodes. Stopping a stopped node is not an error, so it is safe to run again.",
		Timeout: 30 * time.Second, Changes: true, Args: []Arg{argNodes},
	}
}

func (containerStop) Run(ctx context.Context, env Env, a Args) (Result, error) {
	return actOnNodes(ctx, env, a.List("nodes"), "stopped", false, env.Runtime.Stop)
}

type containerStart struct{}

func (containerStart) Spec() Spec {
	return Spec{
		Name: "container.start", Summary: "Starts stopped nodes. Starting a running node is not an error, so it is safe to run again.",
		Timeout: 30 * time.Second, Changes: true, Args: []Arg{argNodes},
	}
}

func (containerStart) Run(ctx context.Context, env Env, a Args) (Result, error) {
	return actOnNodes(ctx, env, a.List("nodes"), "started", false, env.Runtime.Start)
}

type containerRemove struct{}

func (containerRemove) Spec() Spec {
	return Spec{
		Name: "container.remove", Summary: "Removes nodes, stopping them first. A node that is already gone counts as removed, so it is safe to run again.",
		Timeout: 30 * time.Second, Changes: true, Args: []Arg{argNodes},
	}
}

func (containerRemove) Run(ctx context.Context, env Env, a Args) (Result, error) {
	return actOnNodes(ctx, env, a.List("nodes"), "removed", true, env.Runtime.Remove)
}

type containerKillProcess struct{}

func (containerKillProcess) Spec() Spec {
	return Spec{
		Name:    "container.kill-process",
		Summary: "Sends a signal to a process by name inside a node. It fails if no such process is running.",
		Timeout: 15 * time.Second,
		Changes: true, // a second run fails, because the process is already gone
		Args: []Arg{
			argNode,
			{Name: "process", Type: String, Required: true, Help: "The exact name of the process, for example vault."},
			{Name: "signal", Type: String, Default: "KILL", Help: "The signal name without SIG, for example KILL or TERM."},
		},
	}
}

func (containerKillProcess) Run(ctx context.Context, env Env, a Args) (Result, error) {
	n, err := env.node(a.String("node"))
	if err != nil {
		return Result{}, err
	}
	sig := strings.ToUpper(strings.TrimPrefix(strings.ToUpper(a.String("signal")), "SIG"))
	if sig == "" || strings.Trim(sig, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") != "" {
		return Result{}, fmt.Errorf("signal %q is not a signal name such as KILL or TERM", a.String("signal"))
	}

	res, err := env.Runtime.Exec(ctx, n.ContainerID, []string{"pkill", "-" + sig, "-x", a.String("process")}, runtime.ExecOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("run pkill in %s: %w", n.Name, err)
	}
	switch res.ExitCode {
	case 0:
		return passed("sent %s to %s in %s", sig, a.String("process"), n.Name), nil
	case 1:
		return failed(res.Stdout+res.Stderr, "no process named %q is running in %s", a.String("process"), n.Name), nil
	default:
		return Result{}, fmt.Errorf("pkill in %s exited with %d: %s", n.Name, res.ExitCode, strings.TrimSpace(res.Stderr))
	}
}

type containerCopyIn struct{}

func (containerCopyIn) Spec() Spec {
	return Spec{
		Name:    "container.copy-in",
		Summary: "Copies a file from the scenario's folder into a node. Copying again overwrites the file, so it is safe to run again.",
		Timeout: 30 * time.Second,
		Changes: true,
		Args: []Arg{
			argNode,
			{Name: "source", Type: String, Required: true, Help: "The file's path inside the scenario folder, for example fixtures/data.json."},
			{Name: "path", Type: String, Required: true, Help: "The absolute destination path inside the node."},
			{Name: "mode", Type: String, Default: "0644", Help: "The file mode in octal, for example 0755 for an executable."},
		},
	}
}

func (containerCopyIn) Run(ctx context.Context, env Env, a Args) (Result, error) {
	if env.ReadFile == nil {
		return Result{}, errors.New("this run has no scenario files to copy from")
	}
	data, err := env.ReadFile(a.String("source"))
	if err != nil {
		return Result{}, fmt.Errorf("read %s from the scenario: %w", a.String("source"), err)
	}
	return copyFile(ctx, env, a.String("node"), a.String("path"), a.String("mode"), data)
}

// copyFile writes bytes to a path in a node.
func copyFile(ctx context.Context, env Env, node, dest, mode string, data []byte) (Result, error) {
	n, err := env.node(node)
	if err != nil {
		return Result{}, err
	}
	m, err := parseMode(mode)
	if err != nil {
		return Result{}, err
	}
	err = env.Runtime.Copy(ctx, n.ContainerID, runtime.File{Path: dest, Mode: m, Size: int64(len(data)), Content: bytes.NewReader(data)})
	if err != nil {
		return Result{}, fmt.Errorf("copy to %s:%s: %w", n.Name, dest, err)
	}
	return passed("wrote %s on %s (%d bytes)", dest, n.Name, len(data)), nil
}
