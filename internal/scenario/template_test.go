package scenario

import (
	"strings"
	"testing"
)

func TestNodeNamesFollowSection361(t *testing.T) {
	sc := &Scenario{Topology: Topology{
		Clusters: []Cluster{{Name: "primary", Nodes: 3}, {Name: "dr-secondary", Nodes: 2}},
		Services: []Service{{Name: "postgres"}},
	}}
	want := "node-1 node-2 node-3 dr-secondary-node-1 dr-secondary-node-2 postgres-node-1"
	if got := strings.Join(sc.NodeNames(), " "); got != want {
		t.Errorf("NodeNames = %q, want %q", got, want)
	}

	names := sc.TemplateNames()
	if got := strings.Join(names.Nodes, " "); !strings.Contains(got, "dr_secondary_node_1") || strings.Contains(got, "-") {
		t.Errorf("template keys = %q, want dashes written as underscores", got)
	}
	if got := strings.Join(names.Creds, " "); got != "primary dr_secondary postgres" {
		t.Errorf("credential keys = %q", got)
	}
}

func TestTemplateKey(t *testing.T) {
	if got := TemplateKey("dr-secondary-node-1"); got != "dr_secondary_node_1" {
		t.Errorf("TemplateKey = %q", got)
	}
}

var tplNames = Names{
	Params: []string{"version", "nodes"},
	Nodes:  []string{"node_1", "node_2", "dr_secondary_node_1"},
	Creds:  []string{"primary", "dr_secondary"},
}

func TestCheckTemplate(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string // "" means no problems
	}{
		{"a parameter", `{{ .Params.version }}`, ""},
		{"a node field", `http://{{ .Nodes.node_1.IP }}:8200`, ""},
		{"a node of a later cluster", `{{ .Nodes.dr_secondary_node_1.Name }}`, ""},
		{"a credential", `{{ .Creds.dr_secondary.rootToken }}`, ""},
		{"an output, which is only known at run time", `{{ .Outputs.anything }}`, ""},
		{"inside a condition", `{{ if .Params.nodes }}{{ .Nodes.node_2.IP }}{{ else }}{{ .Nodes.node_1.IP }}{{ end }}`, ""},
		{"inside a pipeline with a builtin", `{{ printf "%s:8200" .Nodes.node_1.IP }}`, ""},
		{"no template at all", `plain text`, ""},

		{"a misspelled parameter", `{{ .Params.versoin }}`, `"versoin" is not a parameter of this scenario (available: nodes, version)`},
		{"a parameter on its own", `{{ .Params }}`, "Write .Params.<name>"},
		{"an unknown node", `{{ .Nodes.node_9.IP }}`, `"node_9" is not a node of this scenario`},
		{"an unknown node field", `{{ .Nodes.node_1.Address }}`, `a node has no field "Address" (available: Cluster, IP, Name)`},
		{"a node without a field", `{{ .Nodes.node_1 }}`, "without a field"},
		{"an unknown cluster", `{{ .Creds.secondary.rootToken }}`, `"secondary" is not a cluster or service of this scenario`},
		{"credentials without a key", `{{ .Creds.primary }}`, "without a key"},
		{"outputs on their own", `{{ .Outputs }}`, "Write .Outputs.<name>"},
		{"a root that does not exist", `{{ .Env.HOME }}`, "can read .Params, .Nodes, .Creds and .Outputs, not .Env"},
		{"the dash form from the old definition", `{{ .Creds.dr-secondary.rootToken }}`, "written with an underscore"},
		{"a dash in a node name", `{{ .Nodes.node-1.IP }}`, "written with an underscore"},
		{"an unclosed action", `{{ .Params.version`, "template error"},
		{"an unknown function", `{{ shell "rm -rf /" }}`, "template error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Join(CheckTemplate(tt.src, tplNames), "; ")
			if tt.want == "" {
				if got != "" {
					t.Errorf("problems = %q, want none", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("problems = %q, want one containing %q", got, tt.want)
			}
		})
	}
}

func TestCheckTemplateReportsEveryBadReference(t *testing.T) {
	got := CheckTemplate(`{{ .Params.a }} {{ .Nodes.node_9.IP }} {{ .Creds.nope.x }}`, tplNames)
	if len(got) != 3 {
		t.Errorf("problems = %q, want one for each of the three bad references", got)
	}
}

func TestValidateChecksTemplateValuesInSteps(t *testing.T) {
	y := strings.Replace(worked, "args: { nodes: [node-2, node-3] }",
		`args: { host: "{{ .Nodes.node_9.IP }}", v: ["{{ .Params.versoin }}"] }`, 1)
	ps := validate(t, y, nil, strict)

	byField := map[string]string{}
	for _, p := range ps {
		byField[p.Field] = p.Message
	}
	if m := byField["steps[0].action[0].args.host"]; !strings.Contains(m, `"node_9" is not a node`) {
		t.Errorf("host problem = %q (all: %+v)", m, ps)
	}
	if m := byField["steps[0].action[0].args.v[0]"]; !strings.Contains(m, `"versoin" is not a parameter`) {
		t.Errorf("v[0] problem = %q (all: %+v)", m, ps)
	}
}

