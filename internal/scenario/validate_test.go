package scenario

import (
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

type primitives map[string]bool

func (p primitives) Has(name string) bool { return p[name] }

var knownPrimitives = primitives{"container.remove": true, "vault.expect-no-leader": true, "vault.raft-peers": true}

var strict = Options{Primitives: knownPrimitives, Products: []string{"vault", "boundary"}}

// validFS holds a scenario folder with both step guides present.
func validFS(extra map[string]string) fstest.MapFS {
	files := map[string]string{
		"s/scenario.yaml":       worked,
		"s/steps/01-break.md":   "# Break\nRemove two nodes.\n",
		"s/steps/02-recover.md": "# Recover\nUse peers.json.\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	return folder(files)
}

func validate(t *testing.T, yaml string, extra map[string]string, opts Options) []Problem {
	t.Helper()
	files := map[string]string{"s/scenario.yaml": yaml}
	for k, v := range extra {
		files[k] = v
	}
	fsys := validFS(files)
	sc, err := Load(fsys, "s")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	err = sc.Validate(opts)
	if err == nil {
		return nil
	}
	return problemsOf(t, err)
}

// dropLine removes the line that starts with prefix. dropBlock also removes the
// indented lines that follow it.
func dropLine(y, prefix string) string {
	return regexp.MustCompile(`(?m)^`+regexp.QuoteMeta(prefix)+`.*\n`).ReplaceAllString(y, "")
}

func dropBlock(y, key string) string {
	return regexp.MustCompile(`(?m)^`+regexp.QuoteMeta(key)+`:.*\n(?:[ ]+.*\n)*`).ReplaceAllString(y, "")
}

// lineOfNth returns the line of the nth (1-based) line containing needle.
func lineOfNth(y, needle string, nth int) int {
	for i, l := range strings.Split(y, "\n") {
		if strings.Contains(l, needle) {
			if nth--; nth <= 0 {
				return i + 1
			}
		}
	}
	return 0
}

func TestTheWorkedExampleIsValid(t *testing.T) {
	if ps := validate(t, worked, nil, strict); ps != nil {
		t.Fatalf("problems = %+v, want none", ps)
	}
}

func TestValidateRules(t *testing.T) {
	tests := []struct {
		name  string
		yaml  string
		extra map[string]string
		field string
		msg   string
		at    string // text on the line the problem must point at; "" when there is no parent line either
		nth   int    // which line containing at, counting from 1; 0 means the first
	}{
		// Scenario Standard fields (5.4, R-SCN-13).
		{"missing id", dropLine(worked, "id: vault"), nil, "id", "is required", "", 0},
		{"id not kebab-case", strings.Replace(worked, "id: vault-raft-lost-quorum", "id: Vault_Raft", 1), nil, "id", "kebab-case", "id: Vault_Raft", 0},
		{"missing name", dropLine(worked, "name: Raft"), nil, "name", "is required", "", 0},
		{"missing summary", dropLine(worked, "summary:"), nil, "summary", "is required", "", 0},
		{"missing product", strings.Replace(worked, "product: vault\neditions", "editions", 1), nil, "product", "is required", "", 0},
		{"unknown product", strings.Replace(worked, "product: vault\neditions", "product: nomad\neditions", 1), nil, "product", "not a known product", "product: nomad", 0},
		{"no editions", strings.Replace(worked, "editions: [community, enterprise]", "editions: []", 1), nil, "editions", "at least one", "editions: []", 0},
		{"unknown edition", strings.Replace(worked, "[community, enterprise]", "[community, free]", 1), nil, "editions[1]", "not an edition", "editions: [community, free]", 0},
		{"duplicate edition", strings.Replace(worked, "[community, enterprise]", "[community, community]", 1), nil, "editions[1]", "listed twice", "editions: [community, community]", 0},
		{"bad difficulty", strings.Replace(worked, "difficulty: intermediate", "difficulty: hard", 1), nil, "difficulty", "not a difficulty", "difficulty: hard", 0},
		{"zero minutes", strings.Replace(worked, "estimatedMinutes: 20", "estimatedMinutes: 0", 1), nil, "estimatedMinutes", "between 1 and", "estimatedMinutes: 0", 0},
		{"too many minutes", strings.Replace(worked, "estimatedMinutes: 20", "estimatedMinutes: 9999", 1), nil, "estimatedMinutes", "between 1 and", "estimatedMinutes: 9999", 0},
		{"no tags", strings.Replace(worked, "tags: [vault, raft, quorum]", "tags: []", 1), nil, "tags", "at least one", "tags: []", 0},
		{"no objectives", dropBlock(worked, "objectives"), nil, "objectives", "at least one", "", 0},
		{"no prerequisites", dropBlock(worked, "prerequisites"), nil, "prerequisites", "at least one", "", 0},
		{"missing initial state", dropLine(worked, "initialState:"), nil, "initialState", "is required", "", 0},
		{"missing failure created", dropLine(worked, "failureCreated:"), nil, "failureCreated", "is required", "", 0},
		{"missing success criteria", dropLine(worked, "successCriteria:"), nil, "successCriteria", "is required", "", 0},
		{"no pitfalls", dropBlock(worked, "pitfalls"), nil, "pitfalls", "at least one", "", 0},
		{"no references", dropBlock(worked, "references"), nil, "references", "at least one", "", 0},
		{"reference without https", strings.Replace(worked, "url: https://", "url: http://", 1), nil, "references[0].url", "https://", "url: http://", 0},
		{"reference without title", strings.Replace(worked, "  - title: Raft recovery\n    url", "  - url", 1), nil, "references[0].title", "is required", "- url:", 0},

		// Parameters (R-SCN-11).
		{"no parameters", dropBlock(worked, "parameters"), nil, "parameters", "at least one", "", 0},
		{"parameter id with a dash", strings.Replace(worked, "id: nodes", "id: node-count", 1), nil, "parameters[1].id", ".Params.node-count", "id: node-count", 0},
		{"duplicate parameter id", strings.Replace(worked, "id: nodes", "id: version", 1), nil, "parameters[1].id", "used by another parameter", "id: version", 2},
		{"unknown parameter type", strings.Replace(worked, "type: int", "type: color", 1), nil, "parameters[1].type", "not a parameter type", "type: color", 0},
		{"version parameter without a product", strings.Replace(worked, "    type: version\n    product: vault\n", "    type: version\n", 1), nil, "parameters[0].product", "is required", "- id: version", 0},
		{"int parameter without bounds", dropLine(strings.Replace(worked, "    min: 3\n", "", 1), "    max:"), nil, "parameters[1]", "needs both min and max", "- id: nodes", 0},
		{"min above max", strings.Replace(worked, "min: 3", "min: 9", 1), nil, "parameters[1].min", "greater than max", "min: 9", 0},
		{"default outside the bounds", strings.Replace(worked, "default: 3", "default: 9", 1), nil, "parameters[1].default", "outside min", "default: 9", 0},
		{"default not a number", strings.Replace(worked, "default: 3", "default: three", 1), nil, "parameters[1].default", "whole number", "default: three", 0},

		// Topology.
		{"no clusters", strings.Replace(worked, "  clusters:\n    - name: primary\n      product: vault\n      nodes: 3\n      storage: raft\n", "  clusters: []\n", 1), nil, "topology.clusters", "at least one cluster", "clusters: []", 0},
		{"cluster name not kebab-case", strings.Replace(worked, "name: primary", "name: Primary", 1), nil, "topology.clusters[0].name", "kebab-case", "name: Primary", 0},
		{"cluster and service share a name", strings.Replace(worked, "name: postgres", "name: primary", 1), nil, "topology.services[0].name", "used by another", "name: primary", 2},
		{"cluster with no nodes", strings.Replace(worked, "nodes: 3\n      storage", "nodes: 0\n      storage", 1), nil, "topology.clusters[0].nodes", "at least 1", "nodes: 0", 0},
		{"cluster with an unknown product", strings.Replace(worked, "      product: vault\n      nodes", "      product: nomad\n      nodes", 1), nil, "topology.clusters[0].product", "not a known product", "product: nomad", 0},
		{"service without a type", strings.Replace(worked, "      type: postgres\n", "", 1), nil, "topology.services[0].type", "is required", "- name: postgres", 0},

		// Steps.
		{"duplicate step id", strings.Replace(worked, "id: recover", "id: break", 1), nil, "steps[1].id", "used by another step", "id: break", 2},
		{"step id not kebab-case", strings.Replace(worked, "id: recover", "id: Recover_It", 1), nil, "steps[1].id", "kebab-case", "id: Recover_It", 0},
		{"unknown step kind", strings.Replace(worked, "kind: verification", "kind: celebration", 1), nil, "steps[1].kind", "not a step kind", "kind: celebration", 0},
		{"step without a title", dropLine(worked, "    title: Remove two nodes"), nil, "steps[0].title", "is required", "- id: break", 0},
		{"step without a guide", dropLine(worked, "    guide: steps/01-break.md"), nil, "steps[0].guide", "is required", "- id: break", 0},
		{"guide file missing", strings.Replace(worked, "steps/01-break.md", "steps/nope.md", 1), nil, "steps[0].guide", "does not exist", "guide: steps/nope.md", 0},
		{"guide outside the folder", strings.Replace(worked, "steps/01-break.md", "../other/secret.md", 1), nil, "steps[0].guide", "inside the scenario folder", "guide: ../other", 0},
		{"guide empty", worked, map[string]string{"s/steps/01-break.md": "  \n"}, "steps[0].guide", "is empty", "guide: steps/01-break.md", 0},
		{"breakage without an action", strings.Replace(worked, "    action:\n      - primitive: container.remove\n        args: { nodes: [node-2, node-3] }\n", "", 1), nil, "steps[0].action", "needs an action", "- id: break", 0},
		{"breakage without a verify", strings.Replace(worked, "    verify:\n      - primitive: vault.expect-no-leader\n        args: { node: node-1 }\n", "", 1), nil, "steps[0].verify", "confirms the environment is broken", "- id: break", 0},
		{"verification without a verify", strings.Replace(worked, "    verify:\n      - primitive: vault.raft-peers\n        args: { node: node-1, voters: 1 }\n", "", 1), nil, "steps[1].verify", "checks the fix", "- id: recover", 0},
		{"verification with an action", strings.Replace(worked, "    resetPoint: true\n", "    resetPoint: true\n    action:\n      - primitive: container.remove\n", 1), nil, "steps[1].action", "does not apply anything", "    action:", 2},
		{"reset without a reset point", strings.Replace(worked, "    verify:\n      - primitive: vault.expect-no-leader", "    reset:\n      - primitive: container.remove\n    verify:\n      - primitive: vault.expect-no-leader", 1), nil, "steps[0].reset", "resetPoint", "    reset:", 0},
		{"unknown primitive", strings.Replace(worked, "container.remove", "container.explode", 1), nil, "steps[0].action[0].primitive", "not a known primitive", "primitive: container.explode", 0},
		{"call without a primitive", strings.Replace(worked, "      - primitive: container.remove\n        args", "      - args", 1), nil, "steps[0].action[0].primitive", "is required", "- args:", 0},

		// Templates (4.5).
		{"template with a dash in a name", strings.Replace(worked, "args: { nodes: [node-2, node-3] }", `args: { host: "{{ .Nodes.node-1.IP }}" }`, 1), nil, "steps[0].action[0].args.host", "written with an underscore", "args: { host:", 0},
		{"template that never closes", strings.Replace(worked, "args: { nodes: [node-2, node-3] }", `args: { host: "{{ .Params.version" }`, 1), nil, "steps[0].action[0].args.host", "template error", "args: { host:", 0},
		{"template inside a list", strings.Replace(worked, "args: { nodes: [node-2, node-3] }", `args: { nodes: ["{{ .Nodes.node_1.Name", "x"] }`, 1), nil, "steps[0].action[0].args.nodes[0]", "template error", "args: { nodes:", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := validate(t, tt.yaml, tt.extra, strict)
			var got *Problem
			for i := range ps {
				if ps[i].Field == tt.field {
					got = &ps[i]
					break
				}
			}
			if got == nil {
				t.Fatalf("no problem for field %q in %+v", tt.field, ps)
			}
			if !strings.Contains(got.Message, tt.msg) {
				t.Errorf("message = %q, want it to contain %q", got.Message, tt.msg)
			}
			wantLine := 0
			if tt.at != "" {
				nth := tt.nth
				if nth == 0 {
					nth = 1
				}
				wantLine = lineOfNth(tt.yaml, tt.at, nth)
				if wantLine == 0 {
					t.Fatalf("test bug: %q is not in the YAML", tt.at)
				}
			}
			if got.Line != wantLine {
				t.Errorf("line = %d, want %d", got.Line, wantLine)
			}
			if got.File != "s/scenario.yaml" {
				t.Errorf("file = %q", got.File)
			}
		})
	}
}

func TestValidTemplatesPass(t *testing.T) {
	y := strings.Replace(worked, "args: { nodes: [node-2, node-3] }",
		`args: { host: "{{ .Nodes.node_1.IP }}", token: "{{ .Creds.primary.rootToken }}", v: "{{ .Params.version }}", plain: "no template" }`, 1)
	if ps := validate(t, y, nil, strict); ps != nil {
		t.Errorf("problems = %+v, want none", ps)
	}
}

func TestASandboxNeedsNoFailureDescription(t *testing.T) {
	sandbox := `id: vault-sandbox
name: Vault Sandbox
summary: A free-form Vault cluster.
product: vault
editions: [community]
difficulty: beginner
estimatedMinutes: 60
tags: [vault]
objectives: [Explore Vault]
prerequisites: [None]
parameters:
  - id: version
    type: version
    product: vault
    label: Vault version
topology:
  clusters:
    - name: primary
      product: vault
      nodes: 1
initialState: A running single node Vault.
references:
  - title: Vault docs
    url: https://developer.hashicorp.com/vault
`
	if ps := validate(t, sandbox, nil, strict); ps != nil {
		t.Errorf("a sandbox with no failure, criteria or pitfalls reported %+v (R-SCN-17)", ps)
	}
	// The same file with a step must describe the failure.
	withStep := sandbox + "steps:\n  - id: break\n    kind: breakage\n    title: Break it\n    guide: steps/01-break.md\n    action: [{ primitive: container.remove }]\n    verify: [{ primitive: vault.raft-peers }]\n"
	ps := validate(t, withStep, nil, strict)
	fields := map[string]bool{}
	for _, p := range ps {
		fields[p.Field] = true
	}
	for _, f := range []string{"failureCreated", "successCriteria", "pitfalls"} {
		if !fields[f] {
			t.Errorf("a scenario with steps did not report %s", f)
		}
	}
}

func TestEveryProblemIsReportedInLineOrder(t *testing.T) {
	y := strings.Replace(worked, "difficulty: intermediate", "difficulty: hard", 1)
	y = strings.Replace(y, "kind: verification", "kind: celebration", 1)
	y = strings.Replace(y, "estimatedMinutes: 20", "estimatedMinutes: 0", 1)

	ps := validate(t, y, nil, strict)
	if len(ps) != 3 {
		t.Fatalf("problems = %+v, want 3", ps)
	}
	for i := 1; i < len(ps); i++ {
		if ps[i].Line < ps[i-1].Line {
			t.Errorf("problems are not in line order: %+v", ps)
		}
	}
}

func TestOptionsAreOptional(t *testing.T) {
	y := strings.Replace(worked, "container.remove", "container.explode", 1)
	y = strings.Replace(y, "product: vault\neditions", "product: nomad\neditions", 1)
	if ps := validate(t, y, nil, Options{}); ps != nil {
		t.Errorf("with no registry to ask, problems = %+v, want the primitive and product checks skipped", ps)
	}
}

func TestProblemText(t *testing.T) {
	ps := validate(t, strings.Replace(worked, "difficulty: intermediate", "difficulty: hard", 1), nil, strict)
	want := "s/scenario.yaml:6: difficulty:"
	if len(ps) != 1 || !strings.HasPrefix(ps[0].Error(), want) {
		t.Errorf("problem text = %q, want it to start with %q", ps[0].Error(), want)
	}
}

// argChecking is a primitive set that also checks arguments.
type argChecking struct {
	primitives
	problems map[string][]string
}

func (a argChecking) CheckArgs(name string, _ map[string]any) []string { return a.problems[name] }

func TestValidateAsksThePrimitivesToCheckArguments(t *testing.T) {
	set := argChecking{
		primitives: knownPrimitives,
		problems:   map[string][]string{"container.remove": {`unknown argument "nodez". container.remove takes: nodes, timeout`}},
	}
	ps := validate(t, worked, nil, Options{Primitives: set})
	if len(ps) != 1 || ps[0].Field != "steps[0].action[0].args" || !strings.Contains(ps[0].Message, `unknown argument "nodez"`) {
		t.Fatalf("problems = %+v, want the typo reported on the args of steps[0].action[0]", ps)
	}
	if want := lineOfNth(worked, "args: { nodes", 1); ps[0].Line != want && ps[0].Line != want-1 {
		t.Errorf("line = %d, want the line of the call (%d) or its args", ps[0].Line, want)
	}

	// A set that cannot check arguments still validates names.
	if ps := validate(t, worked, nil, Options{Primitives: knownPrimitives}); ps != nil {
		t.Errorf("problems = %+v, want none", ps)
	}
}
