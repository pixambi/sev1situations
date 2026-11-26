// Package scenario defines the scenario model and loads scenario folders from
// YAML (R-SCN-1, section 4). It is pure data: it knows nothing about
// containers, products or the UI.
//
// A scenario is a self-contained folder (R-SCN-15) with a scenario.yaml and the
// Markdown, templates and fixtures it refers to. Loading is strict: an unknown
// field or a value of the wrong type is an error that names the file and line
// (R-SCN-3).
package scenario

import (
	"io/fs"
	"path"
)

// FileName is the name of the scenario file inside a scenario folder.
const FileName = "scenario.yaml"

// Difficulty levels from the Scenario Standard (5.4).
const (
	// Beginner is for newcomers to the product.
	Beginner = "beginner"
	// Intermediate expects working knowledge of the product.
	Intermediate = "intermediate"
	// Advanced expects deep knowledge of the product.
	Advanced = "advanced"
)

// Step kinds (4.2). The Start step is implicit and is not listed (R-SCN-11).
const (
	// KindBreakage applies a breaking change, then verifies the broken state.
	KindBreakage = "breakage"
	// KindVerification checks that the trainee's fix worked.
	KindVerification = "verification"
)

// Parameter types (4.4), added as scenarios need them.
const (
	// ParamVersion is a drop-down fed by the product's available versions.
	ParamVersion = "version"
	// ParamEdition is a drop-down of the scenario's editions.
	ParamEdition = "edition"
	// ParamInt is a bounded whole number.
	ParamInt = "int"
)

// Scenario is one loaded scenario. It holds every field of the Scenario
// Standard (5.4, R-SCN-13).
type Scenario struct {
	// ID is the stable, unique, kebab-case identifier.
	ID string `yaml:"id"`
	// Name is shown in the catalogue.
	Name string `yaml:"name"`
	// Summary is one sentence shown in the catalogue.
	Summary string `yaml:"summary"`
	// Product is the scenario's main product, for example vault.
	Product string `yaml:"product"`
	// Editions lists the editions it supports: community, enterprise or both.
	Editions []string `yaml:"editions"`
	// Difficulty is beginner, intermediate or advanced.
	Difficulty string `yaml:"difficulty"`
	// EstimatedMinutes is how long a session takes.
	EstimatedMinutes int `yaml:"estimatedMinutes"`
	// Tags group scenarios in the catalogue.
	Tags []string `yaml:"tags"`
	// Objectives is what the trainee should gain.
	Objectives []string `yaml:"objectives"`
	// Prerequisites is the knowledge and tools needed.
	Prerequisites []string `yaml:"prerequisites"`
	// Parameters is what the Start step lets the instructor choose.
	Parameters []Parameter `yaml:"parameters"`
	// Topology describes the deployment.
	Topology Topology `yaml:"topology"`
	// InitialState says what the deployment provides before step one.
	InitialState string `yaml:"initialState"`
	// FailureCreated says what is broken, the symptoms and whether it is recoverable.
	FailureCreated string `yaml:"failureCreated"`
	// SuccessCriteria says what "fixed" means.
	SuccessCriteria string `yaml:"successCriteria"`
	// Pitfalls lists common mistakes and their symptoms.
	Pitfalls []string `yaml:"pitfalls"`
	// References links to product documentation.
	References []Reference `yaml:"references"`
	// Steps are the ordered steps after the implicit Start step. A scenario
	// with none is an open-ended sandbox (R-SCN-17).
	Steps []Step `yaml:"steps"`

	// Dir is the scenario folder inside the file system it was loaded from.
	Dir string `yaml:"-"`

	fsys  fs.FS
	lines map[string]int
}

