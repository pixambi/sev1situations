package scenario

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"text/template/parse"
)

// Templates are Go text/template strings in a step's arguments (4.5). They read
// four things:
//
//	.Params.<id>            a parameter the instructor chose
//	.Nodes.<node>.<field>   a node's Name, IP or Cluster
//	.Creds.<name>.<key>     a credential, keyed by cluster or service name
//	.Outputs.<name>         a value an earlier primitive produced
//
// Go templates cannot parse a dash, so inside a template a dash in a name is
// written as an underscore: node-1 is node_1 and dr-secondary is dr_secondary.

// NodeData is what a template can read about a node.
type NodeData struct {
	// Name is the node's name, for example node-1.
	Name string
	// IP is the node's address on the run's network.
	IP string
	// Cluster is the name of the cluster or service the node belongs to.
	Cluster string
}

// TemplateData is everything a template can read when a step runs.
type TemplateData struct {
	// Params holds the parameter values the instructor chose, by parameter ID.
	Params map[string]any
	// Nodes holds the deployment's nodes, by template key (see TemplateKey).
	Nodes map[string]NodeData
	// Creds holds credentials, by template key of the cluster or service name.
	Creds map[string]map[string]string
	// Outputs holds values produced by earlier primitives.
	Outputs map[string]string
}

// nodeFields are the fields of NodeData a template may read.
var nodeFields = []string{"Name", "IP", "Cluster"}

// TemplateKey converts a node, cluster or service name to the key a template
// uses: dashes become underscores.
func TemplateKey(name string) string { return strings.ReplaceAll(name, "-", "_") }

// Names are the values a scenario's templates may refer to. Outputs are not
// listed: they are produced while a step runs, so they are checked then.
type Names struct {
	// Params are the parameter IDs.
	Params []string
	// Nodes are the template keys of the nodes.
	Nodes []string
	// Creds are the template keys of the clusters and services.
	Creds []string
}

// NodeNames returns the node names the topology produces (section 3.6.1). The
// first cluster names its nodes node-1, node-2 and so on. Nodes of later
// clusters are <cluster>-node-N and a service has <service>-node-1, so names are
// unique across the scenario.
func (s *Scenario) NodeNames() []string {
	var out []string
	for i, c := range s.Topology.Clusters {
		for n := 1; n <= s.mostNodes(c); n++ {
			if i == 0 {
				out = append(out, fmt.Sprintf("node-%d", n))
			} else {
				out = append(out, fmt.Sprintf("%s-node-%d", c.Name, n))
			}
		}
	}
	for _, sv := range s.Topology.Services {
		out = append(out, sv.Name+"-node-1")
	}
	return out
}

// mostNodes is the largest number of nodes a cluster can have, so a template may
// name any node the instructor's choice could create.
func (s *Scenario) mostNodes(c Cluster) int {
	if c.NodesFrom != "" {
		for _, p := range s.Parameters {
			if p.ID == c.NodesFrom && p.Max != nil {
				return *p.Max
			}
		}
	}
	return c.Nodes
}

// TemplateNames returns the names this scenario's templates may use.
func (s *Scenario) TemplateNames() Names {
	var n Names
	for _, p := range s.Parameters {
		n.Params = append(n.Params, p.ID)
	}
	for _, name := range s.NodeNames() {
		n.Nodes = append(n.Nodes, TemplateKey(name))
	}
	for _, c := range s.Topology.Clusters {
		n.Creds = append(n.Creds, TemplateKey(c.Name))
	}
	for _, sv := range s.Topology.Services {
		n.Creds = append(n.Creds, TemplateKey(sv.Name))
	}
	return n
}

// CheckTemplate parses a template and checks that every value it reads exists.
// It returns one message per problem: a syntax error, or a reference to a
// parameter, node, field or credential set that the scenario does not have.
func CheckTemplate(src string, names Names) []string {
	t, err := template.New("arg").Parse(src)
	if err != nil {
		return []string{"has a template error: " + strings.TrimPrefix(err.Error(), "template: arg:1: ") + dashHint(src)}
	}
	if t.Tree == nil {
		return nil
	}

	var problems []string
	for _, ref := range fieldRefs(t.Root, nil) {
		if msg := checkRef(ref, names); msg != "" {
			problems = append(problems, msg)
		}
	}
	return problems
}

