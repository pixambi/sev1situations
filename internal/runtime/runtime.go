// Package runtime describes containers, networks, execs and logs as Go types
// and one interface, Runtime (R-RUN-3). The rest of the application depends on
// this package, never on Podman, so everything above it can be tested with the
// fake in internal/runtime/fake.
//
// Every object a Runtime creates carries the labels in this package, and a
// Runtime must never touch an object that does not carry them (R-RUN-4).
//
// The interface follows the sketch in section 3.5 of the project definition,
// with these changes that building it showed to be needed: Copy takes a File so
// the mode and size are known (a binary must be executable, and archives need
// the size up front); Attach returns a Terminal that can be resized; ExecOptions
// can detach, which is how a product process is started; EnsureImage pulls on demand; and Ping and Runs
// exist for the start-up checks and leftover cleanup (R-STATE-3).
package runtime

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"time"
)

// Label keys and values stamped on every object a run creates (R-RUN-4).
const (
	// LabelApp marks an object as created by this application.
	LabelApp = "s1s.app"
	// LabelRun carries the run identifier.
	LabelRun = "s1s.run"
	// LabelScenario carries the scenario identifier.
	LabelScenario = "s1s.scenario"

	// AppLabelValue is the value of LabelApp.
	AppLabelValue = "sev1situations"
)

// Errors that callers branch on. Implementations wrap them with context, so
// match with errors.Is.
var (
	// ErrNotFound means the named container or network does not exist.
	ErrNotFound = errors.New("not found")
	// ErrUnavailable means the container runtime itself cannot be reached, for
	// example Podman is not installed or its machine is stopped.
	ErrUnavailable = errors.New("container runtime unavailable")
)

// Run identifies the run an object belongs to.
type Run struct {
	// ID is the short run identifier (D6).
	ID string
	// Scenario is the scenario identifier.
	Scenario string
}

// State is the lifecycle state of a container.
type State string

// Container states.
const (
	// StateRunning means the container's main process is running.
	StateRunning State = "running"
	// StateStopped means the container exists but is not running.
	StateStopped State = "stopped"
)

// NetworkOptions describes a network to create.
type NetworkOptions struct {
	// Run owns the network.
	Run Run
	// Name is a short name. The runtime builds the full name from the run ID.
	Name string
}

// Network is a created network.
type Network struct {
	// ID identifies the network to later calls.
	ID string
	// Name is the full name, which includes the run ID.
	Name string
	// Run is the run that owns the network.
	Run Run
}

// PortMapping publishes a container port on the host. The host side always
// binds to 127.0.0.1 (R-APP-3).
type PortMapping struct {
	// Host is the host port, allocated by the application (R-APP-4).
	Host int
	// Container is the port inside the container.
	Container int
}

// ContainerOptions describes a container to create and start.
type ContainerOptions struct {
	// Run owns the container.
	Run Run
	// Name is a short name. The runtime builds the full name from the run ID.
	Name string
	// Image is the image reference. It must be local: call EnsureImage first.
	Image string
	// Command overrides the image's command.
	Command []string
	// Env is the environment of the main process.
	Env map[string]string
	// Network is the ID of a network from CreateNetwork.
	Network string
	// Hostname is the container's host name. It defaults to Name.
	Hostname string
	// Ports are published on the loopback interface of the host.
	Ports []PortMapping
	// CapAdd lists Linux capabilities to add, for example IPC_LOCK.
	CapAdd []string
}

// Container is a created container.
type Container struct {
	// ID identifies the container to later calls.
	ID string
	// Name is the full name, which includes the run ID.
	Name string
	// Image is the image reference it was created from.
	Image string
	// Run is the run that owns the container.
	Run Run
	// State is the current lifecycle state.
	State State
	// IP is the address on the run's network, which other containers use.
	IP string
	// Ports are the published ports.
	Ports []PortMapping
}

// ExecOptions adjusts a command run inside a container.
type ExecOptions struct {
	// Env adds to the container's environment for this command.
	Env map[string]string
	// User runs the command as this user.
	User string
	// WorkDir is the working directory.
	WorkDir string
	// Detach returns as soon as the command has started. The result then has
	// no output and an exit code of zero. This is how a product process is
	// started inside its container.
	Detach bool
}

// ExecResult is the outcome of a command that ran to completion. A non-zero
// exit code is not an error: the caller decides what it means.
type ExecResult struct {
	// ExitCode is the command's exit status.
	ExitCode int
	// Stdout is what the command wrote to standard output.
	Stdout string
	// Stderr is what the command wrote to standard error.
	Stderr string
}

// LogOptions selects which log lines to read.
type LogOptions struct {
	// Follow keeps the stream open for new lines until the context ends.
	Follow bool
	// Tail limits the output to the last N lines. Zero means all of them.
	Tail int
	// Since limits the output to lines after this time. The zero time means all.
	Since time.Time
}

// File is one file to place inside a container.
type File struct {
	// Path is the absolute destination path inside the container.
	Path string
	// Mode is the file mode, for example 0o755 for an executable.
	Mode fs.FileMode
	// Size is the exact number of bytes Content will yield.
	Size int64
	// Content is the file's bytes.
	Content io.Reader
}

// Terminal is an interactive session in a container (a TTY exec). Reads return
// the terminal's output and writes send input. Close ends the session.
type Terminal interface {
	io.ReadWriteCloser
	// Resize changes the terminal's size in character cells.
	Resize(ctx context.Context, cols, rows uint16) error
}

// Runtime performs container operations. Methods taking a container ID accept
// the ID returned by StartContainer. A missing container or network returns an
// error that matches ErrNotFound.
type Runtime interface {
	// Ping reports whether the runtime can be reached. It returns an error
	// matching ErrUnavailable when it cannot.
	Ping(ctx context.Context) error

	// EnsureImage makes an image available locally. It pulls only when the image
	// is missing, so a scenario deploys without a network once its images are
	// cached (R-RUN-9). progress, which may be nil, receives the runtime's
	// status lines while it pulls. There is deliberately no way to delete an
	// image (R-STATE-5).
	EnsureImage(ctx context.Context, ref string, progress func(line string)) error

	// CreateNetwork creates a labelled network.
	CreateNetwork(ctx context.Context, o NetworkOptions) (Network, error)
	// StartContainer creates a labelled container and starts it.
	StartContainer(ctx context.Context, o ContainerOptions) (Container, error)

	// Exec runs a command to completion and returns its output and exit code.
	Exec(ctx context.Context, id string, cmd []string, o ExecOptions) (ExecResult, error)
	// Attach opens an interactive terminal running cmd (R-TERM-2).
	Attach(ctx context.Context, id string, cmd []string) (Terminal, error)
	// Logs returns the container's log lines. The caller closes the reader.
	Logs(ctx context.Context, id string, o LogOptions) (io.ReadCloser, error)
	// Copy writes a file into the container. It works before and after start.
	Copy(ctx context.Context, id string, f File) error

	// Stop stops a running container. Stopping a stopped container is not an error.
	Stop(ctx context.Context, id string) error
	// Remove removes a container, stopping it first if needed.
	Remove(ctx context.Context, id string) error

	// List returns the containers that belong to a run.
	List(ctx context.Context, runID string) ([]Container, error)
	// Runs returns the IDs of every run that has objects present, for the
	// start-up cleanup of leftovers (R-STATE-3).
	Runs(ctx context.Context) ([]string, error)
	// RemoveRun removes every container and network of a run, in dependency
	// order. It is idempotent and keeps going past a failed removal, returning
	// the failures joined together (section 7).
	RemoveRun(ctx context.Context, runID string) error
}