// Parameter is one input of the Start step (R-SCN-11).
type Parameter struct {
	// ID identifies the parameter, and is used as .Params.<id> in templates.
	ID string `yaml:"id"`
	// Type is version, edition or int.
	Type string `yaml:"type"`
	// Label is the form label.
	Label string `yaml:"label"`
	// Help is the explanation shown beside the field.
	Help string `yaml:"help"`
	// Default is the initial value.
	Default any `yaml:"default"`
	// Product is the product whose versions feed a version parameter.
	Product string `yaml:"product"`
	// Min is the smallest value of an int parameter.
	Min *int `yaml:"min"`
	// Max is the largest value of an int parameter.
	Max *int `yaml:"max"`
}

// Topology describes what the deployment builds.
type Topology struct {
	// Clusters are the product clusters. The first is named node-1, node-2 and
	// so on; later ones are named <cluster>-node-N (section 3.6.1).
	Clusters []Cluster `yaml:"clusters"`
	// Services are supporting services such as a database.
	Services []Service `yaml:"services"`
}

// Cluster is a group of nodes of one product.
type Cluster struct {
	// Name is the cluster's name, used as the key for its credentials.
	Name string `yaml:"name"`
	// Product is the product the nodes run.
	Product string `yaml:"product"`
	// Nodes is the number of nodes.
	Nodes int `yaml:"nodes"`
	// NodesFrom, if set, is the ID of an int parameter whose value is the number
	// of nodes, in place of Nodes. A sandbox lets the instructor choose its size.
	NodesFrom string `yaml:"nodesFrom"`
	// Storage is the storage backend, for example raft.
	Storage string `yaml:"storage"`
}

// Service is a supporting service next to the clusters.
type Service struct {
	// Name is the service's name, used as the key for its credentials.
	Name string `yaml:"name"`
	// Type is the kind of service, for example postgres.
	Type string `yaml:"type"`
}

// Reference is a link to documentation.
type Reference struct {
	// Title is the link text.
	Title string `yaml:"title"`
	// URL is the address.
	URL string `yaml:"url"`
}

// Step is one step of a scenario.
type Step struct {
	// ID identifies the step and is unique within the scenario.
	ID string `yaml:"id"`
	// Kind is breakage or verification.
	Kind string `yaml:"kind"`
	// Title is the step's heading.
	Title string `yaml:"title"`
	// Guide is the path of the Markdown guidance, relative to the scenario folder.
	Guide string `yaml:"guide"`
	// ResetPoint marks a state the instructor can roll back to (R-SCN-6).
	ResetPoint bool `yaml:"resetPoint"`
	// Action runs when the instructor applies a breakage.
	Action []Call `yaml:"action"`
	// Verify checks the environment after an action or a fix.
	Verify []Call `yaml:"verify"`
	// Reset replaces the default redeploy-and-replay when rolling back to this step (4.7).
	Reset []Call `yaml:"reset"`
}

// Call is one use of a primitive, with its arguments (R-SCN-2).
type Call struct {
	// Primitive is the dotted name, for example container.exec.
	Primitive string `yaml:"primitive"`
	// Args are the primitive's arguments. String values may use templates.
	Args map[string]any `yaml:"args"`
}

// ReadFile reads a file inside the scenario folder, for example a step guide or
// a template. The path is relative to the folder and may not leave it
// (R-SCN-15).
func (s *Scenario) ReadFile(rel string) ([]byte, error) {
	clean := path.Clean(rel)
	if clean == "." || !fs.ValidPath(clean) {
		return nil, &fs.PathError{Op: "read", Path: rel, Err: fs.ErrInvalid}
	}
	return fs.ReadFile(s.fsys, path.Join(s.Dir, clean))
}

// Line returns the line in scenario.yaml where the named field starts, or 0 if
// it is not known. The path uses dots and indexes, for example "steps[1].kind".
func (s *Scenario) Line(fieldPath string) int {
	return s.lines[fieldPath]
}

// NodeCount returns how many nodes a cluster has for the chosen parameters: the
// value of the parameter named by NodesFrom if there is one, else Nodes.
func (c Cluster) NodeCount(params map[string]any) int {
	if c.NodesFrom != "" {
		switch n := params[c.NodesFrom].(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return c.Nodes
}
