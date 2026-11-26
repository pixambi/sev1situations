package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

const podmanInstallURL = "https://podman.io/docs/installation"

// podmanMachine is the part of `podman machine list --format json` we use.
type podmanMachine struct {
	Name     string `json:"Name"`
	Default  bool   `json:"Default"`
	Running  bool   `json:"Running"`
	Starting bool   `json:"Starting"`
}

// prereq reports whether Podman is installed and has a running machine. It
// prints what it found, and on failure what to do about it.
func prereq(ctx context.Context, stdout, stderr io.Writer, sys system) int {
	path, err := sys.LookPath("podman")
	if err != nil {
		fmt.Fprintf(stderr, "podman: not found. Install it from %s\n", podmanInstallURL)
		return exitFail
	}
	fmt.Fprintf(stdout, "podman: found at %s\n", path)

	out, err := sys.Output(ctx, "podman", "machine", "list", "--format", "json")
	if err != nil {
		fmt.Fprintf(stderr, "podman machine: could not list machines: %v\n", err)
		return exitFail
	}

	var machines []podmanMachine
	if err := json.Unmarshal(out, &machines); err != nil {
		fmt.Fprintf(stderr, "podman machine: unexpected output from podman: %v\n", err)
		return exitFail
	}

	if len(machines) == 0 {
		fmt.Fprintln(stderr, "podman machine: none exists. Run: podman machine init && podman machine start")
		return exitFail
	}

	chosen := pickMachine(machines)
	switch {
	case chosen.Running:
		fmt.Fprintf(stdout, "podman machine: %s is running\n", chosen.Name)
		return exitOK
	case chosen.Starting:
		fmt.Fprintf(stderr, "podman machine: %s is still starting. Try again in a moment\n", chosen.Name)
	default:
		fmt.Fprintf(stderr, "podman machine: %s is stopped. Run: podman machine start %s\n", chosen.Name, chosen.Name)
	}
	return exitFail
}

// pickMachine prefers a running machine, then the default one, then the first.
func pickMachine(machines []podmanMachine) podmanMachine {
	best := machines[0]
	for _, m := range machines[1:] {
		switch {
		case m.Running && !best.Running:
			best = m
		case m.Running == best.Running && m.Default && !best.Default:
			best = m
		}
	}
	return best
}
