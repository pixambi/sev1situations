package scenario

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// worked is the example from section 4.4 of the definition, with the rest of
// the Scenario Standard filled in.
const worked = `id: vault-raft-lost-quorum
name: Raft Lost Quorum
summary: Recover a Vault cluster that has lost Raft quorum.
product: vault
editions: [community, enterprise]
difficulty: intermediate
estimatedMinutes: 20
tags: [vault, raft, quorum]
objectives:
  - Recognise the symptoms of lost quorum
  - Recover a single node with peers.json
prerequisites:
  - Basic Vault operations
parameters:
  - id: version
    type: version
    product: vault
    label: Vault version
  - id: nodes
    type: int
    label: Node count
    default: 3
    min: 3
    max: 5
topology:
  clusters:
    - name: primary
      product: vault
      nodes: 3
      storage: raft
  services:
    - name: postgres
      type: postgres
initialState: A healthy three node Vault cluster.
failureCreated: Two nodes are removed, so the cluster has no leader. It is recoverable.
successCriteria: The remaining node is the leader and accepts writes.
pitfalls:
  - Editing peers.json on the wrong node.
references:
  - title: Raft recovery
    url: https://developer.hashicorp.com/vault/docs/concepts/integrated-storage
steps:
  - id: break
    kind: breakage
    title: Remove two nodes
    guide: steps/01-break.md
    action:
      - primitive: container.remove
        args: { nodes: [node-2, node-3] }
    verify:
      - primitive: vault.expect-no-leader
        args: { node: node-1 }
  - id: recover
    kind: verification
    title: Cluster writable again
    guide: steps/02-recover.md
    resetPoint: true
    verify:
      - primitive: vault.raft-peers
        args: { node: node-1, voters: 1 }
`

func folder(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for name, body := range files {
		m[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return m
}

func loadOne(t *testing.T, yaml string) (*Scenario, error) {
	t.Helper()
	return Load(folder(map[string]string{"scenarios/vault/x/scenario.yaml": yaml}), "scenarios/vault/x")
}

func problemsOf(t *testing.T, err error) []Problem {
	t.Helper()
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("error = %v (%T), want a *LoadError", err, err)
	}
	return le.Problems
}

func TestLoadAValidScenario(t *testing.T) {
	sc, err := loadOne(t, worked)
	if err != nil {
		t.Fatal(err)
	}

	if sc.ID != "vault-raft-lost-quorum" || sc.Product != "vault" || sc.Difficulty != Intermediate || sc.EstimatedMinutes != 20 {
		t.Errorf("metadata = %q %q %q %d", sc.ID, sc.Product, sc.Difficulty, sc.EstimatedMinutes)
	}
	if len(sc.Editions) != 2 || len(sc.Tags) != 3 || len(sc.Objectives) != 2 || len(sc.Prerequisites) != 1 {
		t.Errorf("lists = %v %v %v %v", sc.Editions, sc.Tags, sc.Objectives, sc.Prerequisites)
	}
	if sc.InitialState == "" || sc.FailureCreated == "" || sc.SuccessCriteria == "" || len(sc.Pitfalls) != 1 || len(sc.References) != 1 {
		t.Errorf("standard fields not all loaded: %+v", sc)
	}
	if sc.References[0].URL == "" {
		t.Error("reference URL not loaded")
	}

	if len(sc.Parameters) != 2 || sc.Parameters[0].Type != ParamVersion || sc.Parameters[0].Product != "vault" {
		t.Fatalf("parameters = %+v", sc.Parameters)
	}
	nodes := sc.Parameters[1]
	if nodes.Type != ParamInt || nodes.Default != 3 || nodes.Min == nil || *nodes.Min != 3 || nodes.Max == nil || *nodes.Max != 5 {
		t.Errorf("int parameter = %+v", nodes)
	}

	if len(sc.Topology.Clusters) != 1 || sc.Topology.Clusters[0].Nodes != 3 || sc.Topology.Clusters[0].Storage != "raft" {
		t.Errorf("topology clusters = %+v", sc.Topology.Clusters)
	}
	if len(sc.Topology.Services) != 1 || sc.Topology.Services[0].Type != "postgres" {
		t.Errorf("topology services = %+v", sc.Topology.Services)
	}

	if len(sc.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(sc.Steps))
	}
	b, r := sc.Steps[0], sc.Steps[1]
	if b.Kind != KindBreakage || b.Guide != "steps/01-break.md" || len(b.Action) != 1 || len(b.Verify) != 1 {
		t.Errorf("break step = %+v", b)
	}
	if got := b.Action[0]; got.Primitive != "container.remove" || got.Args["nodes"] == nil {
		t.Errorf("action = %+v", got)
	}
	if r.Kind != KindVerification || !r.ResetPoint || r.Verify[0].Args["voters"] != 1 {
		t.Errorf("recover step = %+v", r)
	}
	if sc.Dir != "scenarios/vault/x" {
		t.Errorf("Dir = %q", sc.Dir)
	}
}

