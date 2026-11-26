package scenario

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// scenarioFiles returns the files of a valid scenario folder with the given
// ID, name and product, placed under dir.
func scenarioFiles(dir, id, name, product string) map[string]string {
	y := strings.NewReplacer(
		"id: vault-raft-lost-quorum", "id: "+id,
		"name: Raft Lost Quorum", "name: "+name,
		"product: vault\neditions", "product: "+product+"\neditions",
	).Replace(worked)
	return map[string]string{
		dir + "/scenario.yaml":       y,
		dir + "/steps/01-break.md":   "# Break\n",
		dir + "/steps/02-recover.md": "# Recover\n",
	}
}

func merge(parts ...map[string]string) fstest.MapFS {
	all := map[string]string{}
	for _, p := range parts {
		for k, v := range p {
			all[k] = v
		}
	}
	return folder(all)
}

var registryOpts = Options{Primitives: knownPrimitives}

func ids(es []Entry) string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Scenario.ID
	}
	return strings.Join(out, ",")
}

func TestLoadFindsScenariosWithoutAnyGoChange(t *testing.T) {
	fsys := merge(
		scenarioFiles("scenarios/vault/raft", "vault-raft", "Raft", "vault"),
		scenarioFiles("scenarios/vault/kms", "vault-kms", "Broken KMS", "vault"),
		scenarioFiles("scenarios/boundary/workers", "boundary-workers", "Workers", "boundary"),
	)
	r := NewRegistry(registryOpts)
	if err := r.Load(fsys, "scenarios", BuiltIn); err != nil {
		t.Fatal(err)
	}
	// Ordered by product, then name.
	if got, want := ids(r.List()), "boundary-workers,vault-kms,vault-raft"; got != want {
		t.Errorf("List = %s, want %s", got, want)
	}

	// Adding a folder is the only change needed (R-SCN-16).
	for k, v := range scenarioFiles("scenarios/vault/new-one", "vault-new-one", "Another", "vault") {
		fsys[k] = &fstest.MapFile{Data: []byte(v)}
	}
	r2 := NewRegistry(registryOpts)
	if err := r2.Load(fsys, "scenarios", BuiltIn); err != nil {
		t.Fatal(err)
	}
	if _, ok := r2.Get("vault-new-one"); !ok {
		t.Error("the new folder was not discovered")
	}
}

func TestGetReportsTheSource(t *testing.T) {
	r := NewRegistry(registryOpts)
	if err := r.Load(merge(scenarioFiles("s/a", "vault-a", "A", "vault")), "s", User); err != nil {
		t.Fatal(err)
	}
	e, ok := r.Get("vault-a")
	if !ok || e.Source != User || e.Scenario.Dir != "s/a" {
		t.Errorf("Get = %+v, %v", e, ok)
	}
	if _, ok := r.Get("nope"); ok {
		t.Error("Get found a scenario that does not exist")
	}
}

func TestADuplicateIDKeepsTheFirst(t *testing.T) {
	r := NewRegistry(registryOpts)
	if err := r.Load(merge(scenarioFiles("b/raft", "vault-raft", "Built in", "vault")), "b", BuiltIn); err != nil {
		t.Fatal(err)
	}

	err := r.Load(merge(scenarioFiles("u/mine", "vault-raft", "Mine", "vault")), "u", User)
	ps := problemsOf(t, err)
	if len(ps) != 1 || ps[0].Field != "id" || ps[0].File != "u/mine/scenario.yaml" || !strings.Contains(ps[0].Message, "already used by the built-in scenario in b/raft") {
		t.Fatalf("problems = %+v, want the duplicate named with its source and folder", ps)
	}
	if ps[0].Line != 1 {
		t.Errorf("line = %d, want the id line (1)", ps[0].Line)
	}
	if e, _ := r.Get("vault-raft"); e.Source != BuiltIn || e.Scenario.Name != "Built in" {
		t.Errorf("the registry holds %+v, want the built-in scenario to win", e)
	}
}

func TestOneBrokenScenarioDoesNotHideTheOthers(t *testing.T) {
	broken := scenarioFiles("s/broken", "vault-broken", "Broken", "vault")
	broken["s/broken/scenario.yaml"] = strings.Replace(broken["s/broken/scenario.yaml"], "difficulty: intermediate", "difficulty: hard", 1)
	notYAML := map[string]string{"s/garbage/scenario.yaml": "id: [unclosed\n"}

	r := NewRegistry(registryOpts)
	err := r.Load(merge(scenarioFiles("s/good", "vault-good", "Good", "vault"), broken, notYAML), "s", User)

	ps := problemsOf(t, err)
	files := map[string]bool{}
	for _, p := range ps {
		files[p.File] = true
	}
	if !files["s/broken/scenario.yaml"] || !files["s/garbage/scenario.yaml"] {
		t.Errorf("problems = %+v, want both broken scenarios named", ps)
	}
	if got := ids(r.List()); got != "vault-good" {
		t.Errorf("catalogue = %q, want only the good scenario", got)
	}
}

func TestAMissingRootIsNotAnError(t *testing.T) {
	r := NewRegistry(registryOpts)
	if err := r.Load(folder(nil), "scenarios", User); err != nil {
		t.Errorf("Load of a missing root = %v, want nothing", err)
	}
	if len(r.List()) != 0 {
		t.Error("the catalogue is not empty")
	}
}

func TestFoldersWithoutAScenarioFileAreWalkedThrough(t *testing.T) {
	// scenarios/vault is only a grouping directory: the scenario is two levels down.
	r := NewRegistry(registryOpts)
	if err := r.Load(merge(scenarioFiles("scenarios/vault/deep/er", "vault-deep", "Deep", "vault")), "scenarios", BuiltIn); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Get("vault-deep"); !ok {
		t.Error("a scenario in a nested grouping folder was not found")
	}
}

func TestAScenarioFolderIsNeverSearchedForMoreScenarios(t *testing.T) {
	outer := scenarioFiles("s/outer", "vault-outer", "Outer", "vault")
	inner := scenarioFiles("s/outer/nested", "vault-nested", "Nested", "vault")

	r := NewRegistry(registryOpts)
	if err := r.Load(merge(outer, inner), "s", BuiltIn); err != nil {
		t.Fatal(err)
	}
	if got := ids(r.List()); got != "vault-outer" {
		t.Errorf("catalogue = %q, want only the outer scenario (R-SCN-15)", got)
	}
}

func TestHiddenFoldersAreSkipped(t *testing.T) {
	r := NewRegistry(registryOpts)
	err := r.Load(merge(
		scenarioFiles("s/.git/hooks", "vault-hidden", "Hidden", "vault"),
		scenarioFiles("s/visible", "vault-visible", "Visible", "vault"),
	), "s", User)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(r.List()); got != "vault-visible" {
		t.Errorf("catalogue = %q, want hidden folders skipped", got)
	}
}

func TestLoadFromARealDirectory(t *testing.T) {
	dir := t.TempDir()
	for name, body := range scenarioFiles("mine", "vault-mine", "Mine", "vault") {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	r := NewRegistry(registryOpts)
	if err := r.Load(os.DirFS(dir), ".", User); err != nil {
		t.Fatal(err)
	}
	e, ok := r.Get("vault-mine")
	if !ok {
		t.Fatal("scenario not found in the real directory")
	}
	if guide, err := e.Scenario.ReadFile("steps/01-break.md"); err != nil || string(guide) != "# Break\n" {
		t.Errorf("ReadFile = %q, %v", guide, err)
	}
}
