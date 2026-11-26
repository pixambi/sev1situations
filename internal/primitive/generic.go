package primitive

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// maxEvidence caps captured output so a chatty command cannot flood the report.
const maxEvidence = 8 << 10

// RegisterGeneric adds the generic primitives (section 4.5): container, file,
// http, wait and assert. Product packages add their own through Product.Primitives.
func RegisterGeneric(r *Registry) error {
	for _, p := range []Primitive{
		containerExec{}, containerStop{}, containerStart{}, containerRemove{}, containerKillProcess{}, containerCopyIn{},
		fileRender{}, fileWrite{},
		httpRequest{},
		waitUntil{}, waitSleep{},
		assertEquals{}, assertContains{}, assertMatches{}, assertJSONPath{},
	} {
		if err := r.Register(p); err != nil {
			return err
		}
	}
	return nil
}

// Lookup finds a node of the deployment by name. An unknown node means a primitive
// cannot run, which is an error and not a Fail.
func (e Env) Lookup(name string) (Node, error) {
	if n, ok := e.Nodes[name]; ok {
		return n, nil
	}
	known := make([]string, 0, len(e.Nodes))
	for k := range e.Nodes {
		known = append(known, k)
	}
	sort.Strings(known)
	return Node{}, fmt.Errorf("node %q is not part of this deployment (nodes: %s)", name, strings.Join(known, ", "))
}

func passed(format string, args ...any) Result {
	return Result{Status: Pass, Message: fmt.Sprintf(format, args...)}
}

func failed(evidence, format string, args ...any) Result {
	return Result{Status: Fail, Message: fmt.Sprintf(format, args...), Evidence: truncate(evidence)}
}

func truncate(s string) string {
	if len(s) <= maxEvidence {
		return s
	}
	return s[:maxEvidence] + "\n[output truncated]"
}

// parseMode reads a file mode written in octal, such as 0755.
func parseMode(s string) (fs.FileMode, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 8, 32)
	if err != nil || n > 0o7777 {
		return 0, fmt.Errorf("mode %q is not an octal file mode such as 0644 or 0755", s)
	}
	return fs.FileMode(n), nil
}

// joinErrors reports several failures as one error.
func joinErrors(errs []error) error { return errors.Join(errs...) }

var (
	argNode = Arg{Name: "node", Type: String, Required: true, Help: "The node to act on, for example node-1."}
	argSave = Arg{Name: "save", Type: String, Help: "If set, the value is kept under this name for later steps as .Outputs.<name>."}
)