var renderData = TemplateData{
	Params:  map[string]any{"version": "1.21.0", "nodes": 3},
	Nodes:   map[string]NodeData{"node_1": {Name: "node-1", IP: "10.89.1.2", Cluster: "primary"}},
	Creds:   map[string]map[string]string{"primary": {"rootToken": "hvs.secret"}},
	Outputs: map[string]string{"unsealKey": "abc123"},
}

func TestRender(t *testing.T) {
	tests := []struct{ src, want string }{
		{`vault_{{ .Params.version }}`, "vault_1.21.0"},
		{`{{ .Params.nodes }} nodes`, "3 nodes"},
		{`http://{{ .Nodes.node_1.IP }}:8200`, "http://10.89.1.2:8200"},
		{`{{ .Nodes.node_1.Name }} in {{ .Nodes.node_1.Cluster }}`, "node-1 in primary"},
		{`{{ .Creds.primary.rootToken }}`, "hvs.secret"},
		{`{{ .Outputs.unsealKey }}`, "abc123"},
		{`{{ printf "%s:%d" .Nodes.node_1.IP 8200 }}`, "10.89.1.2:8200"},
	}
	for _, tt := range tests {
		got, err := Render(tt.src, renderData)
		if err != nil || got != tt.want {
			t.Errorf("Render(%q) = %q, %v; want %q", tt.src, got, err, tt.want)
		}
	}
}

func TestRenderFailsOnAMissingValue(t *testing.T) {
	for _, src := range []string{
		`{{ .Params.missing }}`,
		`{{ .Nodes.node_9.IP }}`,
		`{{ .Creds.nope.rootToken }}`,
		`{{ .Creds.primary.nope }}`,
		`{{ .Outputs.notYet }}`,
		`{{ .Nodes.node_1.Nope }}`,
	} {
		got, err := Render(src, renderData)
		if err == nil {
			t.Errorf("Render(%q) = %q, want an error and not an empty value", src, got)
		}
	}
	if _, err := Render(`{{ .Params.x`, renderData); err == nil {
		t.Error("a template that does not parse was rendered")
	}
}

func TestRenderWithNoData(t *testing.T) {
	if got, err := Render("plain", TemplateData{}); err != nil || got != "plain" {
		t.Errorf("Render = %q, %v", got, err)
	}
	if _, err := Render(`{{ .Params.x }}`, TemplateData{}); err == nil {
		t.Error("a missing value in an empty data set rendered without an error")
	}
}

func TestRenderArgsDescendsIntoListsAndMaps(t *testing.T) {
	args := map[string]any{
		"host":    "{{ .Nodes.node_1.IP }}",
		"count":   3,
		"literal": "no template",
		"list":    []any{"{{ .Params.version }}", "x", 7},
		"headers": map[string]any{"X-Token": "{{ .Creds.primary.rootToken }}"},
	}
	got, err := RenderArgs(args, renderData)
	if err != nil {
		t.Fatal(err)
	}
	if got["host"] != "10.89.1.2" || got["count"] != 3 || got["literal"] != "no template" {
		t.Errorf("scalars = %+v", got)
	}
	if l := got["list"].([]any); l[0] != "1.21.0" || l[1] != "x" || l[2] != 7 {
		t.Errorf("list = %v", l)
	}
	if h := got["headers"].(map[string]any); h["X-Token"] != "hvs.secret" {
		t.Errorf("headers = %v", h)
	}
	if args["host"] != "{{ .Nodes.node_1.IP }}" {
		t.Error("RenderArgs changed its input")
	}

	_, err = RenderArgs(map[string]any{"list": []any{"ok", "{{ .Params.nope }}"}}, renderData)
	if err == nil || !strings.Contains(err.Error(), `"list"`) || !strings.Contains(err.Error(), "item 1") {
		t.Errorf("error = %v, want it to say which argument and item failed", err)
	}
}

func TestAClusterSizeCanComeFromAParameter(t *testing.T) {
	three, nine := 3, 9
	sc := &Scenario{
		Parameters: []Parameter{{ID: "nodes", Type: ParamInt, Max: &nine, Min: &three}},
		Topology:   Topology{Clusters: []Cluster{{Name: "primary", NodesFrom: "nodes", Nodes: 1}}},
	}
	c := sc.Topology.Clusters[0]

	for _, tt := range []struct {
		params map[string]any
		want   int
	}{
		{map[string]any{"nodes": 5}, 5},
		{map[string]any{"nodes": int64(7)}, 7},
		{map[string]any{"nodes": 4.0}, 4},
		{map[string]any{}, 1}, // no value: the fixed number is the fallback
	} {
		if got := c.NodeCount(tt.params); got != tt.want {
			t.Errorf("NodeCount(%v) = %d, want %d", tt.params, got, tt.want)
		}
	}
	if got := (Cluster{Nodes: 4}).NodeCount(map[string]any{"nodes": 9}); got != 4 {
		t.Errorf("a cluster with no nodesFrom ignores the parameters, got %d", got)
	}

	// Templates may name any node the instructor's choice could create.
	if names := sc.NodeNames(); len(names) != 9 || names[8] != "node-9" {
		t.Errorf("NodeNames = %v, want node-1 to node-9", names)
	}
}
