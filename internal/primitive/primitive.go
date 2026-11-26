// Package primitive defines the primitive interface and registry (R-SCN-2,
// section 4.5). A primitive is a named, tested building block that a scenario
// composes in YAML. Primitives are the only way scenario files act on the world.
//
// A primitive declares its arguments, so a typo fails `s1s scenario validate`
// and not a live run. It reports a Fail when it ran and the condition was not
// met, and returns an error when it could not run at all. The registry runs it
// under a timeout and the run's context, and redacts known credentials from
// everything it reports (R-QUAL-3).
package primitive

import (
	"context"
	"time"

	"github.com/pixambi/sev1situations/internal/runtime"
)

// Status is the outcome of a primitive that ran.
type Status string

const (
	// Pass means the primitive ran and its condition was met.
	Pass Status = "pass"
	// Fail means the primitive ran and its condition was not met, for example
	// the cluster is still broken. It is not an error.
	Fail Status = "fail"
)

// Result is what a primitive that ran reports.
type Result struct {
	// Status is Pass or Fail.
	Status Status
	// Message is a short explanation, shown beside the result.
	Message string
	// Outputs are named values later steps can use in templates.
	Outputs map[string]string
	// Evidence is captured output for the instructor, such as command output.
	Evidence string
}

// ArgType is the type of a primitive's argument.
type ArgType string

// Argument types.
const (
	// String is text.
	String ArgType = "string"
	// Int is a whole number.
	Int ArgType = "int"
	// Bool is true or false.
	Bool ArgType = "bool"
	// StringList is a list of text values.
	StringList ArgType = "list"
	// Duration is a time span written like 30s or 2m.
	Duration ArgType = "duration"
	// Map is a set of named values, such as HTTP headers.
	Map ArgType = "map"
)

// Arg declares one argument of a primitive.
type Arg struct {
	// Name is the key in the scenario's args.
	Name string
	// Type is the argument's type.
	Type ArgType
	// Required means the scenario must give it. A required argument has no default.
	Required bool
	// Default is used when the scenario leaves the argument out.
	Default any
	// Help explains the argument for the Contributing docs.
	Help string
}

// Spec describes a primitive: what it is called, what it does and what it takes.
type Spec struct {
	// Name is dotted and lower-case, grouped by owner, for example container.exec.
	Name string
	// Summary is one sentence for the Contributing docs.
	Summary string
	// Args are the declared arguments. "timeout" is added to every primitive.
	Args []Arg
	// Timeout is the default time limit. It must be positive: no primitive
	// blocks forever.
	Timeout time.Duration
	// Changes says the primitive changes the environment. The documentation
	// must also say whether running it again is safe.
	Changes bool
	// Nested names Map arguments that hold another call, written
	// {primitive: name, args: {...}}. The registry checks the nested call's
	// name and arguments when a scenario loads, as it does for a step's own.
	Nested []string
}

// Node is one container of the deployment, as a primitive sees it.
type Node struct {
	// Name is the node's name, for example node-1.
	Name string
	// Cluster is the cluster or service the node belongs to.
	Cluster string
	// ContainerID identifies the node's container to the runtime.
	ContainerID string
	// IP is the node's address on the run's network.
	IP string
}

// Env is what a primitive may use while it runs.
type Env struct {
	// Runtime performs container operations.
	Runtime runtime.Runtime
	// Run identifies the run.
	Run runtime.Run
	// Nodes are the deployment's nodes by name.
	Nodes map[string]Node
	// ReadFile reads a file from the scenario's folder, for example a template
	// or a fixture. The engine sets it. It may not leave the folder (R-SCN-15).
	ReadFile func(rel string) ([]byte, error)
	// Render fills in a template with the run's values. The engine sets it.
	Render func(src string) (string, error)
	// Call runs another primitive in the same environment. The registry sets
	// it, so wait.until can run the check it retries.
	Call func(ctx context.Context, name string, args map[string]any) (Result, error)
	// Redactor removes the run's known credentials from what a primitive
	// reports. The registry applies it, so a primitive need not. It may be nil.
	Redactor *Redactor
}

// Primitive is one named building block.
type Primitive interface {
	// Spec describes the primitive. It must return the same value every time.
	Spec() Spec
	// Run executes the primitive. args hold every declared argument, already
	// checked and typed, with defaults filled in. Return a Result with Status
	// Fail when the condition is not met, and an error only when the primitive
	// could not run. It must stop when ctx ends.
	Run(ctx context.Context, env Env, args Args) (Result, error)
}
