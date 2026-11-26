// Package app parses the command line and runs the headless subcommands
// (R-APP-5). The subcommands are thin wrappers over the same internal packages
// the window uses.
package app

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/pixambi/sev1situations/internal/version"
)

// Exit codes returned by Run.
const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

const usage = `Usage: s1s [command]

With no command, s1s opens the application window.

Commands:
  version    print the version, commit and build date
  prereq     check that Podman is installed and its machine is running
  scenario   list, validate and create scenarios (try: s1s scenario help)
  sandbox    deploy the Vault sandbox on Podman and tear it down again (temporary)
  help       print this message
`

// system is what the subcommands need from the machine they run on.
type system interface {
	// LookPath finds an executable on the PATH.
	LookPath(name string) (string, error)
	// Output runs a command and returns its standard output.
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Run executes the subcommand named by args[0] and returns the process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, opts Options) int {
	return run(ctx, args, stdout, stderr, hostSystem{}, opts)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, sys system, opts Options) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}

	switch args[0] {
	case "version":
		fmt.Fprintf(stdout, "s1s %s\n", version.String())
		return exitOK
	case "prereq":
		return prereq(ctx, stdout, stderr, sys)
	case "scenario":
		return scenarioCommand(args[1:], stdout, stderr, opts)
	case "sandbox":
		return sandboxCommand(ctx, args[1:], stdout, stderr, opts)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "s1s: unknown command %q\n\n%s", args[0], usage)
		return exitUsage
	}
}

// hostSystem runs commands on the real machine.
type hostSystem struct{}

func (hostSystem) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (hostSystem) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