func checkRef(ref []string, names Names) string {
	path := "." + strings.Join(ref, ".")
	switch ref[0] {
	case "Params":
		if len(ref) < 2 {
			return "uses .Params on its own. Write .Params.<name>"
		}
		if !contains(names.Params, ref[1]) {
			return fmt.Sprintf("%s: %q is not a parameter of this scenario%s", path, ref[1], available(names.Params))
		}
	case "Nodes":
		if len(ref) < 3 {
			return "uses a node without a field. Write .Nodes.<node>." + strings.Join(nodeFields, " or .")
		}
		if !contains(names.Nodes, ref[1]) {
			return fmt.Sprintf("%s: %q is not a node of this scenario%s", path, ref[1], available(names.Nodes))
		}
		if !contains(nodeFields, ref[2]) {
			return fmt.Sprintf("%s: a node has no field %q%s", path, ref[2], available(nodeFields))
		}
	case "Creds":
		if len(ref) < 3 {
			return "uses credentials without a key. Write .Creds.<cluster or service>.<key>"
		}
		if !contains(names.Creds, ref[1]) {
			return fmt.Sprintf("%s: %q is not a cluster or service of this scenario%s", path, ref[1], available(names.Creds))
		}
	case "Outputs":
		if len(ref) < 2 {
			return "uses .Outputs on its own. Write .Outputs.<name>"
		}
	default:
		return fmt.Sprintf("%s: templates can read .Params, .Nodes, .Creds and .Outputs, not .%s", path, ref[0])
	}
	return ""
}

func available(have []string) string {
	if len(have) == 0 {
		return " (there are none)"
	}
	sorted := append([]string(nil), have...)
	sort.Strings(sorted)
	return " (available: " + strings.Join(sorted, ", ") + ")"
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// fieldRefs collects every .A.B.C reference in a parse tree. References through
// a variable, such as $x.Name, are not ours to check.
func fieldRefs(n parse.Node, acc [][]string) [][]string {
	switch n := n.(type) {
	case *parse.ListNode:
		if n == nil {
			return acc
		}
		for _, c := range n.Nodes {
			acc = fieldRefs(c, acc)
		}
	case *parse.ActionNode:
		acc = fieldRefs(n.Pipe, acc)
	case *parse.PipeNode:
		if n == nil {
			return acc
		}
		for _, c := range n.Cmds {
			acc = fieldRefs(c, acc)
		}
	case *parse.CommandNode:
		for _, a := range n.Args {
			acc = fieldRefs(a, acc)
		}
	case *parse.FieldNode:
		acc = append(acc, n.Ident)
	case *parse.ChainNode:
		acc = fieldRefs(n.Node, acc)
	case *parse.IfNode:
		acc = branchRefs(&n.BranchNode, acc)
	case *parse.RangeNode:
		acc = branchRefs(&n.BranchNode, acc)
	case *parse.WithNode:
		acc = branchRefs(&n.BranchNode, acc)
	}
	return acc
}

func branchRefs(b *parse.BranchNode, acc [][]string) [][]string {
	acc = fieldRefs(b.Pipe, acc)
	acc = fieldRefs(b.List, acc)
	return fieldRefs(b.ElseList, acc)
}

// Render fills in a template. A value that is missing is an error, never an
// empty string, so a typo cannot silently produce a wrong command.
func Render(src string, data TemplateData) (string, error) {
	t, err := template.New("arg").Option("missingkey=error").Parse(src)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, templateRoot(data)); err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	return buf.String(), nil
}

// templateRoot gives every top-level name a non-nil map, so a missing key is
// reported as a missing key and not as a nil dereference.
func templateRoot(d TemplateData) map[string]any {
	root := map[string]any{
		"Params": d.Params, "Nodes": d.Nodes, "Creds": d.Creds, "Outputs": d.Outputs,
	}
	if d.Params == nil {
		root["Params"] = map[string]any{}
	}
	if d.Nodes == nil {
		root["Nodes"] = map[string]NodeData{}
	}
	if d.Creds == nil {
		root["Creds"] = map[string]map[string]string{}
	}
	if d.Outputs == nil {
		root["Outputs"] = map[string]string{}
	}
	return root
}

// RenderArgs returns a copy of a step's arguments with every string that
// contains a template rendered, descending into lists and maps.
func RenderArgs(args map[string]any, data TemplateData) (map[string]any, error) {
	out := make(map[string]any, len(args))
	for k, v := range args {
		r, err := renderValue(v, data)
		if err != nil {
			return nil, fmt.Errorf("argument %q: %w", k, err)
		}
		out[k] = r
	}
	return out, nil
}

func renderValue(v any, data TemplateData) (any, error) {
	switch x := v.(type) {
	case string:
		if strings.Contains(x, "{{") {
			return Render(x, data)
		}
		return x, nil
	case map[string]any:
		return RenderArgs(x, data)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			r, err := renderValue(item, data)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			out[i] = r
		}
		return out, nil
	}
	return v, nil
}