func TestASandboxHasNoSteps(t *testing.T) {
	sc, err := loadOne(t, "id: vault-sandbox\nname: Vault Sandbox\nproduct: vault\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.Steps) != 0 {
		t.Errorf("steps = %d, want none (R-SCN-17)", len(sc.Steps))
	}
}

func TestLoadRejectsBadFiles(t *testing.T) {
	const file = "scenarios/vault/x/scenario.yaml"
	tests := []struct {
		name     string
		yaml     string
		wantLine int
		wantText string
	}{
		{"unknown top-level field", "id: x\nname: X\nstepz: []\n", 3, "stepz"},
		{"unknown nested field", "id: x\nsteps:\n  - id: s1\n    kynd: breakage\n", 4, "kynd"},
		{"unknown parameter field", "id: x\nparameters:\n  - id: v\n    typo: version\n", 4, "typo"},
		{"wrong type for a number", "id: x\nestimatedMinutes: twenty\n", 2, ""},
		{"a list where text belongs", "id: x\nname: [a, b]\n", 2, ""},
		{"duplicate key", "id: x\nid: y\n", 2, "already defined"},
		{"tab indentation", "id: x\nsteps:\n\t- id: s1\n", 3, ""},
		{"empty file", "", 0, "empty"},
		{"two documents", "id: x\n---\nid: y\n", 0, "more than one"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadOne(t, tt.yaml)
			ps := problemsOf(t, err)
			if len(ps) == 0 {
				t.Fatal("no problems reported")
			}
			p := ps[0]
			if p.File != file {
				t.Errorf("file = %q, want %q", p.File, file)
			}
			if tt.wantLine != 0 && p.Line != tt.wantLine {
				t.Errorf("line = %d, want %d (%s)", p.Line, tt.wantLine, p.Message)
			}
			if !strings.Contains(p.Message, tt.wantText) {
				t.Errorf("message = %q, want it to contain %q", p.Message, tt.wantText)
			}
			if got := err.Error(); !strings.HasPrefix(got, file) {
				t.Errorf("error text %q does not start with the file name", got)
			}
		})
	}
}

func TestAllProblemsAreReportedTogether(t *testing.T) {
	_, err := loadOne(t, "id: x\nfoo: 1\nsteps:\n  - id: s1\n    bar: 2\n")
	ps := problemsOf(t, err)
	if len(ps) != 2 || ps[0].Line != 2 || ps[1].Line != 5 {
		t.Fatalf("problems = %+v, want the two unknown fields at lines 2 and 5", ps)
	}
	if got := strings.Count(err.Error(), "\n"); got != 1 {
		t.Errorf("error text has %d line breaks, want one line per problem", got)
	}
}

func TestMissingScenarioFile(t *testing.T) {
	_, err := Load(folder(map[string]string{"scenarios/vault/x/other.txt": "x"}), "scenarios/vault/x")
	ps := problemsOf(t, err)
	if len(ps) != 1 || !strings.Contains(ps[0].Message, FileName) || ps[0].File != "scenarios/vault/x/scenario.yaml" {
		t.Errorf("problems = %+v, want one naming the missing file", ps)
	}
}

func TestFieldLines(t *testing.T) {
	sc, err := loadOne(t, worked)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path string
		want int
	}{
		{"id", 1},
		{"difficulty", 6},
		{"steps", 42},
		{"steps[0]", 43},
		{"steps[0].kind", 44},
		{"steps[1].resetPoint", 57},
		{"topology.clusters[0].nodes", 29},
		{"parameters[1].max", 24},
	}
	for _, tt := range tests {
		if got := sc.Line(tt.path); got != tt.want {
			t.Errorf("Line(%q) = %d, want %d", tt.path, got, tt.want)
		}
	}
	if got := sc.Line("no.such.field"); got != 0 {
		t.Errorf("Line of an unknown field = %d, want 0", got)
	}
}

func TestReadFileStaysInsideTheFolder(t *testing.T) {
	fsys := folder(map[string]string{
		"scenarios/vault/x/scenario.yaml":     "id: x\n",
		"scenarios/vault/x/steps/01-break.md": "# Break\n",
		"scenarios/vault/y/steps/secret.md":   "other scenario",
	})
	sc, err := Load(fsys, "scenarios/vault/x")
	if err != nil {
		t.Fatal(err)
	}

	got, err := sc.ReadFile("steps/01-break.md")
	if err != nil || string(got) != "# Break\n" {
		t.Errorf("ReadFile = %q, %v", got, err)
	}
	for _, bad := range []string{"../y/steps/secret.md", "/etc/passwd", "..", ".", "steps/../../y/steps/secret.md"} {
		if _, err := sc.ReadFile(bad); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("ReadFile(%q) = %v, want ErrInvalid (R-SCN-15)", bad, err)
		}
	}
	if _, err := sc.ReadFile("steps/missing.md"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ReadFile of a missing file = %v, want ErrNotExist", err)
	}
}
